// Command server runs the currency quotes HTTP service.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IlnurShafikov/currency-quotes-service/internal/config"
)

// startupTimeout bounds connecting to the database and applying migrations.
const startupTimeout = 30 * time.Second

func main() {
	// The context is cancelled on Ctrl+C or SIGTERM, which starts a graceful
	// shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Getenv, os.Stdout)

	stop()

	if err != nil {
		fmt.Fprintln(os.Stderr, "currency-quotes-service:", err)
		os.Exit(1)
	}
}

// run starts the service and blocks until ctx is cancelled and the service
// has shut down. It reads the configuration through getenv and writes logs
// to out.
func run(ctx context.Context, getenv func(string) string, out io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	log := slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: cfg.LogLevel}))

	startCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	application, err := newApp(startCtx, cfg, log)

	cancel()

	if err != nil {
		return fmt.Errorf("start: %w", err)
	}

	return application.serve(ctx)
}
