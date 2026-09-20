package domain

import (
	"fmt"
	"time"
)

// AlertState is the phase-1 composite fire warning state of a device.
//
// There is deliberately no "acknowledged" state: phase 1 has no alert
// acknowledgement endpoint, and a state that no endpoint can produce would be a
// contract that lies. Adding acknowledgement is a phase-2 contract change that
// must add both the state and the endpoint together.
type AlertState string

// Frozen phase-1 alert states.
const (
	AlertNormal      AlertState = "normal"
	AlertSuspect     AlertState = "suspect"
	AlertFireWarning AlertState = "fire_warning"
	AlertRecovered   AlertState = "recovered"
)

// Valid reports whether s is a frozen alert state.
func (s AlertState) Valid() bool {
	switch s {
	case AlertNormal, AlertSuspect, AlertFireWarning, AlertRecovered:
		return true
	default:
		return false
	}
}

// Active reports whether the state represents an alert that is still running.
func (s AlertState) Active() bool {
	return s == AlertSuspect || s == AlertFireWarning
}

// AlertEvidence records why an alert fired. It is written once at trigger time
// and never recomputed, because reconstructing the reason for a historical
// alert from current values is not possible.
//
// The gas term is measured in ADC codes rather than ppm on purpose. The gas
// trend is a difference, the MQ135 estimate is uncalibrated, and gasPpm may be
// absent entirely; an ADC-coded rise is always available, is monotonic with
// concentration, and cannot be confused with the absolute gasHighPpm threshold.
type AlertEvidence struct {
	// GasAdcRise is the filtered gas increase across the evaluation window, in
	// ADC codes (0..4095).
	GasAdcRise int
	// GasAdcRiseThreshold is the configured rise threshold at trigger time, in
	// ADC codes.
	GasAdcRiseThreshold int
	// TemperatureRateCPerMinute is the least-squares temperature slope scaled to
	// one minute, in degrees Celsius per minute.
	TemperatureRateCPerMinute float64
	// TemperatureRateThresholdCPerMinute is the configured slope threshold.
	TemperatureRateThresholdCPerMinute float64
	// SampleCount is how many samples contributed to the decision.
	SampleCount int
	// WindowSeconds is the evaluation window length.
	WindowSeconds int
}

// AlertEvent is one composite fire warning episode for a device.
type AlertEvent struct {
	ID        string
	DeviceID  string
	State     AlertState
	StartedAt time.Time
	EndedAt   *time.Time
	Evidence  AlertEvidence
}

// Validate checks that an alert event is internally consistent. It returns an
// error wrapping ErrInvalidAlert.
func (e AlertEvent) Validate() error {
	if err := ValidateDeviceID(e.DeviceID); err != nil {
		return err
	}
	if !e.State.Valid() {
		return fmt.Errorf("%w: unknown state %q", ErrInvalidAlert, e.State)
	}
	if e.StartedAt.IsZero() {
		return fmt.Errorf("%w: startedAt must be set", ErrInvalidAlert)
	}
	if e.Evidence.SampleCount < 1 {
		return fmt.Errorf("%w: evidence.sampleCount must be >= 1", ErrInvalidAlert)
	}
	if e.Evidence.WindowSeconds < 1 {
		return fmt.Errorf("%w: evidence.windowSeconds must be >= 1", ErrInvalidAlert)
	}
	if e.EndedAt != nil && e.EndedAt.Before(e.StartedAt) {
		return fmt.Errorf("%w: endedAt precedes startedAt", ErrInvalidAlert)
	}
	return nil
}

// Connectivity is the backend's own verdict on whether a device is reachable.
// It is derived from the last valid telemetry time, never from the broker
// connection state and never from the device-reported Network field.
type Connectivity string

// Frozen connectivity values.
const (
	ConnectivityOnline  Connectivity = "online"
	ConnectivityOffline Connectivity = "offline"
	ConnectivityUnknown Connectivity = "unknown"
)
