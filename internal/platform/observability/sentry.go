// Package observability wires error reporting (Sentry).
package observability

import (
	"fmt"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/saianikethreddyp/thandra-api/internal/config"
)

// InitSentry configures Sentry and returns a flush function to call on shutdown.
// With an empty DSN, Sentry runs disabled and every call is a no-op.
func InitSentry(cfg *config.Config) (flush func(), err error) {
	tracesRate := 0.0
	if cfg.IsProduction() {
		tracesRate = 0.1
	}
	err = sentry.Init(sentry.ClientOptions{
		Dsn:              cfg.SentryDSN,
		Environment:      cfg.Env,
		Release:          cfg.Release,
		AttachStacktrace: true,
		SendDefaultPII:   false,
		TracesSampleRate: tracesRate,
		BeforeSend:       scrub,
	})
	if err != nil {
		return func() {}, fmt.Errorf("init sentry: %w", err)
	}
	return func() { sentry.Flush(2 * time.Second) }, nil
}

// scrub removes credentials and request data from events before they leave
// the process. We never want tokens, passwords or contacts in Sentry.
func scrub(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event.Request != nil {
		event.Request.Cookies = ""
		event.Request.Data = ""
		event.Request.QueryString = ""
		for _, h := range []string{"Authorization", "Cookie", "Set-Cookie", "X-Api-Key"} {
			delete(event.Request.Headers, h)
			delete(event.Request.Headers, http.CanonicalHeaderKey(h))
		}
	}
	return event
}
