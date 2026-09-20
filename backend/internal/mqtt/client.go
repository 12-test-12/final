package mqtt

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

// Defaults for the client. They match the values recorded in
// docs/device-protocol.md for the device side so that both ends of the link are
// configured from the same numbers.
const (
	DefaultKeepAlive      = 30 * time.Second
	DefaultWriteTimeout   = 10 * time.Second
	DefaultConnectTimeout = 10 * time.Second
	DefaultReconnectMin   = 1 * time.Second
	DefaultReconnectMax   = 30 * time.Second
	DefaultReadLimit      = 64 * 1024
	// DefaultPublishTimeout bounds how long a QoS 1 publish waits for PUBACK
	// before being reported as a failure to the caller.
	DefaultPublishTimeout = 5 * time.Second
)

// Errors returned by the client.
var (
	// ErrNotConnected reports a publish attempted while the session is down.
	ErrNotConnected = errors.New("mqtt: not connected")
	// ErrPublishTimeout reports a QoS 1 publish that was never acknowledged.
	ErrPublishTimeout = errors.New("mqtt: publish acknowledgement timed out")
	// ErrRejected reports a broker that refused a subscribe or a connect.
	ErrRejected = errors.New("mqtt: broker rejected the request")
)

// Message is one received application message.
type Message struct {
	Topic   string
	Payload []byte
	QoS     byte
	Retain  bool
}

// HandlerFunc processes one received message. It must not block for long: the
// read loop calls it inline so that acknowledgement order matches arrival order.
type HandlerFunc func(context.Context, Message)

// Config configures a Client.
type Config struct {
	// Address is the broker host:port.
	Address string
	// TLS enables TLS when non-nil. The address must then be the TLS endpoint.
	TLS *tls.Config
	// ClientID is the MQTT client identifier. It must be unique per connection.
	ClientID string
	// Username and Password are optional credentials.
	Username string
	Password string
	// KeepAlive is the negotiated keep-alive interval.
	KeepAlive time.Duration
	// CleanSession requests a fresh session on every connect.
	CleanSession bool
	// Subscriptions are established after every successful connect.
	Subscriptions []TopicFilter
	// Handler receives inbound messages.
	Handler HandlerFunc
	// Logger receives connection lifecycle events. Nil disables logging.
	Logger *slog.Logger
	// ReconnectMin and ReconnectMax bound the exponential reconnect backoff.
	ReconnectMin time.Duration
	ReconnectMax time.Duration
	// PublishTimeout bounds the QoS 1 acknowledgement wait.
	PublishTimeout time.Duration
	// ReadLimit caps an inbound packet body.
	ReadLimit int
}

// withDefaults fills every unset field.
func (c Config) withDefaults() Config {
	if c.KeepAlive <= 0 {
		c.KeepAlive = DefaultKeepAlive
	}
	if c.ReconnectMin <= 0 {
		c.ReconnectMin = DefaultReconnectMin
	}
	if c.ReconnectMax <= 0 {
		c.ReconnectMax = DefaultReconnectMax
	}
	if c.ReconnectMax < c.ReconnectMin {
		c.ReconnectMax = c.ReconnectMin
	}
	if c.PublishTimeout <= 0 {
		c.PublishTimeout = DefaultPublishTimeout
	}
	if c.ReadLimit <= 0 {
		c.ReadLimit = DefaultReadLimit
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.DiscardHandler)
	}
	return c
}

// validate rejects configurations that cannot produce a working session.
func (c Config) validate() error {
	if c.Address == "" {
		return errors.New("mqtt: address is required")
	}
	if c.ClientID == "" {
		return errors.New("mqtt: client id is required")
	}
	if len(c.ClientID) > 23 {
		// MQTT 3.1.1 §3.1.3.1 requires brokers to accept at least 23 bytes and
		// leaves longer identifiers to broker discretion; staying inside the
		// guaranteed bound keeps the client portable across brokers.
		return fmt.Errorf("mqtt: client id %q exceeds 23 bytes", c.ClientID)
	}
	if c.KeepAlive > 0 && c.KeepAlive < time.Second {
		return fmt.Errorf("mqtt: keep alive %s is below one second", c.KeepAlive)
	}
	return nil
}

