package mqtt

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

// cursor is a bounds-checked reader over a decoded packet body. Every read
// returns false past the end instead of panicking, so a truncated packet from a
// hostile or buggy peer becomes an error rather than a crash.
type cursor struct {
	buf []byte
	pos int
}

// uint8 reads one byte.
func (c *cursor) uint8() (byte, bool) {
	if c.pos+1 > len(c.buf) {
		return 0, false
	}
	value := c.buf[c.pos]
	c.pos++
	return value, true
}

// uint16 reads a big-endian 16-bit integer.
func (c *cursor) uint16() (uint16, bool) {
	if c.pos+2 > len(c.buf) {
		return 0, false
	}
	value := binary.BigEndian.Uint16(c.buf[c.pos:])
	c.pos += 2
	return value, true
}

// string reads a length-prefixed UTF-8 string.
func (c *cursor) string() (string, bool) {
	length, ok := c.uint16()
	if !ok {
		return "", false
	}
	if c.pos+int(length) > len(c.buf) {
		return "", false
	}
	raw := c.buf[c.pos : c.pos+int(length)]
	c.pos += int(length)
	if !utf8.Valid(raw) {
		// The spec requires well-formed UTF-8; invalid bytes would otherwise
		// reach topics and identifiers that are compared as strings.
		return "", false
	}
	return string(raw), true
}

// bytes reads a length-prefixed byte string without UTF-8 validation, used for
// the CONNECT password field which may be arbitrary binary.
func (c *cursor) bytes() ([]byte, bool) {
	length, ok := c.uint16()
	if !ok {
		return nil, false
	}
	if c.pos+int(length) > len(c.buf) {
		return nil, false
	}
	value := c.buf[c.pos : c.pos+int(length)]
	c.pos += int(length)
	return value, true
}

// rest returns every remaining byte.
func (c *cursor) rest() []byte {
	value := c.buf[c.pos:]
	c.pos = len(c.buf)
	return value
}

// done reports whether the whole body was consumed.
func (c *cursor) done() bool { return c.pos == len(c.buf) }

// writer accumulates an encoded packet body.
type writer struct{ buf []byte }

// uint8 appends one byte.
func (w *writer) uint8(value byte) { w.buf = append(w.buf, value) }

// uint16 appends a big-endian 16-bit integer.
func (w *writer) uint16(value uint16) {
	w.buf = binary.BigEndian.AppendUint16(w.buf, value)
}

// string appends a length-prefixed UTF-8 string.
func (w *writer) string(value string) {
	w.uint16(uint16(len(value)))
	w.buf = append(w.buf, value...)
}

// raw appends bytes verbatim.
func (w *writer) raw(value []byte) { w.buf = append(w.buf, value...) }

// decodeBody parses a packet body into packet.
func decodeBody(packet *Packet, body []byte) error {
	c := &cursor{buf: body}
	switch packet.Type {
	case PacketCONNECT:
		return decodeConnect(packet, c)
	case PacketCONNACK:
		return decodeConnack(packet, c)
	case PacketPUBLISH:
		return decodePublish(packet, c)
	case PacketPUBACK:
		return decodePacketIDOnly(packet, c, PacketPUBACK)
	case PacketSUBSCRIBE:
		return decodeSubscribe(packet, c)
	case PacketSUBACK:
		return decodeSuback(packet, c)
	case PacketPINGREQ, PacketPINGRESP, PacketDISCONNECT:
		if len(body) != 0 {
			return fmt.Errorf("%w: %s must have an empty body", ErrMalformedPacket, packet.Type)
		}
		return nil
	default:
		return fmt.Errorf("%w: %s is outside the supported subset", ErrUnsupportedPacket, packet.Type)
	}
}

