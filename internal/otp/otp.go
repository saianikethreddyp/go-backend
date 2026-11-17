// Package otp issues and verifies one-time login codes. Codes are stored only
// as an HMAC, expire quickly, allow a few attempts, and are rate limited per
// phone number. Delivery is pluggable through Sender.
package otp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/hkdf"

	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

// Intent says why a code was requested.
const (
	IntentLogin  = "login"
	IntentSignup = "signup"
)

const (
	codeDigits  = 6
	maxAttempts = 5
	cooldown    = 60 * time.Second
	maxPerHour  = 5
)

// Sender delivers a code to a phone number (SMS, WhatsApp, ...).
type Sender interface {
	Send(ctx context.Context, phone, code string) error
}

// ConsoleSender writes the code to the server log. It exists so the flow can be
// developed and tested without an SMS provider and must never run in production.
type ConsoleSender struct{ Log *slog.Logger }

func (c ConsoleSender) Send(ctx context.Context, phone, code string) error {
	c.Log.WarnContext(ctx, "OTP (console sender, development only)", "phone", phone, "code", code)
	return nil
}

// Service stores and checks codes.
type Service struct {
	pool   *pgxpool.Pool
	sender Sender
	key    []byte
	ttl    time.Duration
}

// New creates a Service. The hashing key is derived from secret.
func New(pool *pgxpool.Pool, sender Sender, secret string, ttl time.Duration) (*Service, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, []byte(secret), nil, []byte("thandra/otp/v1")), key); err != nil {
		return nil, fmt.Errorf("derive otp key: %w", err)
	}
	return &Service{pool: pool, sender: sender, key: key, ttl: ttl}, nil
}

func (s *Service) hash(phone, intent, code string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(phone + ":" + intent + ":" + code))
	return m.Sum(nil)
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", codeDigits, n.Int64()), nil
}

// Issue creates a fresh code for phone, invalidating earlier ones, and sends it.
func (s *Service) Issue(ctx context.Context, phone, intent string) error {
	var recent int
	var latest *time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE created_at > now() - interval '1 hour'), max(created_at)
		   FROM otp_codes WHERE phone = $1`, phone).Scan(&recent, &latest); err != nil {
		return err
	}
	if latest != nil && time.Since(*latest) < cooldown {
		return httpx.TooManyRequests("please wait a minute before requesting another code")
	}
	if recent >= maxPerHour {
		return httpx.TooManyRequests("too many codes requested, try again later")
	}

	code, err := newCode()
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE otp_codes SET consumed_at = now() WHERE phone = $1 AND consumed_at IS NULL`, phone); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO otp_codes (phone, intent, code_hash, expires_at)
		 VALUES ($1, $2, $3, now() + make_interval(secs => $4))`,
		phone, intent, s.hash(phone, intent, code), s.ttl.Seconds()); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if err := s.sender.Send(ctx, phone, code); err != nil {
		return &httpx.AppError{Status: 503, Code: "otp_send_failed", Message: "could not send the code, please try again", Err: err}
	}
	return nil
}

// Verify checks code against the newest live code for phone and intent. A
// correct code is consumed. It reports false for wrong, expired, used or
// over-attempted codes, without saying which.
func (s *Service) Verify(ctx context.Context, phone, intent, code string) (bool, error) {
	if len(code) != codeDigits {
		return false, nil
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false, nil
		}
	}
	var ok bool
	err := s.pool.QueryRow(ctx,
		`UPDATE otp_codes
		    SET attempts = attempts + 1,
		        consumed_at = CASE WHEN code_hash = $2 THEN now() END
		  WHERE id = (
		        SELECT id FROM otp_codes
		         WHERE phone = $1 AND intent = $3 AND consumed_at IS NULL
		           AND expires_at > now() AND attempts < $4
		         ORDER BY created_at DESC LIMIT 1
		         FOR UPDATE)
		RETURNING consumed_at IS NOT NULL`,
		phone, s.hash(phone, intent, code), intent, maxAttempts).Scan(&ok)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return ok, nil
}
