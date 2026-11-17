// Command api runs the Thandra HTTP API.
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

	"github.com/joho/godotenv"

	"github.com/saianikethreddyp/thandra-api/internal/auth"
	"github.com/saianikethreddyp/thandra-api/internal/cars"
	"github.com/saianikethreddyp/thandra-api/internal/config"
	"github.com/saianikethreddyp/thandra-api/internal/hubs"
	"github.com/saianikethreddyp/thandra-api/internal/platform/database"
	"github.com/saianikethreddyp/thandra-api/internal/platform/logger"
	"github.com/saianikethreddyp/thandra-api/internal/platform/observability"
	"github.com/saianikethreddyp/thandra-api/internal/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load() // local development only; production uses real env vars

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(cfg.IsProduction())
	slog.SetDefault(log)

	flushSentry, err := observability.InitSentry(cfg)
	if err != nil {
		return err
	}
	defer flushSentry()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tokens := auth.NewTokenManager(cfg.JWTAccessSecret, cfg.AccessTokenTTL)
	authSvc := auth.NewService(auth.NewPgRepository(pool), tokens, cfg.RefreshTokenTTL)

	handler := server.NewRouter(server.Deps{
		Cfg:    cfg,
		Log:    log,
		Pool:   pool,
		AuthMW: auth.NewMiddleware(tokens),
		Auth:   auth.NewHandler(authSvc),
		Hubs:   hubs.NewHandler(hubs.NewService(hubs.NewPgRepository(pool))),
		Cars:   cars.NewHandler(cars.NewService(cars.NewPgRepository(pool))),
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", srv.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("stopped")
	return nil
}
