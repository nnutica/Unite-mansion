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

	"unite-mansion/api"
	auditpg "unite-mansion/internal/audit/adapter/postgres"
	auditapp "unite-mansion/internal/audit/app"
	"unite-mansion/internal/platform/config"
	"unite-mansion/internal/platform/health"
	"unite-mansion/internal/platform/httpx"
	"unite-mansion/internal/platform/logging"
	"unite-mansion/internal/platform/postgres"
	"unite-mansion/internal/shared"
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

	// Domain events: every event is written to the audit log in the same tx.
	dispatcher := shared.NewDispatcher()
	dispatcher.SubscribeAll(auditapp.NewRecorder(auditpg.NewRepository(pool)).Handle)

	if cfg.AppEnv != "local" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	if err := router.SetTrustedProxies(nil); err != nil {
		return fmt.Errorf("set trusted proxies: %w", err)
	}
	router.Use(
		httpx.RequestIDMiddleware(),
		httpx.Errors(log /* each context's ErrorMappings go here */),
		httpx.Recovery(log),
	)
	router.NoRoute(httpx.NotFound)

	srv := &server{
		Handler: health.NewHandler(pool),
	}
	strict := api.NewStrictHandlerWithOptions(srv, nil, api.StrictGinServerOptions{
		RequestErrorHandlerFunc:  httpx.RequestError,
		HandlerErrorFunc:         httpx.HandlerError,
		ResponseErrorHandlerFunc: httpx.HandlerError,
	})
	api.RegisterHandlersWithOptions(router, strict, api.GinServerOptions{
		BaseURL:      "/api/v1",
		ErrorHandler: httpx.ParamError,
	})

	httpServer := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           router,
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
