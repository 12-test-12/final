package mqtt_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/BobcGn/final/backend/internal/mqtt"
)

// roundTrip encodes a packet and decodes it again.
func roundTrip(t *testing.T, packet *mqtt.Packet) mqtt.Packet {
	t.Helper()

	var buffer bytes.Buffer
	if err := packet.Encode(&buffer); err != nil {
		t.Fatalf("encode %s: %v", packet.Type, err)
	}
	decoded, err := mqtt.ReadPacket(&buffer, 0)
	if err != nil {
		t.Fatalf("decode %s: %v", packet.Type, err)
	}
	if decoded.Type != packet.Type {
		t.Fatalf("decoded type %s, want %s", decoded.Type, packet.Type)
	}
	return decoded
}

// TestConnectRoundTrip covers the CONNECT fields the backend sets.
func TestConnectRoundTrip(t *testing.T) {
	decoded := roundTrip(t, &mqtt.Packet{
		Type:         mqtt.PacketCONNECT,
		ClientID:     "lab-backend",
		KeepAlive:    30,
		CleanSession: true,
		HasUsername:  true,
		Username:     "backend",
		HasPassword:  true,
		Password:     []byte("secret"),
	})

	if decoded.ClientID != "lab-backend" {
		t.Fatalf("client id = %q", decoded.ClientID)
	}
	if decoded.KeepAlive != 30 || !decoded.CleanSession {
		t.Fatalf("session flags decoded as keepAlive %d clean %v", decoded.KeepAlive, decoded.CleanSession)
	}
	if decoded.Username != "backend" || string(decoded.Password) != "secret" {
		t.Fatalf("credentials decoded as %q / %q", decoded.Username, decoded.Password)
	}
}

// TestPublishRoundTrip covers both QoS levels, including the packet identifier
// that only a QoS above zero carries.
func TestPublishRoundTrip(t *testing.T) {
	t.Run("qos 0", func(t *testing.T) {
		decoded := roundTrip(t, &mqtt.Packet{
			Type: mqtt.PacketPUBLISH, Topic: "device/telemetry", Payload: []byte("{}"),
		})
		if decoded.Topic != "device/telemetry" || string(decoded.Payload) != "{}" {
			t.Fatalf("decoded %+v", decoded)
		}
		if decoded.QoS() != 0 || decoded.PacketID != 0 {
			t.Fatalf("QoS 0 publish carried flags %d and packet id %d", decoded.QoS(), decoded.PacketID)
		}
	})

	t.Run("qos 1", func(t *testing.T) {
		packet := &mqtt.Packet{
			Type: mqtt.PacketPUBLISH, Topic: "device/control",
			Payload: []byte(`{"type":"set_mute"}`), PacketID: 7,
		}
		packet.SetPublishFlags(false, 1, false)
		decoded := roundTrip(t, packet)

		if decoded.QoS() != 1 {
			t.Fatalf("QoS = %d, want 1", decoded.QoS())
		}
		if decoded.PacketID != 7 {
			t.Fatalf("packet id = %d, want 7", decoded.PacketID)
		}
		if decoded.Dup() || decoded.Retain() {
			t.Fatalf("DUP and RETAIN must both be clear, got %d / %v", decoded.Flags, decoded.Retain())
		}
	})

	t.Run("empty payload", func(t *testing.T) {
		decoded := roundTrip(t, &mqtt.Packet{Type: mqtt.PacketPUBLISH, Topic: "device/telemetry"})
		if len(decoded.Payload) != 0 {
			t.Fatalf("payload = %q, want empty", decoded.Payload)
		}
	})
}

// TestSubscribeRoundTrip covers the subscription filters.
func TestSubscribeRoundTrip(t *testing.T) {
	decoded := roundTrip(t, &mqtt.Packet{
		Type: mqtt.PacketSUBSCRIBE, Flags: 0x02,
		PacketID: 3,
		Filters: []mqtt.TopicFilter{
			{Topic: "device/telemetry", QoS: 1},
			{Topic: "device/command-ack", QoS: 1},
		},
	})

	if len(decoded.Filters) != 2 {
		t.Fatalf("decoded %d filters, want 2", len(decoded.Filters))
	}
	if decoded.Filters[1].Topic != "device/command-ack" || decoded.Filters[1].QoS != 1 {
		t.Fatalf("second filter decoded as %+v", decoded.Filters[1])
	}
}

// TestSimplePacketsRoundTrip covers the packets with an empty body.
func TestSimplePacketsRoundTrip(t *testing.T) {
	for _, packetType := range []mqtt.PacketType{mqtt.PacketPINGREQ, mqtt.PacketPINGRESP, mqtt.PacketDISCONNECT} {
		decoded := roundTrip(t, &mqtt.Packet{Type: packetType})
		if decoded.Type != packetType {
			t.Fatalf("decoded %s, want %s", decoded.Type, packetType)
		}
	}
}

// TestConnackRoundTrip covers the connection acknowledgement.
func TestConnackRoundTrip(t *testing.T) {
	decoded := roundTrip(t, &mqtt.Packet{
		Type: mqtt.PacketCONNACK, ReturnCode: mqtt.ConnackBadCredentials, SessionPresent: true,
	})
	if decoded.ReturnCode != mqtt.ConnackBadCredentials {
		t.Fatalf("return code = %s", decoded.ReturnCode)
	}
	if !decoded.SessionPresent {
		t.Fatal("session-present flag was lost")
	}
}