// decodeConnect parses a CONNECT packet.
func decodeConnect(packet *Packet, c *cursor) error {
	if packet.Flags != 0 {
		return fmt.Errorf("%w: CONNECT reserved flags must be zero", ErrMalformedPacket)
	}
	name, ok := c.string()
	if !ok {
		return fmt.Errorf("%w: CONNECT protocol name", ErrMalformedPacket)
	}
	if name != ProtocolName {
		return fmt.Errorf("%w: protocol name %q", ErrUnsupportedPacket, name)
	}
	level, ok := c.uint8()
	if !ok {
		return fmt.Errorf("%w: CONNECT protocol level", ErrMalformedPacket)
	}
	if level != ProtocolLevel {
		return fmt.Errorf("%w: protocol level %d; only MQTT 3.1.1 (4) is supported", ErrUnsupportedPacket, level)
	}
	flags, ok := c.uint8()
	if !ok {
		return fmt.Errorf("%w: CONNECT flags", ErrMalformedPacket)
	}
	if flags&0x01 != 0 {
		return fmt.Errorf("%w: CONNECT reserved flag must be zero", ErrMalformedPacket)
	}
	willFlag := flags&0x04 != 0
	willQoS := (flags >> 3) & 0x03
	if willFlag && willQoS > 2 {
		return fmt.Errorf("%w: invalid will QoS %d", ErrMalformedPacket, willQoS)
	}
	packet.CleanSession = flags&0x02 != 0
	packet.HasUsername = flags&0x80 != 0
	packet.HasPassword = flags&0x40 != 0

	keepAlive, ok := c.uint16()
	if !ok {
		return fmt.Errorf("%w: CONNECT keep alive", ErrMalformedPacket)
	}
	packet.KeepAlive = keepAlive

	clientID, ok := c.string()
	if !ok {
		return fmt.Errorf("%w: CONNECT client identifier", ErrMalformedPacket)
	}
	packet.ClientID = clientID

	if willFlag {
		return fmt.Errorf("%w: CONNECT will messages are not used by this deployment", ErrUnsupportedPacket)
	}
	if packet.HasUsername {
		username, ok := c.string()
		if !ok {
			return fmt.Errorf("%w: CONNECT user name", ErrMalformedPacket)
		}
		packet.Username = username
	}
	if packet.HasPassword {
		password, ok := c.bytes()
		if !ok {
			return fmt.Errorf("%w: CONNECT password", ErrMalformedPacket)
		}
		packet.Password = password
	}
	if !c.done() {
		return fmt.Errorf("%w: CONNECT has trailing bytes", ErrMalformedPacket)
	}
	return nil
}

// decodeConnack parses a CONNACK packet.
func decodeConnack(packet *Packet, c *cursor) error {
	flags, ok := c.uint8()
	if !ok {
		return fmt.Errorf("%w: CONNACK flags", ErrMalformedPacket)
	}
	if flags&0xFE != 0 {
		return fmt.Errorf("%w: CONNACK reserved flags must be zero", ErrMalformedPacket)
	}
	packet.SessionPresent = flags&0x01 != 0

	code, ok := c.uint8()
	if !ok {
		return fmt.Errorf("%w: CONNACK return code", ErrMalformedPacket)
	}
	packet.ReturnCode = ConnackReturnCode(code)
	if !c.done() {
		return fmt.Errorf("%w: CONNACK has trailing bytes", ErrMalformedPacket)
	}
	return nil
}

// decodePublish parses a PUBLISH packet.
func decodePublish(packet *Packet, c *cursor) error {
	qos := packet.QoS()
	if qos == 3 {
		return fmt.Errorf("%w: PUBLISH QoS bits are both set", ErrMalformedPacket)
	}
	if qos == 2 {
		return fmt.Errorf("%w: QoS 2 is not supported", ErrUnsupportedPacket)
	}
	if qos == 0 && packet.Dup() {
		return fmt.Errorf("%w: PUBLISH DUP must be zero for QoS 0", ErrMalformedPacket)
	}

	topic, ok := c.string()
	if !ok {
		return fmt.Errorf("%w: PUBLISH topic", ErrMalformedPacket)
	}
	if topic == "" {
		return fmt.Errorf("%w: PUBLISH topic must not be empty", ErrMalformedPacket)
	}
	packet.Topic = topic

	if qos > 0 {
		packetID, ok := c.uint16()
		if !ok {
			return fmt.Errorf("%w: PUBLISH packet identifier", ErrMalformedPacket)
		}
		if packetID == 0 {
			return fmt.Errorf("%w: PUBLISH packet identifier must not be zero", ErrMalformedPacket)
		}
		packet.PacketID = packetID
	}
	packet.Payload = c.rest()
	return nil
}

// decodePacketIDOnly parses a packet whose whole body is a packet identifier.
func decodePacketIDOnly(packet *Packet, c *cursor, want PacketType) error {
	packetID, ok := c.uint16()
	if !ok {
		return fmt.Errorf("%w: %s packet identifier", ErrMalformedPacket, want)
	}
	packet.PacketID = packetID
	if !c.done() {
		return fmt.Errorf("%w: %s has trailing bytes", ErrMalformedPacket, want)
	}
	return nil
}

// decodeSubscribe parses a SUBSCRIBE packet.
func decodeSubscribe(packet *Packet, c *cursor) error {
	if packet.Flags != 0x02 {
		// The spec fixes SUBSCRIBE's reserved flags to 0b0010; a peer that gets
		// this wrong is not interoperable and must not be accommodated.
		return fmt.Errorf("%w: SUBSCRIBE reserved flags must be 0x2", ErrMalformedPacket)
	}
	packetID, ok := c.uint16()
	if !ok || packetID == 0 {
		return fmt.Errorf("%w: SUBSCRIBE packet identifier must not be zero", ErrMalformedPacket)
	}
	packet.PacketID = packetID

	for !c.done() {
		topic, ok := c.string()
		if !ok {
			return fmt.Errorf("%w: SUBSCRIBE topic filter", ErrMalformedPacket)
		}
		qos, ok := c.uint8()
		if !ok {
			return fmt.Errorf("%w: SUBSCRIBE requested QoS", ErrMalformedPacket)
		}
		if qos > 2 {
			return fmt.Errorf("%w: SUBSCRIBE requested QoS %d", ErrMalformedPacket, qos)
		}
		packet.Filters = append(packet.Filters, TopicFilter{Topic: topic, QoS: qos})
	}
	if len(packet.Filters) == 0 {
		return fmt.Errorf("%w: SUBSCRIBE must carry at least one topic filter", ErrMalformedPacket)
	}
	return nil
}

