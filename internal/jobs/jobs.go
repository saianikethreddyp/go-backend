// Package jobs runs periodic cleanup: deleting customer contacts after the
// retention period, and removing expired tokens and one-time codes. Only one
// API instance does the work at a time, guarded by a Postgres advisory lock.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// advisoryLockID is an arbitrary constant identifying this job's lock.
const advisoryLockID int64 = 727001

// TokenCleaner removes unusable refresh tokens.
type TokenCleaner interface {
	DeleteStale(ctx context.Context) (int64, error)
}

// Runner executes the cleanup tasks.
type Runner struct {
	pool          *pgxpool.Pool
	log           *slog.Logger
	retentionDays int
	tokens        []TokenCleaner
}

func NewRunner(pool *pgxpool.Pool, log *slog.Logger, retentionDays int, tokens ...TokenCleaner) *Runner {
	return &Runner{pool: pool, log: log, retentionDays: retentionDays, tokens: tokens}
}

// Start runs the tasks every interval until ctx is cancelled. The first run
// happens after one minute so startup stays fast.
func (r *Runner) Start(ctx context.Context, interval time.Duration) {
	go func() {
		timer := time.NewTimer(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if err := r.RunOnce(ctx); err != nil {
					r.log.Error("cleanup job failed", "error", err)
				}
				timer.Reset(interval)
			}
		}
	}()
}

// RunOnce takes the advisory lock (skipping silently if another instance holds
// it) and runs every task.
func (r *Runner) RunOnce(ctx context.Context) error {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, advisoryLockID).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, advisoryLockID) }()

	n, err := r.DeleteExpiredContacts(ctx)
	if err != nil {
		return fmt.Errorf("delete expired contacts: %w", err)
	}
	var stale int64
	for _, t := range r.tokens {
		c, err := t.DeleteStale(ctx)
		if err != nil {
			return fmt.Errorf("delete stale tokens: %w", err)
		}
		stale += c
	}
	tag, err := conn.Exec(ctx, `DELETE FROM otp_codes WHERE created_at < now() - interval '1 day'`)
	if err != nil {
		return fmt.Errorf("delete old otp codes: %w", err)
	}
	r.log.Info("cleanup finished", "customers_contacts_cleared", n, "stale_tokens", stale, "old_otp_codes", tag.RowsAffected())
	return nil
}

// DeleteExpiredContacts deletes contacts of customers who have no open booking
// and whose last ride finished (or last sync happened) more than the
// retention period ago. It returns how many customers were cleared.
func (r *Runner) DeleteExpiredContacts(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		WITH due AS (
		    SELECT u.id FROM users u
		     WHERE u.contacts_synced_at IS NOT NULL
		       AND NOT EXISTS (SELECT 1 FROM bookings b WHERE b.user_id = u.id AND b.status IN ('confirmed', 'active'))
		       AND COALESCE((SELECT max(b.completed_at) FROM bookings b WHERE b.user_id = u.id AND b.status = 'completed'),
		                    u.contacts_synced_at) < now() - make_interval(days => $1)
		), deleted AS (
		    DELETE FROM user_contacts WHERE user_id IN (SELECT id FROM due) RETURNING user_id
		)
		UPDATE users SET contacts_synced_at = NULL, updated_at = now() WHERE id IN (SELECT id FROM due)`,
		r.retentionDays)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
