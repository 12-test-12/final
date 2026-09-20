// Command backend runs the lab environment monitoring service.
//
// It wires the MQTT ingress, the persistence layer, the composite fire warning
// evaluator, the control-command service and the REST/WebSocket API together,
// then runs until it receives SIGINT or SIGTERM.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/BobcGn/final/backend/internal/alert"
	"github.com/BobcGn/final/backend/internal/api"
	"github.com/BobcGn/final/backend/internal/command"
	"github.com/BobcGn/final/backend/internal/config"
	"github.com/BobcGn/final/backend/internal/ingest"
	"github.com/BobcGn/final/backend/internal/liveness"
	"github.com/BobcGn/final/backend/internal/store"
)

// shutdownGrace bounds how long in-flight HTTP requests may finish.
const shutdownGrace = 10 * time.Second

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet, so the failure goes to stderr directly.
		fmt.Fprintf(os.Stderr, "backend: %v\n", err)
		os.Exit(1)
	}
}

// run builds the process and blocks until it is asked to stop.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	dataStore, closeStore, err := openStore(ctx, cfg, logger)
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
	hub := api.NewHub(api.Deps{
		Logger:     logger,
		MaxClients: cfg.MaxWebSocketClients,
	})

	// The publisher is created before the broker client, which in turn is created
	// before the ingress service because the client's message handler is the
	// ingress service. The holder breaks that cycle without hiding the failure
	// case: publishing before the client exists reports a broker failure, which
	// is exactly what an operator should see.
	publisher := &brokerPublisher{}

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

	server, err := api.NewServer(api.Config{
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

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// The WebSocket route hijacks the connection, so the write timeout must
		// not be shorter than a realtime client's idle period or the server would
		// abort healthy streams.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	var background sync.WaitGroup
	background.Add(1)
	go func() {
		defer background.Done()
		logger.LogAttrs(ctx, slog.LevelInfo, "http server listening", slog.String("addr", cfg.Addr))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.LogAttrs(ctx, slog.LevelError, "http server stopped", slog.String("error", err.Error()))
			stop()
		}
	}()

	if cfg.UsesBroker() {
		client, err := newBrokerClient(cfg, ingestService, logger)
		if err != nil {
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

// openStore selects the persistence implementation and returns its closer.
func openStore(ctx context.Context, cfg config.Config, logger *slog.Logger) (store.Store, func(), error) {
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
