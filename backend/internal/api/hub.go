package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/BobcGn/final/backend/internal/domain"
	"github.com/BobcGn/final/backend/internal/events"
	"github.com/BobcGn/final/backend/internal/store"
)

// Realtime transport tuning. The ping interval is comfortably below the read
// deadline so that one lost pong does not tear down a healthy connection.
const (
	defaultPingInterval = 30 * time.Second
	defaultWriteTimeout = 10 * time.Second
	// defaultSendQueue bounds the per-client backlog. A client that cannot keep
	// up is disconnected and must re-sync over REST, which keeps a slow consumer
	// from growing the process's memory without bound.
	defaultSendQueue = 64
	// maxInboundMessageSize bounds a client frame. Clients only send pong
	// control frames, so anything larger is a protocol violation.
	maxInboundMessageSize = 1024
)

// Hub fans realtime events out to WebSocket subscribers. It implements
// events.Sink.
//
// Delivery is best effort by design. The stream carries increments only, never
// history, so a client that disconnects or is dropped must refetch latest or
// history over REST and then resubscribe. Event identifiers may repeat, so
// clients deduplicate on eventId.
type Hub struct {
	logger       *slog.Logger
	now          func() time.Time
	sendQueue    int
	maxClients   int
	pingInterval time.Duration
	upgrader     websocket.Upgrader

	mu      sync.Mutex
	rooms   map[string]map[*subscriber]struct{}
	clients int
}

// Deps configures a Hub.
type Deps struct {
	Logger *slog.Logger
	Now    func() time.Time
	// SendQueue overrides the per-client backlog bound.
	SendQueue int
	// MaxClients bounds concurrent connections. Zero means unlimited, which is
	// only appropriate for local development.
	MaxClients int
	// PingInterval overrides the server ping period.
	PingInterval time.Duration
	// CheckOrigin, when set, decides whether a browser origin may connect.
	CheckOrigin func(*http.Request) bool
}

// NewHub builds a Hub.
func NewHub(deps Deps) *Hub {
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	sendQueue := deps.SendQueue
	if sendQueue <= 0 {
		sendQueue = defaultSendQueue
	}
	pingInterval := deps.PingInterval
	if pingInterval <= 0 {
		pingInterval = defaultPingInterval
	}
	checkOrigin := deps.CheckOrigin
	if checkOrigin == nil {
		// Default to same-origin. A wildcard default would let any page on the
		// internet open an authenticated stream once tokens are enabled.
		checkOrigin = func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // non-browser client
			}
			return sameOrigin(origin, r.Host)
		}
	}
	return &Hub{
		logger:       logger,
		now:          now,
		sendQueue:    sendQueue,
		maxClients:   deps.MaxClients,
		pingInterval: pingInterval,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			CheckOrigin:     checkOrigin,
		},
		rooms: make(map[string]map[*subscriber]struct{}),
	}
}

// sameOrigin reports whether origin has the same host as the request.
func sameOrigin(origin, host string) bool {
	trimmed := origin
	for _, prefix := range []string{"http://", "https://"} {
		if len(trimmed) > len(prefix) && trimmed[:len(prefix)] == prefix {
			trimmed = trimmed[len(prefix):]
			break
		}
	}
	return trimmed == host
}

// subscriber is one connected realtime client.
type subscriber struct {
	conn     *websocket.Conn
	deviceID string
	send     chan []byte
	closed   chan struct{}
	once     sync.Once
}

// close releases the subscriber exactly once.
func (s *subscriber) close() {
	s.once.Do(func() { close(s.closed) })
}

