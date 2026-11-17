package auth

import (
	"context"
	"net/http"

	"github.com/getsentry/sentry-go"

	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
	"github.com/saianikethreddyp/thandra-api/internal/platform/tokens"
)

type ctxKey struct{}

// WithPrincipal returns a context carrying p.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// PrincipalFrom returns the authenticated principal, if any.
func PrincipalFrom(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(*Principal)
	return p, ok
}

// MustPrincipal returns the principal or a 401 error. Use it in handlers that
// sit behind Authenticate.
func MustPrincipal(r *http.Request) (*Principal, error) {
	p, ok := PrincipalFrom(r.Context())
	if !ok {
		return nil, httpx.Unauthorized("authentication required")
	}
	return p, nil
}

// Middleware authenticates requests using bearer access tokens.
type Middleware struct{ tokens *tokens.Manager }

func NewMiddleware(tm *tokens.Manager) *Middleware { return &Middleware{tokens: tm} }

// Authenticate requires a valid bearer token and stores the principal.
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := httpx.BearerToken(r)
		if !ok {
			httpx.WriteError(w, r, httpx.Unauthorized("missing or malformed bearer token"))
			return
		}
		adminID, err := m.tokens.Parse(token)
		if err != nil {
			httpx.WriteError(w, r, httpx.Unauthorized("invalid or expired token"))
			return
		}
		if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
			hub.Scope().SetUser(sentry.User{ID: adminID.String()}) // id only, no PII
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), &Principal{AdminID: adminID})))
	})
}
