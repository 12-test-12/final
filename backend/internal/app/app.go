// Package app is the composition root: it builds the store, the alert engine,
// the MQTT ingress, the control service and the HTTP API, then runs them until
// the context is cancelled.
//
// It lives outside package main so that the wiring itself is testable. A
// composition root that only exists inside main can only be exercised by
// starting a whole process, which means its failure modes — a missing
// dependency, a listener that cannot bind, a sweeper that panics — are found in
// production rather than in a test.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/BobcGn/final/backend/internal/alert"
	"github.com/BobcGn/final/backend/internal/api"
	"github.com/BobcGn/final/backend/internal/command"
	"github.com/BobcGn/final/backend/internal/config"
	"github.com/BobcGn/final/backend/internal/ingest"
	"github.com/BobcGn/final/backend/internal/liveness"
	"github.com/BobcGn/final/backend/internal/mqtt"
	"github.com/BobcGn/final/backend/internal/protocol"
	"github.com/BobcGn/final/backend/internal/store"
)

// shutdownGrace bounds how long in-flight HTTP requests may finish.
const shutdownGrace = 10 * time.Second

// Options are the inputs of Run.
type Options struct {
	// Config is the validated process configuration.
	Config config.Config
	// Logger receives structured diagnostics. Nil discards them.
	Logger *slog.Logger
	// Ready, when set, is called once the HTTP listener is bound, with the
	// address it is serving. It exists so that a caller which asked for port 0
	// can learn the port the kernel assigned.
	Ready func(address string)
	// Store overrides the persistence layer. Nil selects the implementation the
	// configuration asks for; tests inject the in-memory store so they exercise
	// the real wiring without a database.
	Store store.Store
}

// Run builds the process and blocks until ctx is cancelled or a listener fails.
//
// It returns nil on a clean shutdown. A failure to start is returned rather than
// logged and swallowed, because a service that is not running must not exit
// successfully.
func Run(ctx context.Context, opts Options) error {
	cfg := opts.Config
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	logStartupWarnings(ctx, cfg, logger)

	// The publisher is created before the broker client, which is created before
	// the ingress service because the client's message handler is that service.
	// The holder breaks the cycle without hiding the failure case: publishing
	// before the client exists reports a broker failure, which is exactly what an
	// operator should see.
	publisher := &brokerPublisher{}

	dataStore, closeStore, err := openStore(ctx, cfg, opts.Store, logger)
	if err != nil {
		return err
	}
	defer closeStore()

	engine, err := alert.NewEngine(cfg.Alert)
	if err != nil {
		return err
	}
	tracker, err := liveness.New(cfg.Liveness())
	if err != nil {
		return err
	}
	hub := api.NewHub(api.Deps{Logger: logger, MaxClients: cfg.MaxWebSocketClients})

	commandService, err := command.New(command.Deps{
		Store:     dataStore,
		Publisher: publisher,
		Sink:      hub,
		Logger:    logger,
		TTL:       cfg.CommandTTL,
	})
	if err != nil {
		return err
	}
	ingestService, err := ingest.New(ingest.Deps{
		Store:          dataStore,
		Engine:         engine,
		Tracker:        tracker,
		Sink:           hub,
		AckHandler:     commandService,
		Logger:         logger,
		AllowedDevices: cfg.AllowedDevices,
	})
	if err != nil {
		return err
	}
	handler, err := api.NewServer(api.Config{
		Store:        dataStore,
		Ingest:       ingestService,
		Commands:     commandService,
		Hub:          hub,
		Tracker:      tracker,
		Logger:       logger,
		AuthMode:     api.AuthMode(cfg.AuthMode),
		Tokens:       cfg.AuthTokens,
		OfflineAfter: cfg.OfflineAfter,
	})
	if err != nil {
		return err
	}

	// Listening explicitly rather than through ListenAndServe lets the caller
	// learn the bound address, and turns a bind failure into a returned error
	// instead of a fatal log line from a goroutine.
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("app: listen on %s: %w", cfg.Addr, err)
	}
	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// The realtime route hijacks the connection, so a write timeout shorter
		// than a client's idle period would abort healthy streams.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}
	logger.LogAttrs(ctx, slog.LevelInfo, "http server listening", slog.String("addr", listener.Addr().String()))
	if opts.Ready != nil {
		opts.Ready(listener.Addr().String())
	}

	var background sync.WaitGroup
	background.Add(1)
	go func() {
		defer background.Done()
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.LogAttrs(ctx, slog.LevelError, "http server stopped", slog.String("error", err.Error()))
		}
	}()

	if cfg.UsesBroker() {
		client, err := newBrokerClient(cfg, ingestService, logger)
		if err != nil {
			_ = httpServer.Close()
			return err
		}
		publisher.set(client)
		background.Add(1)
		go func() {
			defer background.Done()
			if err := client.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.LogAttrs(ctx, slog.LevelError, "mqtt client stopped", slog.String("error", err.Error()))
			}
		}()
	}

	background.Add(1)
	go func() {
		defer background.Done()
		runSweeper(ctx, cfg.SweepInterval, ingestService, commandService, logger)
	}()

	<-ctx.Done()
	logger.LogAttrs(context.Background(), slog.LevelInfo, "shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.LogAttrs(context.Background(), slog.LevelWarn, "http shutdown did not complete cleanly", slog.String("error", err.Error()))
	}
	background.Wait()
	return nil
}