// TestReadPacketRejectsMalformed covers the framing rules a hostile or buggy
// peer can violate.
func TestReadPacketRejectsMalformed(t *testing.T) {
	cases := map[string][]byte{
		"unknown packet type":               {0xF0, 0x00},
		"reserved flags on CONNECT":         {0x11, 0x00},
		"remaining length over four bytes":  {0x30, 0x80, 0x80, 0x80, 0x80, 0x01},
		"PINGREQ with a body":               {0xC0, 0x01, 0x00},
		"QoS bits both set":                 {0x36, 0x05, 0x00, 0x01, 'a', 0x00, 0x01},
		"QoS 2 publish":                     {0x34, 0x05, 0x00, 0x01, 'a', 0x00, 0x01},
		"publish with an empty topic":       {0x30, 0x02, 0x00, 0x00},
		"publish QoS 1 without a packet id": {0x32, 0x03, 0x00, 0x01, 'a'},
		"truncated body":                    {0x30, 0x10, 0x00, 0x01, 'a'},
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := mqtt.ReadPacket(bytes.NewReader(raw), 0); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// TestReadPacketRejectsOversizedDeclaredLength verifies the read limit, which
// stops a peer from making the process allocate an announced 256 MiB.
func TestReadPacketRejectsOversizedDeclaredLength(t *testing.T) {
	// A PUBLISH announcing a body far beyond the limit.
	raw := []byte{0x30, 0xFF, 0xFF, 0xFF, 0x7F}
	_, err := mqtt.ReadPacket(bytes.NewReader(raw), 1024)
	if !errors.Is(err, mqtt.ErrPacketTooLarge) {
		t.Fatalf("error = %v, want ErrPacketTooLarge", err)
	}
}

// TestReadPacketReturnsIOErrors verifies that a closed stream is not mistaken
// for a malformed packet.
func TestReadPacketReturnsIOErrors(t *testing.T) {
	if _, err := mqtt.ReadPacket(bytes.NewReader(nil), 0); !errors.Is(err, io.EOF) {
		t.Fatalf("error = %v, want io.EOF", err)
	}
}

// TestEncodeRejectsInvalidPackets verifies the encoder's own guards.
func TestEncodeRejectsInvalidPackets(t *testing.T) {
	cases := map[string]*mqtt.Packet{
		"CONNECT with reserved flags": {Type: mqtt.PacketCONNECT, Flags: 0x01, ClientID: "x"},
		"publish with an empty topic": {Type: mqtt.PacketPUBLISH},
		"publish QoS 1 without a packet id": func() *mqtt.Packet {
			packet := &mqtt.Packet{Type: mqtt.PacketPUBLISH, Topic: "a"}
			packet.SetPublishFlags(false, 1, false)
			return packet
		}(),
		"publish QoS 2": func() *mqtt.Packet {
			packet := &mqtt.Packet{Type: mqtt.PacketPUBLISH, Topic: "a", PacketID: 1}
			packet.SetPublishFlags(false, 2, false)
			return packet
		}(),
		"subscribe without filters": {Type: mqtt.PacketSUBSCRIBE, PacketID: 1},
		"subscribe without a packet id": {
			Type: mqtt.PacketSUBSCRIBE, Filters: []mqtt.TopicFilter{{Topic: "a", QoS: 1}},
		},
		"unsupported packet type": {Type: mqtt.PacketPUBREC},
	}
	for name, packet := range cases {
		t.Run(name, func(t *testing.T) {
			var buffer bytes.Buffer
			if err := packet.Encode(&buffer); err == nil {
				t.Fatalf("%s was encoded", name)
			}
		})
	}
}

// TestRemainingLengthBoundary verifies the variable-length integer encoding at
// the size where it grows past one byte.
func TestRemainingLengthBoundary(t *testing.T) {
	for _, size := range []int{0, 1, 127, 128, 16383, 16384} {
		payload := bytes.Repeat([]byte("x"), size)
		var buffer bytes.Buffer
		if err := (&mqtt.Packet{Type: mqtt.PacketPUBLISH, Topic: "t", Payload: payload}).Encode(&buffer); err != nil {
			t.Fatalf("encode a %d-byte payload: %v", size, err)
		}
		decoded, err := mqtt.ReadPacket(&buffer, 0)
		if err != nil {
			t.Fatalf("decode a %d-byte payload: %v", size, err)
		}
		if len(decoded.Payload) != size {
			t.Fatalf("decoded %d bytes, want %d", len(decoded.Payload), size)
		}
	}
}

// TestPacketTypeString ensures diagnostics name the packet rather than printing
// a bare number.
func TestPacketTypeString(t *testing.T) {
	cases := map[mqtt.PacketType]string{
		mqtt.PacketCONNECT:   "CONNECT",
		mqtt.PacketPUBLISH:   "PUBLISH",
		mqtt.PacketSUBACK:    "SUBACK",
		mqtt.PacketPINGRESP:  "PINGRESP",
		mqtt.PacketType(200): "PacketType(200)",
	}
	for packetType, want := range cases {
		if got := packetType.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", byte(packetType), got, want)
		}
	}
}

// TestConnackReturnCodeString ensures a refused connection is reported in words.
func TestConnackReturnCodeString(t *testing.T) {
	cases := map[mqtt.ConnackReturnCode]string{
		mqtt.ConnackAccepted:       "accepted",
		mqtt.ConnackBadCredentials: "bad user name or password",
		mqtt.ConnackNotAuthorized:  "not authorized",
		mqtt.ConnackReturnCode(9):  "ConnackReturnCode(0x09)",
	}
	for code, want := range cases {
		if got := code.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", byte(code), got, want)
		}
	}
}
