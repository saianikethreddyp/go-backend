package server

import (
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

// ClientIP returns the caller's IP. With hops > 0 it trusts that many proxies
// in front of the app and reads X-Forwarded-For from the right, so a client
// cannot spoof its address by sending its own header. With hops = 0 the header
// is ignored entirely.
func ClientIP(r *http.Request, hops int) string {
	if hops > 0 {
		parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		if idx := len(parts) - hops; idx >= 0 {
			if ip := strings.TrimSpace(parts[idx]); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// recoverer turns panics into a JSON 500. It sits outside sentryhttp, which
// has already reported the panic by the time it propagates here.
func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				log.ErrorContext(r.Context(), "panic recovered",
					"panic", rec, "request_id", middleware.GetReqID(r.Context()), "stack", string(debug.Stack()))
				httpx.JSON(w, http.StatusInternalServerError, map[string]any{
					"error": map[string]any{
						"code": "internal_error", "message": "internal server error",
						"request_id": middleware.GetReqID(r.Context()),
					},
				})
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// tagRequestID attaches the request id to the Sentry scope for correlation.
func tagRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
			hub.Scope().SetTag("request_id", middleware.GetReqID(r.Context()))
		}
		next.ServeHTTP(w, r)
	})
}

// accessLog writes one structured line per request. It logs the path only,
// never the query string or headers, to keep personal data out of logs.
func accessLog(log *slog.Logger, hops int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			log.InfoContext(r.Context(), "request",
				"method", r.Method, "path", r.URL.Path, "status", status,
				"bytes", ww.BytesWritten(), "duration_ms", time.Since(start).Milliseconds(),
				"ip", ClientIP(r, hops), "request_id", middleware.GetReqID(r.Context()))
		})
	}
}

// securityHeaders sets conservative defaults for an API that serves only JSON.
func securityHeaders(production bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("Cache-Control", "no-store")
			if production {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}
