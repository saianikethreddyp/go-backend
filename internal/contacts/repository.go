package contacts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository is the persistence port for contacts.
type Repository interface {
	Upsert(ctx context.Context, userID uuid.UUID, hashes, payloads [][]byte) (added, updated int, err error)
	Count(ctx context.Context, userID uuid.UUID) (int, error)
	SyncedAt(ctx context.Context, userID uuid.UUID) (*time.Time, error)
	DeleteAll(ctx context.Context, userID uuid.UUID, reason *string) (int, error)
	Payloads(ctx context.Context, userID uuid.UUID) ([][]byte, error)
}

// PgRepository implements Repository on PostgreSQL.
type PgRepository struct{ pool *pgxpool.Pool }

func NewPgRepository(pool *pgxpool.Pool) *PgRepository { return &PgRepository{pool: pool} }

// Upsert stores contacts, replacing the payload of ones already stored for the
// same phone hash, and marks the customer as synced. All in one transaction.
func (r *PgRepository) Upsert(ctx context.Context, userID uuid.UUID, hashes, payloads [][]byte) (int, int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx,
		`INSERT INTO user_contacts (user_id, phone_hash, payload, synced_at)
		 SELECT $1, t.h, t.p, now() FROM unnest($2::bytea[], $3::bytea[]) AS t(h, p)
		 ON CONFLICT (user_id, phone_hash) DO UPDATE SET payload = EXCLUDED.payload, synced_at = now()
		 RETURNING (xmax = 0) AS inserted`, userID, hashes, payloads)
	if err != nil {
		return 0, 0, err
	}
	added, updated := 0, 0
	for rows.Next() {
		var inserted bool
		if err := rows.Scan(&inserted); err != nil {
			rows.Close()
			return 0, 0, err
		}
		if inserted {
			added++
		} else {
			updated++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	if _, err := tx.Exec(ctx, `UPDATE users SET contacts_synced_at = now(), updated_at = now() WHERE id = $1`, userID); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return added, updated, nil
}

func (r *PgRepository) Count(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM user_contacts WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

func (r *PgRepository) SyncedAt(ctx context.Context, userID uuid.UUID) (*time.Time, error) {
	var t *time.Time
	err := r.pool.QueryRow(ctx, `SELECT contacts_synced_at FROM users WHERE id = $1`, userID).Scan(&t)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// DeleteAll removes every stored contact of the user, clears the synced flag
// and records the request (without any contact data).
func (r *PgRepository) DeleteAll(ctx context.Context, userID uuid.UUID, reason *string) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `DELETE FROM user_contacts WHERE user_id = $1`, userID)
	if err != nil {
		return 0, err
	}
	n := int(tag.RowsAffected())
	if _, err := tx.Exec(ctx, `UPDATE users SET contacts_synced_at = NULL, updated_at = now() WHERE id = $1`, userID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO contact_deletion_requests (user_id, deleted_count, reason) VALUES ($1, $2, $3)`, userID, n, reason); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return n, nil
}

func (r *PgRepository) Payloads(ctx context.Context, userID uuid.UUID) ([][]byte, error) {
	rows, err := r.pool.Query(ctx, `SELECT payload FROM user_contacts WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var p []byte
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
