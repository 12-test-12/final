// Package protocol implements the frozen MQTT payload codecs described in
// docs/device-protocol.md. It is pure: it turns bytes into validated domain
// values and back, and never touches the broker, the database or the clock.
//
// Every decoder is strict. Unknown fields, missing required fields, wrong JSON
// types and out-of-range numbers are rejected as a whole because a partially
// accepted sample would silently corrupt the stored evidence trail.
package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/BobcGn/final/backend/internal/domain"
)

// Frozen MQTT topics. They are fixed for phase 1; per-device topic trees are a
// deliberate later migration, not something the code may assume.
const (
	TopicTelemetry  = "device/telemetry"
	TopicCommandAck = "device/command-ack"
	TopicControl    = "device/control"
)

// MaxPayloadBytes bounds an accepted MQTT payload. The largest defined payload
// is well under 1 KiB; the limit stops a misbehaving publisher from allocating
// unbounded memory in the ingress path.
const MaxPayloadBytes = 4096

// Reject reasons. They reuse the frozen device error-code vocabulary where the
// same condition exists on both sides, and add backend-only reasons for
// conditions a device cannot report about itself.
const (
	ReasonMalformedJSON     = "malformed_json"
	ReasonTooLarge          = "payload_too_large"
	ReasonSchemaUnsupported = "schema_unsupported"
	ReasonMissingField      = "missing_field"
	ReasonUnknownField      = "unknown_field"
	ReasonBadRequestType    = "bad_request_type"
	ReasonOutOfRange        = "out_of_range"
	ReasonDeviceMismatch    = "device_mismatch"
	ReasonDeviceNotAllowed  = "device_not_allowed"
)

// DecodeError describes why a payload was rejected. Reason is a stable string
// safe to log and to count as a metric label; Field names the offending JSON
// field when one can be identified.
type DecodeError struct {
	Reason string
	Field  string
	err    error
}

// Error implements the error interface.
func (e *DecodeError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%s: %v", e.Reason, e.err)
	}
	return fmt.Sprintf("%s: field %s: %v", e.Reason, e.Field, e.err)
}

// Unwrap exposes the underlying cause so callers can use errors.Is against
// domain sentinels.
func (e *DecodeError) Unwrap() error { return e.err }

// newDecodeError builds a DecodeError.
func newDecodeError(reason, field string, err error) *DecodeError {
	return &DecodeError{Reason: reason, Field: field, err: err}
}

// decodeStrict unmarshals exactly one JSON object into dst after checking that
// every key is declared in allowed. It returns the decoded key set so that a
// caller can tell a field that is present but null from a field that is absent:
// the two are identical once unmarshalled into a pointer, and the frozen
// contract requires some nullable fields to be present.
//
// Unknown keys are detected by comparing the decoded key set against an explicit
// allowlist rather than by relying on json.Decoder.DisallowUnknownFields, whose
// error carries no machine-readable type and would have to be matched by
// message text.
func decodeStrict(raw []byte, allowed map[string]struct{}, dst any) (map[string]json.RawMessage, *DecodeError) {
	if len(raw) > MaxPayloadBytes {
		return nil, newDecodeError(ReasonTooLarge, "", fmt.Errorf("payload is %d bytes, limit is %d", len(raw), MaxPayloadBytes))
	}

	// A payload must be a JSON object. Checking the first significant byte keeps
	// a top-level array or scalar classified as malformed rather than as a range
	// problem, which is what the type error would otherwise look like.
	if first := firstSignificantByte(raw); first != '{' {
		return nil, newDecodeError(ReasonMalformedJSON, "",
			fmt.Errorf("payload must be a JSON object, found %s", describeFirstByte(first)))
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return nil, newDecodeError(ReasonOutOfRange, typeErr.Field, err)
		}
		return nil, newDecodeError(ReasonMalformedJSON, "", err)
	}
	if unknown := firstUnknownKey(keys, allowed); unknown != "" {
		return nil, newDecodeError(ReasonUnknownField, unknown, fmt.Errorf("field %q is not part of the frozen payload", unknown))
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return nil, newDecodeError(ReasonOutOfRange, typeErr.Field, err)
		}
		return nil, newDecodeError(ReasonMalformedJSON, "", err)
	}
	return keys, nil
}

// firstSignificantByte returns the first byte that is not JSON whitespace, or 0
// when the payload is empty or whitespace only.
func firstSignificantByte(raw []byte) byte {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b
		}
	}
	return 0
}

// describeFirstByte names a byte for an error message.
func describeFirstByte(b byte) string {
	if b == 0 {
		return "no content"
	}
	return fmt.Sprintf("%q", string(b))
}

// requireKeys reports the first absent required key, or "" when all are present.
// Sort order makes the reported field deterministic across runs.
func requireKeys(keys map[string]json.RawMessage, required []string) string {
	missing := make([]string, 0, len(required))
	for _, name := range required {
		if _, ok := keys[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	sort.Strings(missing)
	return missing[0]
}

// firstUnknownKey returns the lexicographically first key of keys that is absent
// from allowed, or "" when every key is declared. Sorting keeps the reported
// field deterministic so that tests and metrics do not flap.
func firstUnknownKey(keys map[string]json.RawMessage, allowed map[string]struct{}) string {
	var unknown []string
	for key := range keys {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return ""
	}
	sort.Strings(unknown)
	return unknown[0]
}

// checkSchemaVersion validates the shared envelope fields every payload carries.
func checkSchemaVersion(version int, messageType, wantType string) *DecodeError {
	if version != domain.SchemaVersion {
		return newDecodeError(ReasonSchemaUnsupported, "schemaVersion",
			fmt.Errorf("schemaVersion %d is not supported; this backend accepts %d", version, domain.SchemaVersion))
	}
	if messageType != wantType {
		return newDecodeError(ReasonBadRequestType, "messageType",
			fmt.Errorf("messageType %q is not valid for this topic", messageType))
	}
	return nil
}

// maxDeviceClockSkew bounds how far into the future a device timestamp may be
// before it is treated as a broken clock rather than a real event time.
const maxDeviceClockSkew = 24 * time.Hour

// earliestDeviceTimestamp is the oldest device timestamp the backend accepts. A
// device with an unsynced clock must send null instead of an epoch value, so an
// implausibly old timestamp is a defect rather than data.
var earliestDeviceTimestamp = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// parseDeviceTimestamp converts Unix milliseconds into a UTC timestamp, applying
// the plausibility window. A nil input is valid and means "clock not synced".
func parseDeviceTimestamp(ms *int64, now time.Time) (*time.Time, *DecodeError) {
	if ms == nil {
		return nil, nil
	}
	ts := time.UnixMilli(*ms).UTC()
	if ts.Before(earliestDeviceTimestamp) || ts.After(now.Add(maxDeviceClockSkew)) {
		return nil, newDecodeError(ReasonOutOfRange, "timestamp",
			fmt.Errorf("timestamp %s is outside the plausible window [%s, %s]",
				ts.Format(time.RFC3339), earliestDeviceTimestamp.Format(time.RFC3339), now.Add(maxDeviceClockSkew).Format(time.RFC3339)))
	}
	return &ts, nil
}
