// Command api runs the B Mansion HTTP API. It is the only place that wires
// bounded contexts together.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"unite-mansion/internal/platform/config"
	"unite-mansion/internal/platform/logging"
	"unite-mansion/internal/platform/postgres"
	"unite-mansion/internal/platform/storage/s3store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// .env is a local convenience; real environment variables take precedence.
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log := logging.New(cfg.AppEnv)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// RustFS in dev, Cloudflare R2 in UAT: same adapter, different S3_* env.
	files, err := s3store.New(s3store.Config{
		Endpoint:        cfg.S3.Endpoint,
		Region:          cfg.S3.Region,
		Bucket:          cfg.S3.Bucket,
		AccessKeyID:     cfg.S3.AccessKeyID,
		SecretAccessKey: cfg.S3.SecretAccessKey,
		ForcePathStyle:  cfg.S3.ForcePathStyle,
	})
	if err != nil {
		return fmt.Errorf("file storage: %w", err)
	}

	if cfg.AppEnv != "local" {
		gin.SetMode(gin.ReleaseMode)
	}
	app, err := newApplication(deps{
		pool:         pool,
		files:        files,
		log:          log,
		secureCookie: cfg.AppEnv != "local",
		now:          time.Now,
	})
	if err != nil {
		return fmt.Errorf("build application: %w", err)
	}
	// §6.6 PDPA retention: daily, in this process (no external scheduler).
	go app.documents.RunRetention(ctx, 24*time.Hour, log)

	httpServer := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           app.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", httpServer.Addr, "env", cfg.AppEnv)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listen: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