// Serve upgrades the request and serves one realtime connection until the client
// goes away or falls too far behind. It blocks.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, deviceID string) {
	if h.maxClients > 0 && h.clientCount() >= h.maxClients {
		writeJSON(w, http.StatusServiceUnavailable, errorEnvelope{Error: errorBody{
			Code:    codeRateLimited,
			Message: "the realtime stream is at capacity; retry shortly",
		}})
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an HTTP error response.
		h.logger.LogAttrs(r.Context(), slog.LevelWarn, "websocket upgrade failed", slog.String("error", err.Error()))
		return
	}
	client := &subscriber{
		conn:     conn,
		deviceID: deviceID,
		send:     make(chan []byte, h.sendQueue),
		closed:   make(chan struct{}),
	}
	h.register(client)
	h.logger.LogAttrs(r.Context(), slog.LevelInfo, "realtime client connected", slog.String("device_id", deviceID))

	defer func() {
		h.unregister(client)
		client.close()
		_ = conn.Close()
		h.logger.LogAttrs(r.Context(), slog.LevelInfo, "realtime client disconnected", slog.String("device_id", deviceID))
	}()

	go h.readLoop(r.Context(), client)
	h.writeLoop(r.Context(), client)
}

// register adds a subscriber to its device room.
func (h *Hub) register(client *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[client.deviceID] == nil {
		h.rooms[client.deviceID] = make(map[*subscriber]struct{})
	}
	h.rooms[client.deviceID][client] = struct{}{}
	h.clients++
}

// unregister removes a subscriber.
func (h *Hub) unregister(client *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room, ok := h.rooms[client.deviceID]
	if !ok {
		return
	}
	if _, present := room[client]; !present {
		return
	}
	delete(room, client)
	h.clients--
	if len(room) == 0 {
		delete(h.rooms, client.deviceID)
	}
}

// clientCount returns the number of connected subscribers.
func (h *Hub) clientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.clients
}

// readLoop drains client frames so that control frames are processed and a
// closed connection is noticed promptly. Clients are not expected to send
// application data; anything that arrives is ignored.
func (h *Hub) readLoop(ctx context.Context, client *subscriber) {
	client.conn.SetReadLimit(maxInboundMessageSize)
	_ = client.conn.SetReadDeadline(time.Now().Add(2 * h.pingInterval))
	client.conn.SetPongHandler(func(string) error {
		return client.conn.SetReadDeadline(time.Now().Add(2 * h.pingInterval))
	})

	for {
		if _, _, err := client.conn.ReadMessage(); err != nil {
			if !isExpectedClose(err) {
				h.logger.LogAttrs(ctx, slog.LevelDebug, "realtime read loop ended", slog.String("error", err.Error()))
			}
			client.close()
			return
		}
	}
}

// writeLoop writes queued events and keeps the connection alive with pings.
func (h *Hub) writeLoop(ctx context.Context, client *subscriber) {
	ticker := time.NewTicker(h.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-client.closed:
			return
		case <-ctx.Done():
			_ = client.conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"),
				time.Now().Add(defaultWriteTimeout))
			return
		case payload := <-client.send:
			if err := client.write(payload); err != nil {
				h.logger.LogAttrs(ctx, slog.LevelDebug, "realtime write failed", slog.String("error", err.Error()))
				client.close()
				return
			}
		case <-ticker.C:
			if err := client.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(defaultWriteTimeout)); err != nil {
				client.close()
				return
			}
		}
	}
}

// write sends one text frame with a write deadline.
func (s *subscriber) write(payload []byte) error {
	if err := s.conn.SetWriteDeadline(time.Now().Add(defaultWriteTimeout)); err != nil {
		return err
	}
	return s.conn.WriteMessage(websocket.TextMessage, payload)
}

// isExpectedClose reports whether an error is an ordinary client disconnect
// rather than a defect worth logging at a higher level.
func isExpectedClose(err error) bool {
	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
		websocket.CloseAbnormalClosure,
	) || errors.Is(err, context.Canceled)
}

