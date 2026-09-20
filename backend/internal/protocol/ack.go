package protocol

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/BobcGn/final/backend/internal/domain"
)

// ackKeys is the frozen key set of the command acknowledgement payload.
var ackKeys = keySet(
	"schemaVersion", "messageType", "deviceId", "bootId", "sequence",
	"timestamp", "uptimeMs", "requestId", "status", "thresholdVersion", "errorCode",
)

// ackPayload mirrors the wire format of device/command-ack.
type ackPayload struct {
	SchemaVersion    *int    `json:"schemaVersion"`
	MessageType      string  `json:"messageType"`
	DeviceID         string  `json:"deviceId"`
	BootID           string  `json:"bootId"`
	Sequence         *uint32 `json:"sequence"`
	Timestamp        *int64  `json:"timestamp"`
	UptimeMs         *uint64 `json:"uptimeMs"`
	RequestID        string  `json:"requestId"`
	Status           string  `json:"status"`
	ThresholdVersion *int    `json:"thresholdVersion"`
	ErrorCode        *string `json:"errorCode"`
}

// CommandAck is a validated device acknowledgement of a control command.
type CommandAck struct {
	DeviceID         string
	BootID           string
	Sequence         uint32
	RequestID        string
	Status           domain.AckStatus
	ThresholdVersion *int
	ErrorCode        string
	// EventTime is the device event time when the clock is synced and the server
	// receive time otherwise.
	EventTime  time.Time
	Timestamp  *time.Time
	ReceivedAt time.Time
	UptimeMs   uint64
}

// DecodeCommandAck parses and validates a device/command-ack payload.
func DecodeCommandAck(raw []byte, expectedDeviceID string, receivedAt time.Time) (CommandAck, error) {
	var payload ackPayload
	keys, err := decodeStrict(raw, ackKeys, &payload)
	if err != nil {
		return CommandAck{}, err
	}
	// errorCode is required but nullable, so its key must be present even when
	// its value is null. Checking the decoded key set is the only way to tell
	// "explicitly null" from "field omitted".
	if missing := requireKeys(keys, []string{
		"schemaVersion", "messageType", "deviceId", "bootId", "sequence", "timestamp",
		"uptimeMs", "requestId", "status", "thresholdVersion", "errorCode",
	}); missing != "" {
		return CommandAck{}, newDecodeError(ReasonMissingField, missing, errMissing)
	}
	if err := checkSchemaVersion(*payload.SchemaVersion, payload.MessageType, "command_ack"); err != nil {
		return CommandAck{}, err
	}

	if expectedDeviceID != "" && payload.DeviceID != expectedDeviceID {
		return CommandAck{}, newDecodeError(ReasonDeviceMismatch, "deviceId",
			fmt.Errorf("payload deviceId %q does not match the configured device", payload.DeviceID))
	}
	if err := domain.ValidateDeviceID(payload.DeviceID); err != nil {
		return CommandAck{}, newDecodeError(ReasonOutOfRange, "deviceId", err)
	}
	if err := domain.ValidateBootID(payload.BootID); err != nil {
		return CommandAck{}, newDecodeError(ReasonOutOfRange, "bootId", err)
	}

	status := domain.AckStatus(payload.Status)
	if !status.Valid() {
		return CommandAck{}, newDecodeError(ReasonBadRequestType, "status",
			fmt.Errorf("status %q is not a frozen acknowledgement status", payload.Status))
	}
	errorCode := ""
	if payload.ErrorCode != nil {
		errorCode = *payload.ErrorCode
	}
	if _, err := domain.ApplyAck(status, errorCode, payload.ThresholdVersion, receivedAt); err != nil {
		return CommandAck{}, newDecodeError(ReasonOutOfRange, "errorCode", err)
	}

	timestamp, decodeErr := parseDeviceTimestamp(payload.Timestamp, receivedAt)
	if decodeErr != nil {
		return CommandAck{}, decodeErr
	}

	eventTime := receivedAt
	if timestamp != nil {
		eventTime = *timestamp
	}
	return CommandAck{
		DeviceID:         payload.DeviceID,
		BootID:           payload.BootID,
		Sequence:         *payload.Sequence,
		RequestID:        payload.RequestID,
		Status:           status,
		ThresholdVersion: payload.ThresholdVersion,
		ErrorCode:        errorCode,
		EventTime:        eventTime,
		Timestamp:        timestamp,
		ReceivedAt:       receivedAt,
		UptimeMs:         *payload.UptimeMs,
	}, nil
}

// EncodeCommandAck renders an acknowledgement as the frozen wire payload. It
// exists for tests and for the firmware-independent simulation harness.
func EncodeCommandAck(ack CommandAck) ([]byte, error) {
	var timestamp *int64
	if ack.Timestamp != nil {
		ms := ack.Timestamp.UnixMilli()
		timestamp = &ms
	}
	errorCode := ack.ErrorCode
	wire := ackPayload{
		SchemaVersion:    intPtr(domain.SchemaVersion),
		MessageType:      "command_ack",
		DeviceID:         ack.DeviceID,
		BootID:           ack.BootID,
		Sequence:         &ack.Sequence,
		Timestamp:        timestamp,
		UptimeMs:         &ack.UptimeMs,
		RequestID:        ack.RequestID,
		Status:           string(ack.Status),
		ThresholdVersion: ack.ThresholdVersion,
		ErrorCode:        &errorCode,
	}
	return json.Marshal(wire)
}