// decodeSuback parses a SUBACK packet.
func decodeSuback(packet *Packet, c *cursor) error {
	if packet.Flags != 0 {
		return fmt.Errorf("%w: SUBACK reserved flags must be zero", ErrMalformedPacket)
	}
	packetID, ok := c.uint16()
	if !ok {
		return fmt.Errorf("%w: SUBACK packet identifier", ErrMalformedPacket)
	}
	packet.PacketID = packetID
	packet.GrantedQoS = c.rest()
	if len(packet.GrantedQoS) == 0 {
		return fmt.Errorf("%w: SUBACK must carry at least one return code", ErrMalformedPacket)
	}
	return nil
}

// encodeBody serialises the body of p, excluding the fixed header.
func encodeBody(p *Packet) ([]byte, error) {
	w := &writer{}
	switch p.Type {
	case PacketCONNECT:
		if p.Flags != 0 {
			return nil, fmt.Errorf("%w: CONNECT reserved flags must be zero", ErrMalformedPacket)
		}
		w.string(ProtocolName)
		w.uint8(ProtocolLevel)
		var flags byte
		if p.CleanSession {
			flags |= 0x02
		}
		if p.HasUsername {
			flags |= 0x80
		}
		if p.HasPassword {
			flags |= 0x40
		}
		w.uint8(flags)
		w.uint16(p.KeepAlive)
		w.string(p.ClientID)
		if p.HasUsername {
			w.string(p.Username)
		}
		if p.HasPassword {
			w.uint16(uint16(len(p.Password)))
			w.raw(p.Password)
		}
	case PacketCONNACK:
		var flags byte
		if p.SessionPresent {
			flags = 0x01
		}
		w.uint8(flags)
		w.uint8(byte(p.ReturnCode))
	case PacketPUBLISH:
		if p.QoS() == 3 {
			return nil, fmt.Errorf("%w: PUBLISH QoS bits are both set", ErrMalformedPacket)
		}
		if p.QoS() == 2 {
			return nil, fmt.Errorf("%w: QoS 2 is not supported", ErrUnsupportedPacket)
		}
		if p.Topic == "" {
			return nil, fmt.Errorf("%w: PUBLISH topic must not be empty", ErrMalformedPacket)
		}
		w.string(p.Topic)
		if p.QoS() > 0 {
			if p.PacketID == 0 {
				return nil, fmt.Errorf("%w: PUBLISH packet identifier must not be zero", ErrMalformedPacket)
			}
			w.uint16(p.PacketID)
		}
		w.raw(p.Payload)
	case PacketPUBACK, PacketPUBREC, PacketPUBREL, PacketPUBCOMP:
		if p.PacketID == 0 {
			return nil, fmt.Errorf("%w: %s packet identifier must not be zero", ErrMalformedPacket, p.Type)
		}
		w.uint16(p.PacketID)
	case PacketSUBSCRIBE, PacketUNSUBSCRIBE:
		if p.PacketID == 0 {
			return nil, fmt.Errorf("%w: %s packet identifier must not be zero", ErrMalformedPacket, p.Type)
		}
		if len(p.Filters) == 0 {
			return nil, fmt.Errorf("%w: %s must carry at least one topic filter", ErrMalformedPacket, p.Type)
		}
		w.uint16(p.PacketID)
		for _, filter := range p.Filters {
			w.string(filter.Topic)
			qos := filter.QoS
			if p.Type == PacketUNSUBSCRIBE {
				// UNSUBSCRIBE carries no QoS byte; the spec puts 0 in the
				// payload position only for SUBSCRIBE.
				continue
			}
			w.uint8(qos)
		}
	case PacketSUBACK, PacketUNSUBACK:
		if p.PacketID == 0 {
			return nil, fmt.Errorf("%w: %s packet identifier must not be zero", ErrMalformedPacket, p.Type)
		}
		w.uint16(p.PacketID)
		if len(p.GrantedQoS) == 0 {
			return nil, fmt.Errorf("%w: %s must carry at least one return code", ErrMalformedPacket, p.Type)
		}
		w.raw(p.GrantedQoS)
	case PacketPINGREQ, PacketPINGRESP, PacketDISCONNECT:
	default:
		return nil, fmt.Errorf("%w: cannot encode %s", ErrUnsupportedPacket, p.Type)
	}
	return w.buf, nil
}
