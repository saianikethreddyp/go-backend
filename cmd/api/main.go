// Command api runs the Thandra HTTP API.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/saianikethreddyp/thandra-api/internal/audit"
	"github.com/saianikethreddyp/thandra-api/internal/auth"
	"github.com/saianikethreddyp/thandra-api/internal/bookings"
	"github.com/saianikethreddyp/thandra-api/internal/cars"
	"github.com/saianikethreddyp/thandra-api/internal/catalog"
	"github.com/saianikethreddyp/thandra-api/internal/config"
	"github.com/saianikethreddyp/thandra-api/internal/contacts"
	"github.com/saianikethreddyp/thandra-api/internal/customer"
	"github.com/saianikethreddyp/thandra-api/internal/dashboard"
	"github.com/saianikethreddyp/thandra-api/internal/hubs"
	"github.com/saianikethreddyp/thandra-api/internal/jobs"
	"github.com/saianikethreddyp/thandra-api/internal/notifications"
	"github.com/saianikethreddyp/thandra-api/internal/otp"
	"github.com/saianikethreddyp/thandra-api/internal/platform/database"
	"github.com/saianikethreddyp/thandra-api/internal/platform/logger"
	"github.com/saianikethreddyp/thandra-api/internal/platform/notify"
	"github.com/saianikethreddyp/thandra-api/internal/platform/observability"
	"github.com/saianikethreddyp/thandra-api/internal/platform/refreshstore"
	"github.com/saianikethreddyp/thandra-api/internal/platform/storage"
	"github.com/saianikethreddyp/thandra-api/internal/platform/tokens"
	"github.com/saianikethreddyp/thandra-api/internal/platform/vault"
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

	// --- shared services -------------------------------------------------
	adminTokens := tokens.New(cfg.JWTAccessSecret, cfg.AccessTokenTTL, tokens.AudienceAdmin)
	customerTokens := tokens.New(cfg.JWTAccessSecret, cfg.AccessTokenTTL, tokens.AudienceCustomer)
	adminRefresh := refreshstore.New(pool, refreshstore.Admins)
	customerRefresh := refreshstore.New(pool, refreshstore.Customers)

	secrets, err := vault.New(cfg.ContactsMasterKey)
	if err != nil {
		return fmt.Errorf("contacts key: %w", err)
	}

	var photoStore cars.PhotoStore
	if sc := (storage.Config{
		Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, Bucket: cfg.S3Bucket,
		AccessKeyID: cfg.S3AccessKeyID, SecretAccessKey: cfg.S3SecretAccessKey,
	}); sc.Enabled() {
		client, err := storage.New(sc)
		if err != nil {
			return err
		}
		photoStore = client
	} else {
		log.Warn("object storage not configured; photo upload is disabled")
	}
	photos := cars.NewPhotos(photoStore)

	var pushSender notify.Sender = notify.Noop{Log: log}
	if cfg.FirebaseCredentialsB64 != "" {
		raw, err := base64.StdEncoding.DecodeString(cfg.FirebaseCredentialsB64)
		if err != nil {
			return fmt.Errorf("FIREBASE_CREDENTIALS_B64 is not valid base64: %w", err)
		}
		fcm, err := notify.NewFCM(ctx, raw)
		if err != nil {
			return err
		}
		pushSender = fcm
	} else {
		log.Warn("FIREBASE_CREDENTIALS_B64 not set; push notifications are logged, not sent")
	}
	notifier := notifications.NewService(notifications.NewPgRepository(pool), pushSender, log)

	otpService, err := otp.New(pool, otp.ConsoleSender{Log: log}, cfg.JWTAccessSecret, cfg.OTPTTL)
	if err != nil {
		return err
	}

	// --- features --------------------------------------------------------
	adminSvc := auth.NewService(auth.NewPgRepository(pool), adminTokens, adminRefresh, cfg.RefreshTokenTTL)

	customerSvc := customer.NewService(customer.NewPgRepository(pool), otpService, customerTokens, customerRefresh, cfg.RefreshTokenTTL)

	bookingSvc := bookings.NewService(bookings.NewPgRepository(pool), notifier, customerSvc.ContactsSynced, cfg.RequireContactsToBook)

	contactsSvc := contacts.NewService(contacts.NewPgRepository(pool), secrets, bookingSvc, audit.NewLogger(pool), cfg.ContactsRetentionDays)

	handler := server.NewRouter(server.Deps{
		Cfg:  cfg,
		Log:  log,
		Pool: pool,

		AdminMW: auth.NewMiddleware(adminTokens, adminSvc.IsActive),
		Admin:   auth.NewHandler(adminSvc),
		Hubs:    hubs.NewHandler(hubs.NewService(hubs.NewPgRepository(pool))),
		Cars:    cars.NewHandler(cars.NewService(cars.NewPgRepository(pool), photos)),

		CustomerMW:    customer.NewMiddleware(customerTokens, customerSvc.IsActive),
		Customer:      customer.NewHandler(customerSvc),
		Catalog:       catalog.NewHandler(catalog.NewService(pool, photos)),
		Bookings:      bookings.NewHandler(bookingSvc),
		Contacts:      contacts.NewHandler(contactsSvc),
		Notifications: notifications.NewHandler(notifier),
		Dashboard:     dashboard.NewHandler(dashboard.NewService(pool)),
	})

	// Hourly cleanup: expired contacts, stale tokens, old one-time codes.
	jobs.NewRunner(pool, log, cfg.ContactsRetentionDays, adminRefresh, customerRefresh).Start(ctx, time.Hour)

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
	notifier.Wait() // let in-flight push notifications finish
	log.Info("stopped")
	return nil
}
