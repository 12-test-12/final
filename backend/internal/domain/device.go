// Package domain holds the lab environment monitoring model shared by the MQTT
// ingress, the storage layer and the HTTP API. It contains pure logic only: no
// database, broker or HTTP dependency may be imported here.
//
// Units are part of the contract and are always carried in the field names:
// TemperatureC is degrees Celsius, HumidityRh is percent relative humidity,
// GasAdc* is a 12-bit ADC code in 0..4095, and GasPpm is an uncalibrated
// estimate unless GasCalibrated is true.
package domain

import (
	"errors"
	"fmt"
	"regexp"
)

// MaxDeviceIDLen bounds the deviceId length so that it fits the MQTT clientId,
// the HTTP path parameter and the database column.
const MaxDeviceIDLen = 32

// deviceIDPattern is the frozen device identifier contract. It is shared by the
// MQTT clientId, the REST path parameter and the storage schema.
var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// Errors returned by the domain validation helpers. Callers map them onto HTTP
// status codes and MQTT error codes; they are never surfaced to users verbatim.
var (
	// ErrInvalidDeviceID reports a deviceId that violates the frozen pattern.
	ErrInvalidDeviceID = errors.New("domain: invalid device id")
	// ErrInvalidTelemetry reports a telemetry sample that failed validation.
	ErrInvalidTelemetry = errors.New("domain: invalid telemetry")
	// ErrInvalidThreshold reports a threshold value outside the device range.
	ErrInvalidThreshold = errors.New("domain: invalid threshold")
	// ErrInvalidCommand reports a control command that failed validation.
	ErrInvalidCommand = errors.New("domain: invalid command")
	// ErrInvalidAlert reports an alert transition that is not allowed.
	ErrInvalidAlert = errors.New("domain: invalid alert transition")
	// ErrSchemaUnsupported reports a payload whose schemaVersion is not handled.
	ErrSchemaUnsupported = errors.New("domain: unsupported schema version")
)

// SchemaVersion is the only device payload schema version this backend accepts.
// A payload with any other value is rejected instead of guessed at.
const SchemaVersion = 1

// ValidateDeviceID reports whether id satisfies the frozen device identifier
// contract `^[A-Za-z0-9_-]{1,32}$`. It returns a wrapped ErrInvalidDeviceID so
// callers can classify the failure with errors.Is.
func ValidateDeviceID(id string) error {
	if !deviceIDPattern.MatchString(id) {
		return fmt.Errorf("%w: %q must match %s", ErrInvalidDeviceID, id, deviceIDPattern.String())
	}
	return nil
}

// ValidateBootID reports whether a device-generated boot identifier is usable as
// an in-memory key. The contract requires 1..16 alphanumeric characters; the
// length bound exists because it is part of the telemetry dedup key.
func ValidateBootID(id string) error {
	if len(id) == 0 || len(id) > 16 {
		return fmt.Errorf("%w: bootId must be 1..16 characters, got %d", ErrInvalidTelemetry, len(id))
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		default:
			return fmt.Errorf("%w: bootId contains a non-alphanumeric character", ErrInvalidTelemetry)
		}
	}
	return nil
}
