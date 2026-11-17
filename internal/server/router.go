// Package server assembles the HTTP router and middleware stack.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saianikethreddyp/thandra-api/internal/auth"
	"github.com/saianikethreddyp/thandra-api/internal/cars"
	"github.com/saianikethreddyp/thandra-api/internal/config"
	"github.com/saianikethreddyp/thandra-api/internal/hubs"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

const requestTimeout = 30 * time.Second

// Deps are the collaborators the router needs.
type Deps struct {
	Cfg  *config.Config
	Log  *slog.Logger
	Pool *pgxpool.Pool

	AuthMW *auth.Middleware
	Auth   *auth.Handler
	Hubs   *hubs.Handler
	Cars   *cars.Handler
}

// NewRouter builds the full HTTP handler.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	// Order matters: request id first so everything below can use it, the
	// recoverer outside sentryhttp so panics are reported once then answered.
	r.Use(middleware.RequestID)
	r.Use(recoverer(d.Log))
	r.Use(sentryhttp.New(sentryhttp.Options{Repanic: true, WaitForDelivery: false, Timeout: 2 * time.Second}).Handle)
	r.Use(tagRequestID)
	r.Use(accessLog(d.Log, d.Cfg.ProxyHops))
	r.Use(securityHeaders(d.Cfg.IsProduction()))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   d.Cfg.CORSOrigins, // empty = no cross-origin browser access
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           600,
	}))
	r.Use(httprate.Limit(d.Cfg.RateLimit, time.Minute,
		httprate.WithKeyFuncs(clientKey(d.Cfg.ProxyHops)), httprate.WithLimitHandler(rateLimited)))
	r.Use(middleware.Timeout(requestTimeout))

	r.NotFound(httpx.Handle(func(http.ResponseWriter, *http.Request) error { return httpx.NotFound("route") }))
	r.MethodNotAllowed(httpx.Handle(func(w http.ResponseWriter, _ *http.Request) error {
		return &httpx.AppError{Status: http.StatusMethodNotAllowed, Code: "method_not_allowed", Message: "method not allowed"}
	}))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", httpx.Handle(func(w http.ResponseWriter, req *http.Request) error {
		ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
		defer cancel()
		if err := d.Pool.Ping(ctx); err != nil {
			return &httpx.AppError{Status: http.StatusServiceUnavailable, Code: "not_ready", Message: "database unavailable", Err: err}
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
		return nil
	}))

	r.Route("/v1", func(v1 chi.Router) {
		v1.Route("/auth", func(a chi.Router) {
			// Stricter per-IP limit on credential endpoints.
			a.Group(func(public chi.Router) {
				public.Use(httprate.Limit(d.Cfg.LoginRateLimt, time.Minute,
					httprate.WithKeyFuncs(clientKey(d.Cfg.ProxyHops)), httprate.WithLimitHandler(rateLimited)))
				public.Post("/login", httpx.Handle(d.Auth.Login))
				public.Post("/refresh", httpx.Handle(d.Auth.Refresh))
				public.Post("/logout", httpx.Handle(d.Auth.Logout))
			})
			a.With(d.AuthMW.Authenticate).Get("/me", httpx.Handle(d.Auth.Me))
		})

		v1.Group(func(admin chi.Router) {
			admin.Use(d.AuthMW.Authenticate)

			admin.Route("/admins", func(ar chi.Router) {
				ar.Get("/", httpx.Handle(d.Auth.ListAdmins))
				ar.Post("/", httpx.Handle(d.Auth.CreateAdmin))
			})

			admin.Route("/hubs", func(hr chi.Router) {
				hr.Get("/", httpx.Handle(d.Hubs.List))
				hr.Post("/", httpx.Handle(d.Hubs.Create))
				hr.Get("/{id}", httpx.Handle(d.Hubs.Get))
				hr.Patch("/{id}", httpx.Handle(d.Hubs.Update))
			})

			admin.Route("/cars", func(cr chi.Router) {
				cr.Get("/", httpx.Handle(d.Cars.List))
				cr.Post("/", httpx.Handle(d.Cars.Create))
				cr.Get("/{id}", httpx.Handle(d.Cars.Get))
				cr.Patch("/{id}", httpx.Handle(d.Cars.Update))
				cr.Delete("/{id}", httpx.Handle(d.Cars.Retire))
			})
		})
	})

	return r
}

// rateLimited answers throttled requests with the standard JSON error shape.
func rateLimited(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "60")
	httpx.WriteError(w, r, &httpx.AppError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "too many requests, slow down"})
}

func clientKey(hops int) func(*http.Request) (string, error) {
	return func(r *http.Request) (string, error) { return ClientIP(r, hops), nil }
}
