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
	"github.com/saianikethreddyp/thandra-api/internal/bookings"
	"github.com/saianikethreddyp/thandra-api/internal/cars"
	"github.com/saianikethreddyp/thandra-api/internal/catalog"
	"github.com/saianikethreddyp/thandra-api/internal/config"
	"github.com/saianikethreddyp/thandra-api/internal/contacts"
	"github.com/saianikethreddyp/thandra-api/internal/customer"
	"github.com/saianikethreddyp/thandra-api/internal/dashboard"
	"github.com/saianikethreddyp/thandra-api/internal/hubs"
	"github.com/saianikethreddyp/thandra-api/internal/notifications"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

const requestTimeout = 30 * time.Second

// Deps are the collaborators the router needs.
type Deps struct {
	Cfg  *config.Config
	Log  *slog.Logger
	Pool *pgxpool.Pool

	AdminMW *auth.Middleware
	Admin   *auth.Handler
	Hubs    *hubs.Handler
	Cars    *cars.Handler

	CustomerMW    *customer.Middleware
	Customer      *customer.Handler
	Catalog       *catalog.Handler
	Bookings      *bookings.Handler
	Contacts      *contacts.Handler
	Notifications *notifications.Handler
	Dashboard     *dashboard.Handler
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

	authLimit := httprate.Limit(d.Cfg.LoginRateLimt, time.Minute,
		httprate.WithKeyFuncs(clientKey(d.Cfg.ProxyHops)), httprate.WithLimitHandler(rateLimited))

	r.Route("/v1", func(v1 chi.Router) {
		// Public catalog: browsing needs no login.
		v1.Route("/public", func(p chi.Router) {
			p.Get("/hubs", httpx.Handle(d.Catalog.Hubs))
			p.Get("/cars", httpx.Handle(d.Catalog.Cars))
			p.Get("/cars/{id}", httpx.Handle(d.Catalog.Car))
		})

		// Customer app.
		v1.Route("/customer", func(c chi.Router) {
			c.Route("/auth", func(a chi.Router) {
				a.Use(authLimit)
				a.Post("/otp/request", httpx.Handle(d.Customer.RequestOTP))
				a.Post("/otp/verify", httpx.Handle(d.Customer.VerifyOTP))
				a.Post("/refresh", httpx.Handle(d.Customer.Refresh))
				a.Post("/logout", httpx.Handle(d.Customer.Logout))
			})

			c.Group(func(authed chi.Router) {
				authed.Use(d.CustomerMW.Authenticate)
				authed.Get("/me", httpx.Handle(d.Customer.Me))
				authed.Patch("/me", httpx.Handle(d.Customer.UpdateMe))

				authed.Route("/bookings", func(b chi.Router) {
					b.Post("/", httpx.Handle(d.Bookings.Create))
					b.Get("/", httpx.Handle(d.Bookings.ListMine))
					b.Get("/{id}", httpx.Handle(d.Bookings.GetMine))
					b.Post("/{id}/cancel", httpx.Handle(d.Bookings.CancelMine))
				})

				authed.Route("/contacts", func(ct chi.Router) {
					ct.Post("/sync", httpx.Handle(d.Contacts.Sync))
					ct.Get("/status", httpx.Handle(d.Contacts.Status))
					ct.Post("/deletion", httpx.Handle(d.Contacts.Delete))
				})

				authed.Route("/devices", func(dv chi.Router) {
					dv.Post("/", httpx.Handle(d.Notifications.RegisterDevice))
					dv.Delete("/", httpx.Handle(d.Notifications.UnregisterDevice))
				})
			})
		})

		// Admin app.
		v1.Route("/auth", func(a chi.Router) {
			a.Group(func(public chi.Router) {
				public.Use(authLimit)
				public.Post("/login", httpx.Handle(d.Admin.Login))
				public.Post("/refresh", httpx.Handle(d.Admin.Refresh))
				public.Post("/logout", httpx.Handle(d.Admin.Logout))
			})
			a.With(d.AdminMW.Authenticate).Get("/me", httpx.Handle(d.Admin.Me))
			a.With(d.AdminMW.Authenticate, authLimit).Post("/change-password", httpx.Handle(d.Admin.ChangePassword))
		})

		v1.Group(func(admin chi.Router) {
			admin.Use(d.AdminMW.Authenticate)

			admin.Route("/admins", func(ar chi.Router) {
				ar.Get("/", httpx.Handle(d.Admin.ListAdmins))
				ar.Post("/", httpx.Handle(d.Admin.CreateAdmin))
				ar.Patch("/{id}", httpx.Handle(d.Admin.SetActive))
				ar.Post("/{id}/reset-password", httpx.Handle(d.Admin.ResetPassword))
			})

			admin.Get("/dashboard", httpx.Handle(d.Dashboard.Get))

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
				cr.Post("/{id}/photos", httpx.Handle(d.Cars.UploadPhoto))
				cr.Delete("/{id}/photos", httpx.Handle(d.Cars.DeletePhoto))
			})

			admin.Route("/users", func(ur chi.Router) {
				ur.Get("/", httpx.Handle(d.Customer.AdminList))
				ur.Get("/{id}", httpx.Handle(d.Customer.AdminGet))
				ur.Patch("/{id}", httpx.Handle(d.Customer.AdminSetActive))
			})

			admin.Route("/bookings", func(br chi.Router) {
				br.Get("/", httpx.Handle(d.Bookings.AdminList))
				br.Get("/{id}", httpx.Handle(d.Bookings.AdminGet))
				br.Post("/{id}/start", httpx.Handle(d.Bookings.AdminStart()))
				br.Post("/{id}/complete", httpx.Handle(d.Bookings.AdminComplete()))
				br.Post("/{id}/cancel", httpx.Handle(d.Bookings.AdminCancel()))
				br.Get("/{id}/contacts", httpx.Handle(d.Contacts.AdminBookingContacts))
			})

			admin.Post("/notifications", httpx.Handle(d.Notifications.AdminSend))
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
