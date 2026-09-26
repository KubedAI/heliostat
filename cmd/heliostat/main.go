// Command heliostat serves the Heliostat console: a read-only view of every Ray workload across
// Kubernetes clusters. Configuration comes from a YAML file (see config/heliostat.yaml) and
// HELIOSTAT_* environment variables.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KubedAI/heliostat/internal/app"
	"github.com/KubedAI/heliostat/internal/config"
	"github.com/KubedAI/heliostat/internal/logging"
	"github.com/KubedAI/heliostat/internal/server"
	"github.com/KubedAI/heliostat/ui"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log := logging.New(os.Getenv("HELIOSTAT_LOG_LEVEL"))
	if err := run(log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cwd, _ := os.Getwd()
	cfg, err := config.Load(os.Getenv, cwd)
	if err != nil {
		return err
	}
	a, err := app.New(cfg, log)
	if err != nil {
		return err
	}
	defer a.Store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go a.Run(ctx)

	addr := os.Getenv("HELIOSTAT_LISTEN")
	if addr == "" {
		addr = ":3000"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(a, ui.FS(), log),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr, "version", version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}
