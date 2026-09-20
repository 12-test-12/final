// Command backend runs the lab environment monitoring service.
//
// The process itself does three things: read the configuration, install a
// signal handler, and hand both to the composition root in internal/app. Keeping
// it that small means the wiring is testable — see internal/app/app_test.go —
// and this file needs no test of its own beyond starting the binary.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/BobcGn/final/backend/internal/app"
	"github.com/BobcGn/final/backend/internal/config"
)

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet, so the failure goes to stderr directly.
		fmt.Fprintf(os.Stderr, "backend: %v\n", err)
		os.Exit(1)
	}
}

// run loads the configuration, installs the signal handler and starts the
// service. It returns an error rather than exiting so the failure path stays
// testable and the exit code is decided in one place.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return app.Run(ctx, app.Options{Config: cfg, Logger: logger})
}
