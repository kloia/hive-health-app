// HIVE is the warehouse stock service.
//
// Usage:
//
//	hive          run the HTTP server (default; migrates first unless AUTO_MIGRATE=false)
//	hive serve    same as above
//	hive migrate  apply migrations (and seed if SEED_SAMPLE_DATA=true), then exit
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("hive exited with error", "error", err)
		os.Exit(1)
	}
}

func run(args []string, logger *slog.Logger) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	if cmd != "serve" && cmd != "migrate" {
		return fmt.Errorf("unknown command %q (want serve or migrate)", cmd)
	}

	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := openDB(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer db.Close()
	logger.Info("connected to database", "target", dbTarget(cfg.DatabaseURL))

	if cmd == "migrate" {
		return setupDatabase(ctx, db, cfg.SeedSampleData, logger)
	}
	if cfg.AutoMigrate {
		if err := setupDatabase(ctx, db, cfg.SeedSampleData, logger); err != nil {
			return err
		}
	}
	return serve(ctx, cfg, newServer(&pgStore{db: db}, logger), logger)
}

// serve runs the HTTP server until ctx is cancelled (SIGTERM/SIGINT), then
// drains in-flight requests for up to cfg.ShutdownTimeout.
func serve(ctx context.Context, cfg config, s *server, logger *slog.Logger) error {
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("HIVE listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