// Client is a minimal MQTT 3.1.1 client with automatic reconnection.
//
// Run owns the connection lifecycle and blocks. Publish may be called from any
// goroutine while Run is active; it fails with ErrNotConnected between
// reconnects rather than queueing, so a caller that must not lose a command sees
// the failure and reports it instead of assuming delivery.
type Client struct {
	cfg Config

	writeMu sync.Mutex

	stateMu   sync.RWMutex
	conn      net.Conn
	connected bool
	connEpoch uint64

	pendingMu sync.Mutex
	pending   map[uint16]chan struct{}
	nextID    uint16
}

// New validates cfg and returns a client. It does not connect.
func New(cfg Config) (*Client, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, pending: make(map[uint16]chan struct{}), nextID: 1}, nil
}

// KeepAlive returns the configured keep-alive interval.
func (c *Client) KeepAlive() time.Duration { return c.cfg.KeepAlive }

// Connected reports whether the session is currently established.
func (c *Client) Connected() bool {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.connected
}

// Run connects, subscribes and serves messages until ctx is cancelled. It
// reconnects with exponential backoff after any failure, including a rejected
// connect, so that a broker restart or a wrong credential is retried instead of
// silently stopping the ingress path.
func (c *Client) Run(ctx context.Context) error {
	backoff := c.cfg.ReconnectMin
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := c.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.cfg.Logger.LogAttrs(ctx, slog.LevelWarn, "mqtt session ended; reconnecting",
			slog.String("error", errorString(err)),
			slog.Duration("retry_in", backoff))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if err != nil && backoff < c.cfg.ReconnectMax {
			backoff *= 2
			if backoff > c.cfg.ReconnectMax {
				backoff = c.cfg.ReconnectMax
			}
		}
		if err == nil {
			backoff = c.cfg.ReconnectMin
		}
	}
}

// session runs one connection attempt end to end. It returns when the connection
// fails, when ctx is cancelled, or when the read loop exits.
func (c *Client) session(ctx context.Context) error {
	dialer := &net.Dialer{Timeout: DefaultConnectTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.cfg.Address)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.cfg.Address, err)
	}
	if c.cfg.TLS != nil {
		conn = tls.Client(conn, c.cfg.TLS)
	}
	defer func() { _ = conn.Close() }()

	if err := c.handshake(conn); err != nil {
		return err
	}
	c.setState(conn, true)
	defer c.setState(nil, false)
	c.cfg.Logger.LogAttrs(ctx, slog.LevelInfo, "mqtt connected",
		slog.String("address", c.cfg.Address), slog.String("client_id", c.cfg.ClientID))

	if err := c.subscribe(conn); err != nil {
		return err
	}

	// The ping goroutine is stopped before the connection is closed by the
	// deferred Close above, so a ping cannot outlive its socket.
	pingCtx, stopPing := context.WithCancel(ctx)
	defer stopPing()
	go c.pingLoop(pingCtx, conn)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := c.readLoop(ctx, conn); err != nil {
			return err
		}
		return nil
	}
}

// handshake sends CONNECT and verifies the CONNACK return code.
func (c *Client) handshake(conn net.Conn) error {
	connect := &Packet{
		Type:         PacketCONNECT,
		ClientID:     c.cfg.ClientID,
		KeepAlive:    uint16(c.cfg.KeepAlive / time.Second),
		CleanSession: c.cfg.CleanSession,
		HasUsername:  c.cfg.Username != "",
		Username:     c.cfg.Username,
		HasPassword:  c.cfg.Password != "",
		Password:     []byte(c.cfg.Password),
	}
	if err := c.writePacket(conn, connect); err != nil {
		return fmt.Errorf("send CONNECT: %w", err)
	}

	// A broker that never answers must not hold the ingress down forever.
	if err := conn.SetReadDeadline(time.Now().Add(DefaultConnectTimeout)); err != nil {
		return err
	}
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	reply, err := ReadPacket(conn, c.cfg.ReadLimit)
	if err != nil {
		return fmt.Errorf("read CONNACK: %w", err)
	}
	if reply.Type != PacketCONNACK {
		return fmt.Errorf("%w: expected CONNACK, got %s", ErrMalformedPacket, reply.Type)
	}
	if reply.ReturnCode != ConnackAccepted {
		return fmt.Errorf("%w: %s", ErrRejected, reply.ReturnCode)
	}
	return nil
}

