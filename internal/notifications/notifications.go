// Package notifications registers customer devices and sends them push
// notifications through FCM.
package notifications

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saianikethreddyp/thandra-api/internal/customer"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
	"github.com/saianikethreddyp/thandra-api/internal/platform/notify"
)

const sendTimeout = 15 * time.Second

// Repository stores device tokens.
type Repository interface {
	Upsert(ctx context.Context, userID uuid.UUID, token, platform string) error
	Delete(ctx context.Context, userID uuid.UUID, token string) error
	Tokens(ctx context.Context, userID uuid.UUID) ([]string, error)
	DeleteTokens(ctx context.Context, tokens []string) error
}

// PgRepository implements Repository on PostgreSQL.
type PgRepository struct{ pool *pgxpool.Pool }

func NewPgRepository(pool *pgxpool.Pool) *PgRepository { return &PgRepository{pool: pool} }

// Upsert registers a token for the user. A token that was registered by
// another account (a shared phone) moves to the new owner.
func (r *PgRepository) Upsert(ctx context.Context, userID uuid.UUID, token, platform string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO devices (user_id, fcm_token, platform) VALUES ($1, $2, $3)
		 ON CONFLICT (fcm_token) DO UPDATE SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform, last_seen_at = now()`,
		userID, token, platform)
	return err
}

func (r *PgRepository) Delete(ctx context.Context, userID uuid.UUID, token string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM devices WHERE user_id = $1 AND fcm_token = $2`, userID, token)
	return err
}

func (r *PgRepository) Tokens(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT fcm_token FROM devices WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *PgRepository) DeleteTokens(ctx context.Context, tokens []string) error {
	if len(tokens) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM devices WHERE fcm_token = ANY($1)`, tokens)
	return err
}

// Service registers devices and delivers notifications.
type Service struct {
	repo   Repository
	sender notify.Sender
	log    *slog.Logger
	wg     sync.WaitGroup
}

func NewService(repo Repository, sender notify.Sender, log *slog.Logger) *Service {
	return &Service{repo: repo, sender: sender, log: log}
}

// Register stores a device token for the customer.
func (s *Service) Register(ctx context.Context, userID uuid.UUID, token, platform string) error {
	return s.repo.Upsert(ctx, userID, token, platform)
}

// Unregister removes a device token, for example on logout.
func (s *Service) Unregister(ctx context.Context, userID uuid.UUID, token string) error {
	return s.repo.Delete(ctx, userID, token)
}

// NotifyUser sends a notification to all of the customer's devices in the
// background. Failures are logged and never affect the caller.
func (s *Service) NotifyUser(ctx context.Context, userID uuid.UUID, title, body string, data map[string]string) {
	detached := context.WithoutCancel(ctx)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("push panic", "panic", r)
			}
		}()
		sendCtx, cancel := context.WithTimeout(detached, sendTimeout)
		defer cancel()
		if _, err := s.deliver(sendCtx, userID, title, body, data); err != nil {
			s.log.Error("push failed", "error", err, "user_id", userID)
		}
	}()
}

// Wait blocks until in-flight notifications finish. Call it on shutdown.
func (s *Service) Wait() { s.wg.Wait() }

// deliver sends to every device and prunes tokens FCM reports as invalid. It
// returns how many devices accepted the message.
func (s *Service) deliver(ctx context.Context, userID uuid.UUID, title, body string, data map[string]string) (int, error) {
	tokens, err := s.repo.Tokens(ctx, userID)
	if err != nil {
		return 0, err
	}
	var dead []string
	sent := 0
	for _, t := range tokens {
		err := s.sender.Send(ctx, t, notify.Message{Title: title, Body: body, Data: data})
		switch {
		case err == nil:
			sent++
		case errors.Is(err, notify.ErrUnregistered):
			dead = append(dead, t)
		default:
			s.log.Warn("push send failed", "error", err, "user_id", userID)
		}
	}
	if err := s.repo.DeleteTokens(ctx, dead); err != nil {
		s.log.Warn("prune device tokens failed", "error", err)
	}
	return sent, nil
}

// AdminSend sends a message to one customer and reports how many devices got it.
func (s *Service) AdminSend(ctx context.Context, userID uuid.UUID, title, body string) (int, error) {
	return s.deliver(ctx, userID, title, body, map[string]string{"type": "message"})
}

// Handler exposes the device and notification endpoints.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

type deviceRequest struct {
	FCMToken string `json:"fcm_token" validate:"required,min=20,max=4096"`
	Platform string `json:"platform" validate:"required,oneof=android ios"`
}

type unregisterRequest struct {
	FCMToken string `json:"fcm_token" validate:"required,min=20,max=4096"`
}

type sendRequest struct {
	UserID uuid.UUID `json:"user_id" validate:"required"`
	Title  string    `json:"title" validate:"required,min=1,max=100"`
	Body   string    `json:"body" validate:"required,min=1,max=500"`
}

func (h *Handler) RegisterDevice(w http.ResponseWriter, r *http.Request) error {
	p, err := customer.MustPrincipal(r)
	if err != nil {
		return err
	}
	var req deviceRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.svc.Register(r.Context(), p.UserID, req.FCMToken, req.Platform); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (h *Handler) UnregisterDevice(w http.ResponseWriter, r *http.Request) error {
	p, err := customer.MustPrincipal(r)
	if err != nil {
		return err
	}
	var req unregisterRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.svc.Unregister(r.Context(), p.UserID, req.FCMToken); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// AdminSend handles POST /v1/notifications.
func (h *Handler) AdminSend(w http.ResponseWriter, r *http.Request) error {
	var req sendRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	n, err := h.svc.AdminSend(r.Context(), req.UserID, req.Title, req.Body)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]int{"devices_notified": n})
	return nil
}