// logStartupWarnings records the configurations that are valid but not safe to
// run unattended, so the decision is visible in the log rather than implicit.
func logStartupWarnings(ctx context.Context, cfg config.Config, logger *slog.Logger) {
	logger.LogAttrs(ctx, slog.LevelInfo, "starting backend", slog.Any("config", cfg.Redacted()))
	if api.AuthMode(cfg.AuthMode) == api.AuthNone {
		logger.LogAttrs(ctx, slog.LevelWarn,
			"authentication is disabled; control routes must not be reachable from an untrusted network")
	}
	if cfg.UsesMemoryStore() {
		logger.LogAttrs(ctx, slog.LevelWarn,
			"no DATABASE_URL configured; running on the in-memory store, which loses all telemetry on restart")
	}
	if !cfg.UsesBroker() {
		logger.LogAttrs(ctx, slog.LevelWarn,
			"no MQTT_BROKER_URL configured; device ingress and control publishing are disabled")
	}
}

// openStore selects the persistence implementation and returns its closer.
func openStore(ctx context.Context, cfg config.Config, override store.Store, logger *slog.Logger) (store.Store, func(), error) {
	if override != nil {
		return override, func() {}, nil
	}
	if cfg.UsesMemoryStore() {
		return store.NewMemory(), func() {}, nil
	}
	postgres, err := store.OpenPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	logger.LogAttrs(ctx, slog.LevelInfo, "connected to postgres and applied migrations")
	return postgres, postgres.Close, nil
}

// runSweeper drives the checks that no inbound message can trigger: a device
// that stops reporting produces silence, and a command that is never
// acknowledged produces nothing at all. Both need a timer.
func runSweeper(ctx context.Context, interval time.Duration, ingestService *ingest.Service, commandService *command.Service, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			now = now.UTC()
			if _, err := ingestService.SweepOffline(ctx, now); err != nil {
				logger.LogAttrs(ctx, slog.LevelError, "offline sweep failed", slog.String("error", err.Error()))
			}
			if expired, err := commandService.ExpireDue(ctx, now); err != nil {
				logger.LogAttrs(ctx, slog.LevelError, "command expiry sweep failed", slog.String("error", err.Error()))
			} else if len(expired) > 0 {
				logger.LogAttrs(ctx, slog.LevelInfo, "expired unacknowledged commands", slog.Int("count", len(expired)))
			}
		}
	}
}

// errNoBroker reports a publish attempted while no broker client exists. It is
// the honest answer when the deployment runs without MQTT: the command cannot
// reach a device, and the API must surface that as a broker failure rather than
// record a command the device will never see.
var errNoBroker = errors.New("mqtt: no broker client is configured")

// brokerPublisher adapts the MQTT client to the command service's Publisher
// port.
type brokerPublisher struct {
	mu     sync.RWMutex
	client *mqtt.Client
}

// set installs the client. It is called once during start-up.
func (p *brokerPublisher) set(client *mqtt.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.client = client
}

// Publish implements command.Publisher.
func (p *brokerPublisher) Publish(ctx context.Context, topic string, payload []byte, qos byte) error {
	p.mu.RLock()
	client := p.client
	p.mu.RUnlock()

	if client == nil {
		return errNoBroker
	}
	return client.Publish(ctx, topic, payload, qos)
}

// newBrokerClient builds the MQTT client and points its inbound handler at the
// ingest service.
//
// The subscription list is exactly the two device-to-cloud topics of the frozen
// contract. The backend never subscribes to the control topic: it publishes
// there, and consuming its own commands would be a way to lose them.
func newBrokerClient(cfg config.Config, handler *ingest.Service, logger *slog.Logger) (*mqtt.Client, error) {
	address, err := brokerAddress(cfg.BrokerURL)
	if err != nil {
		return nil, err
	}

	clientCfg := mqtt.Config{
		Address:      address,
		ClientID:     cfg.MQTTClientID,
		Username:     cfg.BrokerUser,
		Password:     cfg.BrokerPass,
		KeepAlive:    cfg.MQTTKeepAlive,
		CleanSession: true,
		Logger:       logger,
		Subscriptions: []mqtt.TopicFilter{
			{Topic: protocol.TopicTelemetry, QoS: 1},
			{Topic: protocol.TopicCommandAck, QoS: 1},
		},
		Handler: func(ctx context.Context, message mqtt.Message) {
			if err := handler.HandleMessage(ctx, message); err != nil {
				// A single rejected message must not tear down the session: the
				// counters in the ingest service are what an operator watches for
				// a systematic problem.
				logger.LogAttrs(ctx, slog.LevelWarn, "message rejected",
					slog.String("topic", message.Topic),
					slog.String("error", err.Error()))
			}
		},
	}
	if cfg.BrokerTLS {
		clientCfg.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return mqtt.New(clientCfg)
}

// brokerSchemes are the URL schemes an operator may paste from a broker's
// documentation. Anything else is left alone so the address check reports it.
var brokerSchemes = []string{"tcp://", "mqtt://", "ssl://", "tls://", "mqtts://"}

// brokerAddress normalises a configured broker URL into a host:port dial target.
//
// The port is validated here rather than left to the dial, because net.SplitHostPort
// accepts any non-empty port text: without this check a typo would surface as a
// connection error on every reconnect attempt instead of as a start-up failure
// naming the setting.
func brokerAddress(configured string) (string, error) {
	address := configured
	for _, scheme := range brokerSchemes {
		if len(address) > len(scheme) && address[:len(scheme)] == scheme {
			address = address[len(scheme):]
			break
		}
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("app: MQTT_BROKER_URL %q must be host:port", configured)
	}
	if host == "" {
		return "", fmt.Errorf("app: MQTT_BROKER_URL %q must name a host", configured)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", fmt.Errorf("app: MQTT_BROKER_URL %q must use a port between 1 and 65535", configured)
	}
	return address, nil
}