// subscribe establishes every configured subscription and requires the broker to
// grant the requested QoS, so a silently downgraded subscription is visible.
func (c *Client) subscribe(conn net.Conn) error {
	if len(c.cfg.Subscriptions) == 0 {
		return nil
	}
	packetID := c.allocatePacketID()
	subscribe := &Packet{
		Type:     PacketSUBSCRIBE,
		Flags:    0x02,
		PacketID: packetID,
		Filters:  c.cfg.Subscriptions,
	}
	if err := c.writePacket(conn, subscribe); err != nil {
		return fmt.Errorf("send SUBSCRIBE: %w", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(DefaultConnectTimeout)); err != nil {
		return err
	}
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	reply, err := ReadPacket(conn, c.cfg.ReadLimit)
	if err != nil {
		return fmt.Errorf("read SUBACK: %w", err)
	}
	if reply.Type != PacketSUBACK {
		return fmt.Errorf("%w: expected SUBACK, got %s", ErrMalformedPacket, reply.Type)
	}
	if reply.PacketID != packetID {
		return fmt.Errorf("%w: SUBACK packet id %d does not match the request %d", ErrMalformedPacket, reply.PacketID, packetID)
	}
	if len(reply.GrantedQoS) != len(c.cfg.Subscriptions) {
		return fmt.Errorf("%w: SUBACK granted %d of %d subscriptions", ErrRejected, len(reply.GrantedQoS), len(c.cfg.Subscriptions))
	}
	for i, granted := range reply.GrantedQoS {
		if granted == 0x80 {
			return fmt.Errorf("%w: subscription to %q was refused", ErrRejected, c.cfg.Subscriptions[i].Topic)
		}
		if granted > c.cfg.Subscriptions[i].QoS {
			return fmt.Errorf("%w: subscription to %q granted QoS %d above the requested %d",
				ErrMalformedPacket, c.cfg.Subscriptions[i].Topic, granted, c.cfg.Subscriptions[i].QoS)
		}
	}
	return nil
}

// readLoop serves packets until the connection fails.
func (c *Client) readLoop(ctx context.Context, conn net.Conn) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// The read deadline is twice the keep alive: one missed ping is
		// tolerated, two in a row means the peer is gone.
		if err := conn.SetReadDeadline(time.Now().Add(2 * c.cfg.KeepAlive)); err != nil {
			return err
		}
		packet, err := ReadPacket(conn, c.cfg.ReadLimit)
		if err != nil {
			if errors.Is(err, io.EOF) || isTimeout(err) {
				return fmt.Errorf("connection lost: %w", err)
			}
			return fmt.Errorf("read packet: %w", err)
		}

		switch packet.Type {
		case PacketPUBLISH:
			if err := c.handlePublish(ctx, conn, packet); err != nil {
				return err
			}
		case PacketPUBACK:
			c.completePending(packet.PacketID)
		case PacketPINGRESP:
			// Liveness only; the read deadline already covers the failure case.
		default:
			c.cfg.Logger.LogAttrs(ctx, slog.LevelWarn, "ignoring unexpected packet",
				slog.String("type", packet.Type.String()))
		}
	}
}

// handlePublish acknowledges an inbound publish and dispatches it.
func (c *Client) handlePublish(ctx context.Context, conn net.Conn, packet Packet) error {
	if packet.QoS() > 0 {
		// The PUBACK is sent before the handler runs so that a slow handler
		// cannot cause the broker to redeliver the message.
		if err := c.writePacket(conn, &Packet{Type: PacketPUBACK, PacketID: packet.PacketID}); err != nil {
			return fmt.Errorf("send PUBACK: %w", err)
		}
	}
	if c.cfg.Handler != nil {
		c.cfg.Handler(ctx, Message{
			Topic:   packet.Topic,
			Payload: packet.Payload,
			QoS:     packet.QoS(),
			Retain:  packet.Retain(),
		})
	}
	return nil
}