// broadcast marshals an event and queues it for every subscriber of its device.
//
// The call never blocks. A subscriber whose queue is full is disconnected
// instead, because buffering without bound would let one stalled client consume
// the process's memory and, worse, make telemetry intake depend on a client's
// read speed.
func (h *Hub) broadcast(ctx context.Context, envelope events.Envelope) {
	envelope.EventID = store.NewID(h.now())
	payload, err := json.Marshal(wsEnvelope{
		Type:       envelope.Type,
		EventID:    envelope.EventID,
		OccurredAt: timestamp(envelope.OccurredAt),
		DeviceID:   envelope.DeviceID,
		Data:       envelope.Data,
	})
	if err != nil {
		h.logger.LogAttrs(ctx, slog.LevelError, "could not encode realtime event",
			slog.String("type", string(envelope.Type)), slog.String("error", err.Error()))
		return
	}

	h.mu.Lock()
	targets := make([]*subscriber, 0, len(h.rooms[envelope.DeviceID]))
	for client := range h.rooms[envelope.DeviceID] {
		targets = append(targets, client)
	}
	h.mu.Unlock()

	for _, client := range targets {
		select {
		case client.send <- payload:
		case <-client.closed:
		default:
			h.logger.LogAttrs(ctx, slog.LevelWarn, "dropping a realtime client that fell behind",
				slog.String("device_id", client.deviceID), slog.Int("queue", h.sendQueue))
			client.close()
		}
	}
}

// PublishTelemetryUpdated implements events.Sink.
func (h *Hub) PublishTelemetryUpdated(ctx context.Context, sample domain.Telemetry) {
	h.broadcast(ctx, events.Envelope{
		Type:       events.TypeTelemetryUpdated,
		OccurredAt: sample.ReceivedAt,
		DeviceID:   sample.DeviceID,
		Data:       events.TelemetryDataFrom(sample),
	})
}

// PublishDeviceStatusChanged implements events.Sink.
func (h *Hub) PublishDeviceStatusChanged(ctx context.Context, deviceID string, data events.DeviceStatusData) {
	h.broadcast(ctx, events.Envelope{
		Type:       events.TypeDeviceStatusChanged,
		OccurredAt: h.now().UTC(),
		DeviceID:   deviceID,
		Data:       data,
	})
}

// PublishAlertStateChanged implements events.Sink.
func (h *Hub) PublishAlertStateChanged(ctx context.Context, event domain.AlertEvent) {
	occurredAt := event.StartedAt
	if event.EndedAt != nil {
		occurredAt = *event.EndedAt
	}
	h.broadcast(ctx, events.Envelope{
		Type:       events.TypeAlertStateChanged,
		OccurredAt: occurredAt,
		DeviceID:   event.DeviceID,
		Data:       events.AlertDataFrom(event),
	})
}

// PublishCommandStatusChanged implements events.Sink.
func (h *Hub) PublishCommandStatusChanged(ctx context.Context, command domain.Command) {
	occurredAt := command.AcceptedAt
	if command.CompletedAt != nil {
		occurredAt = *command.CompletedAt
	}
	h.broadcast(ctx, events.Envelope{
		Type:       events.TypeCommandStatusChanged,
		OccurredAt: occurredAt,
		DeviceID:   command.DeviceID,
		Data:       events.CommandStatusDataFrom(command),
	})
}

// PublishThresholdsConfirmed implements events.Sink.
func (h *Hub) PublishThresholdsConfirmed(ctx context.Context, deviceID string, version int) {
	h.broadcast(ctx, events.Envelope{
		Type:       events.TypeThresholdsConfirmed,
		OccurredAt: h.now().UTC(),
		DeviceID:   deviceID,
		Data:       events.ThresholdsConfirmedData{ConfirmedVersion: version},
	})
}

// handleWebSocket implements GET /ws/v1/devices/{deviceId}/telemetry.
//
// The device must already be known: subscribing to a device that has never
// reported would give a client a silent, permanently empty stream instead of an
// actionable 404.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	deviceID, err := s.deviceID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if s.cfg.Hub == nil {
		s.fail(w, r, newAPIError(http.StatusServiceUnavailable, codeNotImplemented,
			"the realtime stream is not configured on this instance", nil))
		return
	}
	if _, err := s.cfg.Store.Device(r.Context(), deviceID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.fail(w, r, newAPIError(http.StatusNotFound, codeDeviceNotFound, "device has never reported valid telemetry", map[string]any{"deviceId": deviceID}))
			return
		}
		s.fail(w, r, wrap(err, "the device could not be read before subscribing"))
		return
	}
	s.cfg.Hub.Serve(w, r, deviceID)
}

// var assertion that Hub satisfies the event sink contract.
var _ events.Sink = (*Hub)(nil)
