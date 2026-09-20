// Package mqtt implements the subset of MQTT 3.1.1 that the lab monitoring
// backend needs: connect, subscribe, publish at QoS 0 and 1, receive publishes,
// acknowledge them, and keep the session alive.
//
// It is written against the standard library rather than a client library
// because the backend must stay buildable and auditable without pulling a large
// dependency tree, and because the exact framing is part of what the device
// contract relies on. Anything outside the required subset — retained messages,
// wills, QoS 2, MQTT 5 properties — is deliberately unsupported and rejected
// rather than silently half-implemented.
package mqtt

import (
	"errors"
	"fmt"
	"io"
)

// PacketType is the MQTT 3.1.1 control packet type carried in the high nibble of
// the fixed header.
type PacketType byte

// Control packet types used by this package.
const (
	PacketCONNECT     PacketType = 1
	PacketCONNACK     PacketType = 2
	PacketPUBLISH     PacketType = 3
	PacketPUBACK      PacketType = 4
	PacketPUBREC      PacketType = 5
	PacketPUBREL      PacketType = 6
	PacketPUBCOMP     PacketType = 7
	PacketSUBSCRIBE   PacketType = 8
	PacketSUBACK      PacketType = 9
	PacketUNSUBSCRIBE PacketType = 10
	PacketUNSUBACK    PacketType = 11
	PacketPINGREQ     PacketType = 12
	PacketPINGRESP    PacketType = 13
	PacketDISCONNECT  PacketType = 14
)

// String names a packet type for diagnostics.
func (t PacketType) String() string {
	switch t {
	case PacketCONNECT:
		return "CONNECT"
	case PacketCONNACK:
		return "CONNACK"
	case PacketPUBLISH:
		return "PUBLISH"
	case PacketPUBACK:
		return "PUBACK"
	case PacketSUBSCRIBE:
		return "SUBSCRIBE"
	case PacketSUBACK:
		return "SUBACK"
	case PacketPINGREQ:
		return "PINGREQ"
	case PacketPINGRESP:
		return "PINGRESP"
	case PacketDISCONNECT:
		return "DISCONNECT"
	default:
		return fmt.Sprintf("PacketType(%d)", byte(t))
	}
}

// ProtocolLevel is MQTT 3.1.1. The backend speaks 3.1.1 because that is what the
// documented device firmware path targets; MQTT 5 is a later decision.
const ProtocolLevel byte = 4

// ProtocolName is the length-prefixed protocol name in the CONNECT packet.
const ProtocolName = "MQTT"

// maxRemainingLength is the largest value the remaining-length field can carry.
const maxRemainingLength = 268435455

// Errors returned by the codec. Callers classify protocol failures with
// errors.Is instead of matching message text.
var (
	// ErrMalformedPacket reports a packet that violates the framing rules.
	ErrMalformedPacket = errors.New("mqtt: malformed packet")
	// ErrUnsupportedPacket reports a well-formed packet outside the supported
	// subset, such as QoS 2 or MQTT 5.
	ErrUnsupportedPacket = errors.New("mqtt: unsupported packet")
	// ErrPacketTooLarge reports a packet above the configured read limit.
	ErrPacketTooLarge = errors.New("mqtt: packet exceeds the read limit")
)

// ConnackReturnCode is the connection acknowledgement status.
type ConnackReturnCode byte

// CONNACK return codes.
const (
	ConnackAccepted           ConnackReturnCode = 0x00
	ConnackBadProtocol        ConnackReturnCode = 0x01
	ConnackIdentifierRejected ConnackReturnCode = 0x02
	ConnackServerUnavailable  ConnackReturnCode = 0x03
	ConnackBadCredentials     ConnackReturnCode = 0x04
	ConnackNotAuthorized      ConnackReturnCode = 0x05
)

// String names a CONNACK return code for diagnostics.
func (c ConnackReturnCode) String() string {
	switch c {
	case ConnackAccepted:
		return "accepted"
	case ConnackBadProtocol:
		return "unacceptable protocol version"
	case ConnackIdentifierRejected:
		return "identifier rejected"
	case ConnackServerUnavailable:
		return "server unavailable"
	case ConnackBadCredentials:
		return "bad user name or password"
	case ConnackNotAuthorized:
		return "not authorized"
	default:
		return fmt.Sprintf("ConnackReturnCode(0x%02X)", byte(c))
	}
}

// TopicFilter is one subscription entry.
type TopicFilter struct {
	Topic string
	QoS   byte
}

