package customer

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
	"github.com/saianikethreddyp/thandra-api/internal/platform/tokens"
)

type ctxKey struct{}

// PrincipalFrom returns the authenticated customer, if any.
func PrincipalFrom(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(*Principal)
	return p, ok
}

// MustPrincipal returns the customer or a 401 error.
func MustPrincipal(r *http.Request) (*Principal, error) {
	p, ok := PrincipalFrom(r.Context())
	if !ok {
		return nil, httpx.Unauthorized("authentication required")
	}
	return p, nil
}

// activeCacheTTL bounds how long a block can take to apply to a live session.
const activeCacheTTL = 30 * time.Second

type activeEntry struct {
	active bool
	at     time.Time
}

// Middleware authenticates customers with bearer access tokens and rejects
// blocked accounts (checked at most every activeCacheTTL per customer).
type Middleware struct {
	tokens   *tokens.Manager
	isActive func(ctx context.Context, id uuid.UUID) (bool, error)
	cache    sync.Map // uuid.UUID -> activeEntry
}

func NewMiddleware(tm *tokens.Manager, isActive func(ctx context.Context, id uuid.UUID) (bool, error)) *Middleware {
	return &Middleware{tokens: tm, isActive: isActive}
}

func (m *Middleware) active(ctx context.Context, id uuid.UUID) (bool, error) {
	if v, ok := m.cache.Load(id); ok {
		if e := v.(activeEntry); time.Since(e.at) < activeCacheTTL {
			return e.active, nil
		}
	}
	ok, err := m.isActive(ctx, id)
	if err != nil {
		return false, err
	}
	m.cache.Store(id, activeEntry{active: ok, at: time.Now()})
	return ok, nil
}

func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := httpx.BearerToken(r)
		if !ok {
			httpx.WriteError(w, r, httpx.Unauthorized("missing or malformed bearer token"))
			return
		}
		id, err := m.tokens.Parse(token)
		if err != nil {
			httpx.WriteError(w, r, httpx.Unauthorized("invalid or expired token"))
			return
		}
		live, err := m.active(r.Context(), id)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if !live {
			httpx.WriteError(w, r, &httpx.AppError{Status: http.StatusForbidden, Code: "account_disabled", Message: "this account has been disabled"})
			return
		}
		if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
			hub.Scope().SetUser(sentry.User{ID: id.String()}) // id only, no PII
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, &Principal{UserID: id})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Handler exposes the customer endpoints.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

type otpRequest struct {
	Phone  string `json:"phone" validate:"required,max=40"`
	Intent string `json:"intent" validate:"required,oneof=login signup"`
}

type verifyRequest struct {
	Phone  string `json:"phone" validate:"required,max=40"`
	Intent string `json:"intent" validate:"required,oneof=login signup"`
	Code   string `json:"code" validate:"required,len=6"`
	Name   string `json:"name" validate:"max=100"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required,min=20,max=200"`
}

type updateMeRequest struct {
	Name string `json:"name" validate:"required,min=1,max=100"`
}

func (h *Handler) RequestOTP(w http.ResponseWriter, r *http.Request) error {
	var req otpRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.svc.RequestOTP(r.Context(), req.Phone, req.Intent); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "code_sent"})
	return nil
}

func (h *Handler) VerifyOTP(w http.ResponseWriter, r *http.Request) error {
	var req verifyRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	res, err := h.svc.Verify(r.Context(), req.Phone, req.Intent, req.Code, req.Name)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if req.Intent == "signup" {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, res)
	return nil
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) error {
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	pair, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, pair)
	return nil
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) error {
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) error {
	p, err := MustPrincipal(r)
	if err != nil {
		return err
	}
	u, err := h.svc.Me(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, u)
	return nil
}

func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) error {
	p, err := MustPrincipal(r)
	if err != nil {
		return err
	}
	var req updateMeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	u, err := h.svc.UpdateMe(r.Context(), p.UserID, req.Name)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, u)
	return nil
}

// AdminList handles GET /v1/users.
func (h *Handler) AdminList(w http.ResponseWriter, r *http.Request) error {
	limit, offset, err := httpx.Pagination(r)
	if err != nil {
		return err
	}
	users, total, err := h.svc.AdminList(r.Context(), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, httpx.Page[User]{Data: users, Total: total, Limit: limit, Offset: offset})
	return nil
}

// AdminGet handles GET /v1/users/{id}.
func (h *Handler) AdminGet(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	u, err := h.svc.AdminGet(r.Context(), id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, u)
	return nil
}

type setActiveRequest struct {
	Active *bool `json:"active" validate:"required"`
}

// AdminSetActive handles PATCH /v1/users/{id}.
func (h *Handler) AdminSetActive(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req setActiveRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	u, err := h.svc.AdminSetActive(r.Context(), id, *req.Active)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, u)
	return nil
}
