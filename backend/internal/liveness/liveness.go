// Package liveness decides whether a device is reachable.
//
// The verdict is derived only from the arrival of valid telemetry. It is never
// taken from the broker connection state and never from the device-reported
// network field, because a device can be connected to the broker while its
// application loop is stuck, and can be running locally while the broker is
// unreachable.
//
// Offline is decided by absence of traffic, so it cannot be discovered while
// handling a message. Tracker.Sweep exists to be called from a timer.
package liveness

import (
	"fmt"
	"sync"
	"time"

	"github.com/BobcGn/final/backend/internal/domain"
)

// Config configures offline detection.
type Config struct {
	// ReportInterval is the expected telemetry period. The device contract sets
	// it to 5 seconds.
	ReportInterval time.Duration
	// OfflineAfter is the silence that marks a device offline. The frozen
	// contract derives it as three missed report intervals.
	OfflineAfter time.Duration
}

// DefaultConfig returns the frozen phase-1 timing.
func DefaultConfig() Config {
	return Config{ReportInterval: 5 * time.Second, OfflineAfter: 15 * time.Second}
}

// MissedReportsBeforeOffline is the number of consecutive missed report periods
// that the contract requires before a device is called offline.
const MissedReportsBeforeOffline = 3

// Validate checks the timing relationship between the two values.
func (c Config) Validate() error {
	if c.ReportInterval <= 0 {
		return fmt.Errorf("liveness: report interval must be positive, got %s", c.ReportInterval)
	}
	minimum := time.Duration(MissedReportsBeforeOffline) * c.ReportInterval
	if c.OfflineAfter < minimum {
		return fmt.Errorf("liveness: offlineAfter %s must be at least %d report intervals (%s)",
			c.OfflineAfter, MissedReportsBeforeOffline, minimum)
	}
	return nil
}

// Transition reports a connectivity change for one device.
type Transition struct {
	DeviceID     string
	Connectivity domain.Connectivity
	// LastSeenAt is when the device was last heard from.
	LastSeenAt time.Time
	// Since is the moment the new state took effect: the receive time for a
	// recovery, and the sweep time for a degradation.
	Since time.Time
}

// Tracker keeps the last-seen time of every device and derives connectivity.
// It is safe for concurrent use.
type Tracker struct {
	cfg Config

	mu      sync.Mutex
	devices map[string]*entry
}

// entry is the per-device liveness record. now is injectable so tests can drive
// time without sleeping.
type entry struct {
	lastSeen     time.Time
	connectivity domain.Connectivity
}

// New returns a tracker with the given configuration.
func New(cfg Config) (*Tracker, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Tracker{cfg: cfg, devices: make(map[string]*entry)}, nil
}

// Config returns the tracker configuration.
func (t *Tracker) Config() Config { return t.cfg }

// Observe records that a valid telemetry sample arrived at receiveTime. It
// returns the transition when the device was previously offline or unknown,
// which is the signal the API uses to publish a device.status_changed event.
func (t *Tracker) Observe(deviceID string, receiveTime time.Time) (Transition, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	current, known := t.devices[deviceID]
	previous := domain.ConnectivityUnknown
	if known {
		previous = current.connectivity
	}
	// Last-seen only ever moves forward. A late-arriving sample must not make a
	// device look stale and must not delay an offline decision.
	if known && current.lastSeen.After(receiveTime) {
		receiveTime = current.lastSeen
	}
	next := &entry{lastSeen: receiveTime, connectivity: domain.ConnectivityOnline}
	t.devices[deviceID] = next

	if previous == domain.ConnectivityOnline {
		return Transition{}, false
	}
	return Transition{
		DeviceID:     deviceID,
		Connectivity: domain.ConnectivityOnline,
		LastSeenAt:   receiveTime,
		Since:        receiveTime,
	}, true
}

// Sweep marks every device whose last valid sample is older than OfflineAfter as
// offline and returns the transitions. Devices already known to be offline are
// not reported again, so a periodic sweep does not spam the event stream.
func (t *Tracker) Sweep(now time.Time) []Transition {
	t.mu.Lock()
	defer t.mu.Unlock()

	var transitions []Transition
	for deviceID, current := range t.devices {
		if current.connectivity == domain.ConnectivityOffline {
			continue
		}
		// Exactly at the boundary the device is still considered online: the
		// contract says three *missed* intervals, so the third silent interval
		// must have elapsed completely.
		if now.Sub(current.lastSeen) <= t.cfg.OfflineAfter {
			continue
		}
		current.connectivity = domain.ConnectivityOffline
		transitions = append(transitions, Transition{
			DeviceID:     deviceID,
			Connectivity: domain.ConnectivityOffline,
			LastSeenAt:   current.lastSeen,
			Since:        now,
		})
	}
	return transitions
}

// State returns the connectivity of one device, or ConnectivityUnknown when the
// device has never reported.
func (t *Tracker) State(deviceID string) domain.Connectivity {
	t.mu.Lock()
	defer t.mu.Unlock()

	current, known := t.devices[deviceID]
	if !known {
		return domain.ConnectivityUnknown
	}
	return current.connectivity
}

// LastSeen returns the last valid telemetry receive time and whether the device
// is known at all.
func (t *Tracker) LastSeen(deviceID string) (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	current, known := t.devices[deviceID]
	if !known {
		return time.Time{}, false
	}
	return current.lastSeen, true
}

// Forget drops a device's liveness record. It is used when a device is removed;
// the online decision for a device that is merely unreachable must go through
// Sweep so that the offline transition is still published.
func (t *Tracker) Forget(deviceID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.devices, deviceID)
}