// Packet is a decoded MQTT control packet.
//
// A single struct with optional fields is used instead of one type per packet so
// that the reader and writer stay small enough to audit. Fields not belonging to
// a packet's type are left at their zero value.
type Packet struct {
	Type PacketType

	// Flags are the low nibble of the fixed header. For PUBLISH they are the
	// DUP, QoS and RETAIN bits; for other packets the spec requires 0 and the
	// reader rejects anything else.
	Flags byte

	// CONNECT fields.
	ClientID     string
	Username     string
	Password     []byte
	KeepAlive    uint16
	CleanSession bool
	HasUsername  bool
	HasPassword  bool

	// PUBLISH fields.
	Topic    string
	Payload  []byte
	PacketID uint16

	// CONNACK fields.
	SessionPresent bool
	ReturnCode     ConnackReturnCode

	// SUBSCRIBE / SUBACK fields.
	Filters    []TopicFilter
	GrantedQoS []byte
}

// Dup reports the PUBLISH duplicate flag.
func (p *Packet) Dup() bool { return p.Flags&0x08 != 0 }

// QoS returns the PUBLISH quality of service.
func (p *Packet) QoS() byte { return (p.Flags >> 1) & 0x03 }

// Retain reports the PUBLISH retain flag.
func (p *Packet) Retain() bool { return p.Flags&0x01 != 0 }

// SetPublishFlags sets the DUP, QoS and RETAIN bits of a PUBLISH header.
func (p *Packet) SetPublishFlags(dup bool, qos byte, retain bool) {
	var flags byte
	if dup {
		flags |= 0x08
	}
	flags |= (qos & 0x03) << 1
	if retain {
		flags |= 0x01
	}
	p.Flags = flags
}

// ReadPacket reads one control packet from r. A packet larger than maxSize is
// rejected before its body is buffered, so a hostile peer cannot exhaust memory
// by announcing a huge length.
func ReadPacket(r io.Reader, maxSize int) (Packet, error) {
	var header [1]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Packet{}, err
	}
	packetType := PacketType(header[0] >> 4)
	flags := header[0] & 0x0F
	if packetType == 0 || packetType > PacketDISCONNECT {
		return Packet{}, fmt.Errorf("%w: unknown packet type %d", ErrMalformedPacket, packetType)
	}

	remaining, err := readRemainingLength(r)
	if err != nil {
		return Packet{}, err
	}
	if maxSize > 0 && remaining > maxSize {
		return Packet{}, fmt.Errorf("%w: declared %d bytes, limit %d", ErrPacketTooLarge, remaining, maxSize)
	}

	body := make([]byte, remaining)
	if _, err := io.ReadFull(r, body); err != nil {
		return Packet{}, err
	}

	packet := Packet{Type: packetType, Flags: flags}
	if err := decodeBody(&packet, body); err != nil {
		return Packet{}, err
	}
	return packet, nil
}

// Encode serialises p and writes it to w. It is deliberately not named WriteTo
// so that Packet does not accidentally satisfy io.WriterTo with a signature that
// omits the byte count.
func (p *Packet) Encode(w io.Writer) error {
	body, err := encodeBody(p)
	if err != nil {
		return err
	}
	if len(body) > maxRemainingLength {
		return fmt.Errorf("%w: %s body is %d bytes", ErrPacketTooLarge, p.Type, len(body))
	}

	out := make([]byte, 0, len(body)+5)
	out = append(out, byte(p.Type)<<4|p.Flags&0x0F)
	out = appendRemainingLength(out, len(body))
	out = append(out, body...)
	_, err = w.Write(out)
	return err
}

// readRemainingLength decodes the variable-length integer after the fixed
// header byte, following the spec's continuation-bit scheme.
func readRemainingLength(r io.Reader) (int, error) {
	var value int
	var multiplier int = 1
	for i := 0; i < 4; i++ {
		var b [1]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		value += int(b[0]&0x7F) * multiplier
		if b[0]&0x80 == 0 {
			return value, nil
		}
		multiplier *= 128
	}
	return 0, fmt.Errorf("%w: remaining length uses more than four bytes", ErrMalformedPacket)
}

// appendRemainingLength encodes a variable-length integer.
func appendRemainingLength(dst []byte, length int) []byte {
	for {
		digit := byte(length % 128)
		length /= 128
		if length > 0 {
			digit |= 0x80
		}
		dst = append(dst, digit)
		if length == 0 {
			return dst
		}
	}
}
