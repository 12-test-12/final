package domain

import "fmt"

// Threshold range limits frozen in docs/device-protocol.md §4.3. The values fit
// the device's integer sample resolution and leave headroom above the highest
// configured default, so an operator can always raise a threshold remotely
// before it is reached.
const (
	MinTemperatureHighC = 0.0
	MaxTemperatureHighC = 80.0
	MinHumidityHighRh   = 0.0
	MaxHumidityHighRh   = 100.0
	MinGasHighPpm       = 1.0
	MaxGasHighPpm       = 999.0

	// InitialThresholdVersion is the version the device reports for its
	// compile-time defaults (hardware/STM32_Project1/User/app_config.h). The
	// contract defines version 1 as "never remotely configured", so a device
	// never reports version 0 and the backend never mints version 0.
	InitialThresholdVersion = 1
)

// DefaultThresholds are the compile-time firmware defaults, mirrored here so the
// backend can answer GET /thresholds before the device has ever reported, and so
// a device running unconfigured limits is not shown as unconfigured.
//
// They must be kept in sync with hardware/STM32_Project1/User/app_config.h.
func DefaultThresholds() Thresholds {
	return Thresholds{
		TemperatureHighC: 30,
		HumidityHighRh:   80,
		GasHighPpm:       20,
	}
}

// Thresholds is the desired alarm threshold set for one device.
type Thresholds struct {
	TemperatureHighC float64
	HumidityHighRh   float64
	GasHighPpm       float64
}

// Validate checks every threshold against the frozen device range. It returns an
// error wrapping ErrInvalidThreshold naming the offending field.
func (t Thresholds) Validate() error {
	if !isFinite(t.TemperatureHighC) || t.TemperatureHighC < MinTemperatureHighC || t.TemperatureHighC > MaxTemperatureHighC {
		return fmt.Errorf("%w: temperatureHighC %v outside [%v,%v]", ErrInvalidThreshold, t.TemperatureHighC, MinTemperatureHighC, MaxTemperatureHighC)
	}
	if !isFinite(t.HumidityHighRh) || t.HumidityHighRh < MinHumidityHighRh || t.HumidityHighRh > MaxHumidityHighRh {
		return fmt.Errorf("%w: humidityHighRh %v outside [%v,%v]", ErrInvalidThreshold, t.HumidityHighRh, MinHumidityHighRh, MaxHumidityHighRh)
	}
	if !isFinite(t.GasHighPpm) || t.GasHighPpm < MinGasHighPpm || t.GasHighPpm > MaxGasHighPpm {
		return fmt.Errorf("%w: gasHighPpm %v outside [%v,%v]", ErrInvalidThreshold, t.GasHighPpm, MinGasHighPpm, MaxGasHighPpm)
	}
	return nil
}

// Equal reports whether two threshold sets are identical. It backs idempotency
// checks: replaying the same idempotency key with a different payload is a
// conflict, not a new command.
func (t Thresholds) Equal(other Thresholds) bool {
	return t.TemperatureHighC == other.TemperatureHighC &&
		t.HumidityHighRh == other.HumidityHighRh &&
		t.GasHighPpm == other.GasHighPpm
}
