package liveness_test

import (
	"testing"
	"time"

	"github.com/BobcGn/final/backend/internal/domain"
	"github.com/BobcGn/final/backend/internal/liveness"
)

// base is a fixed instant so the cases are reproducible.
var base = time.Date(2026, 9, 18, 11, 20, 0, 0, time.UTC)

// newTracker builds a tracker with the frozen defaults.
func newTracker(t *testing.T) *liveness.Tracker {
	t.Helper()

	tracker, err := liveness.New(liveness.DefaultConfig())
	if err != nil {
		t.Fatalf("new tracker: %v", err)
	}
	return tracker
}

// TestUnknownDeviceIsUnknown verifies the state before any telemetry arrives.
func TestUnknownDeviceIsUnknown(t *testing.T) {
	tracker := newTracker(t)

	if got := tracker.State("MCU001"); got != domain.ConnectivityUnknown {
		t.Fatalf("state = %q, want unknown", got)
	}
	if _, ok := tracker.LastSeen("MCU001"); ok {
		t.Fatal("an unseen device reported a last-seen time")
	}
	// An unseen device must not be swept into an offline transition: there was
	// never an online state to leave, and announcing the transition would make
	// every provisioned-but-silent device look like a fresh outage.
	if transitions := tracker.Sweep(base.Add(time.Hour)); len(transitions) != 0 {
		t.Fatalf("an unseen device produced %d transitions", len(transitions))
	}
}

// TestObserveReportsTheFirstTransitionOnly verifies that a repeat sample from a
// live device is not reported as a change.
func TestObserveReportsTheFirstTransitionOnly(t *testing.T) {
	tracker := newTracker(t)

	transition, changed := tracker.Observe("MCU001", base)
	if !changed {
		t.Fatal("the first sample did not report coming online")
	}
	if transition.Connectivity != domain.ConnectivityOnline || !transition.LastSeenAt.Equal(base) {
		t.Fatalf("transition = %+v", transition)
	}

	if _, changed := tracker.Observe("MCU001", base.Add(5*time.Second)); changed {
		t.Fatal("a repeat sample was reported as a status change")
	}
	if got := tracker.State("MCU001"); got != domain.ConnectivityOnline {
		t.Fatalf("state = %q, want online", got)
	}
}

// TestOfflineBoundary verifies that the device is called offline only after the
// full silent interval, matching the contract's "three missed reports".
func TestOfflineBoundary(t *testing.T) {
	config := liveness.DefaultConfig()
	tracker := newTracker(t)
	tracker.Observe("MCU001", base)

	cases := []struct {
		name    string
		at      time.Duration
		offline bool
	}{
		{"one missed report", config.ReportInterval, false},
		{"two missed reports", 2 * config.ReportInterval, false},
		{"exactly at the threshold", config.OfflineAfter, false},
		{"one nanosecond past the threshold", config.OfflineAfter + time.Nanosecond, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fresh := newTracker(t)
			fresh.Observe("MCU001", base)
			transitions := fresh.Sweep(base.Add(testCase.at))
			if testCase.offline && len(transitions) != 1 {
				t.Fatalf("at %s produced %d transitions, want 1", testCase.at, len(transitions))
			}
			if !testCase.offline && len(transitions) != 0 {
				t.Fatalf("at %s produced %d transitions, want 0", testCase.at, len(transitions))
			}
		})
	}
}

// TestOfflineIsReportedOnce verifies that a periodic sweep does not repeat the
// same transition, which would flood the realtime stream.
func TestOfflineIsReportedOnce(t *testing.T) {
	tracker := newTracker(t)
	tracker.Observe("MCU001", base)

	first := tracker.Sweep(base.Add(time.Minute))
	if len(first) != 1 {
		t.Fatalf("first sweep produced %d transitions, want 1", len(first))
	}
	if first[0].LastSeenAt != base {
		t.Fatalf("the transition reports last-seen %s, want %s", first[0].LastSeenAt, base)
	}

	if second := tracker.Sweep(base.Add(2 * time.Minute)); len(second) != 0 {
		t.Fatalf("second sweep repeated %d transitions", len(second))
	}
}

// TestRecoveryAfterOffline verifies that telemetry brings a device back and that
// the recovery is reported.
func TestRecoveryAfterOffline(t *testing.T) {
	tracker := newTracker(t)
	tracker.Observe("MCU001", base)
	if transitions := tracker.Sweep(base.Add(time.Minute)); len(transitions) != 1 {
		t.Fatal("precondition failed: the device did not go offline")
	}

	recovered := base.Add(2 * time.Minute)
	transition, changed := tracker.Observe("MCU001", recovered)
	if !changed {
		t.Fatal("recovery was not reported")
	}
	if transition.Connectivity != domain.ConnectivityOnline {
		t.Fatalf("recovery reported %q, want online", transition.Connectivity)
	}
	if got := tracker.State("MCU001"); got != domain.ConnectivityOnline {
		t.Fatalf("state = %q, want online", got)
	}
}

// TestLastSeenNeverMovesBackwards verifies that a late-arriving sample cannot
// make a live device look stale, which would trigger a spurious offline sweep.
func TestLastSeenNeverMovesBackwards(t *testing.T) {
	tracker := newTracker(t)
	tracker.Observe("MCU001", base.Add(time.Minute))
	tracker.Observe("MCU001", base)

	lastSeen, ok := tracker.LastSeen("MCU001")
	if !ok {
		t.Fatal("the device is unknown")
	}
	if !lastSeen.Equal(base.Add(time.Minute)) {
		t.Fatalf("last seen moved back to %s", lastSeen)
	}
}

// TestForgetRemovesTheRecord verifies the removal path used when a device is
// decommissioned.
func TestForgetRemovesTheRecord(t *testing.T) {
	tracker := newTracker(t)
	tracker.Observe("MCU001", base)
	tracker.Forget("MCU001")

	if got := tracker.State("MCU001"); got != domain.ConnectivityUnknown {
		t.Fatalf("state after forgetting = %q, want unknown", got)
	}
}

// TestMultipleDevicesAreIndependent verifies that one silent device does not
// affect another.
func TestMultipleDevicesAreIndependent(t *testing.T) {
	tracker := newTracker(t)
	tracker.Observe("MCU001", base)
	tracker.Observe("MCU002", base)

	transitions := tracker.Sweep(base.Add(time.Minute))
	if len(transitions) != 2 {
		t.Fatalf("sweep produced %d transitions, want 2", len(transitions))
	}
	if transitions[0].DeviceID == transitions[1].DeviceID {
		t.Fatalf("sweep reported the same device twice: %+v", transitions)
	}
}

// TestConfigValidation covers the timing relationship the contract fixes.
func TestConfigValidation(t *testing.T) {
	cases := map[string]liveness.Config{
		"zero report interval":     {ReportInterval: 0, OfflineAfter: 15 * time.Second},
		"negative report interval": {ReportInterval: -time.Second, OfflineAfter: 15 * time.Second},
		"offline below three reports": {
			ReportInterval: 5 * time.Second,
			OfflineAfter:   10 * time.Second,
		},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := liveness.New(config); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}

	// Exactly three missed reports is the documented minimum.
	if _, err := liveness.New(liveness.Config{ReportInterval: 5 * time.Second, OfflineAfter: 15 * time.Second}); err != nil {
		t.Fatalf("the documented minimum was rejected: %v", err)
	}
}