// pingLoop sends PINGREQ at the keep-alive interval.
func (c *Client) pingLoop(ctx context.Context, conn net.Conn) {
	ticker := time.NewTicker(c.cfg.KeepAlive)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.writePacket(conn, &Packet{Type: PacketPINGREQ}); err != nil {
				c.cfg.Logger.LogAttrs(ctx, slog.LevelWarn, "keep-alive ping failed",
					slog.String("error", err.Error()))
				// Closing forces the read loop to notice and reconnect.
				_ = conn.Close()
				return
			}
		}
	}
}

// Publish sends an application message. QoS 1 waits for the broker's PUBACK and
// returns ErrPublishTimeout when it never arrives; QoS 0 returns once the bytes
// are written and therefore reports only a successful hand-off to the socket.
func (c *Client) Publish(ctx context.Context, topic string, payload []byte, qos byte) error {
	conn := c.connection()
	if conn == nil {
		return ErrNotConnected
	}
	if qos > 1 {
		return fmt.Errorf("%w: publish QoS %d is not supported", ErrUnsupportedPacket, qos)
	}

	packet := &Packet{Type: PacketPUBLISH, Topic: topic, Payload: payload}
	if qos == 1 {
		packet.SetPublishFlags(false, 1, false)
		packet.PacketID = c.allocatePacketID()
		waiter := c.registerPending(packet.PacketID)
		defer c.cancelPending(packet.PacketID)

		if err := c.writePacket(conn, packet); err != nil {
			return fmt.Errorf("publish %s: %w", topic, err)
		}
		select {
		case <-waiter:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.cfg.PublishTimeout):
			return fmt.Errorf("%w: topic %s packet id %d", ErrPublishTimeout, topic, packet.PacketID)
		}
	}

	packet.SetPublishFlags(false, 0, false)
	if err := c.writePacket(conn, packet); err != nil {
		return fmt.Errorf("publish %s: %w", topic, err)
	}
	return nil
}

// writePacket serialises a packet under the write lock and applies the write
// deadline. Concurrent writers are serialised because MQTT framing is a byte
// stream: interleaving two packets would corrupt the session.
func (c *Client) writePacket(conn net.Conn, packet *Packet) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := conn.SetWriteDeadline(time.Now().Add(DefaultWriteTimeout)); err != nil {
		return err
	}
	return packet.Encode(conn)
}

// setState publishes the current connection.
func (c *Client) setState(conn net.Conn, connected bool) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	c.conn = conn
	c.connected = connected
	if connected {
		c.connEpoch++
	}
	// Any in-flight acknowledgement wait belongs to the previous connection.
	c.pendingMu.Lock()
	for id, waiter := range c.pending {
		close(waiter)
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
}

// connection returns the live connection or nil.
func (c *Client) connection() net.Conn {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	if !c.connected {
		return nil
	}
	return c.conn
}

// allocatePacketID returns the next non-zero packet identifier.
func (c *Client) allocatePacketID() uint16 {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	c.nextID++
	if c.nextID == 0 {
		c.nextID = 1
	}
	return c.nextID
}

// registerPending creates the channel a PUBACK will signal.
func (c *Client) registerPending(id uint16) chan struct{} {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	waiter := make(chan struct{})
	c.pending[id] = waiter
	return waiter
}

// cancelPending removes a waiter that is no longer needed.
func (c *Client) cancelPending(id uint16) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	delete(c.pending, id)
}

// completePending signals the goroutine waiting for a PUBACK.
func (c *Client) completePending(id uint16) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	if waiter, ok := c.pending[id]; ok {
		close(waiter)
		delete(c.pending, id)
	}
}

// isTimeout reports whether err is a deadline expiry.
func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// errorString renders an error for logging without a nil pointer dereference.
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
