package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
)

var (
	// ErrInvalidRefresh means the refresh token is unknown, expired or revoked.
	ErrInvalidRefresh = errors.New("invalid refresh token")
	// ErrRefreshReuse means an already-used refresh token was presented again,
	// which indicates theft; the whole token family has been revoked.
	ErrRefreshReuse = errors.New("refresh token reuse detected")
)

// Repository is the persistence port for the auth service.
type Repository interface {
	GetByEmail(ctx context.Context, email string) (*Admin, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Admin, error)
	Create(ctx context.Context, a *Admin) (*Admin, error)
	List(ctx context.Context) ([]Admin, error)
	RecordFailedLogin(ctx context.Context, id uuid.UUID, maxAttempts int, lockFor time.Duration) error
	ResetLoginState(ctx context.Context, id uuid.UUID) error
	InsertRefreshToken(ctx context.Context, adminID, familyID uuid.UUID, hash []byte, expiresAt time.Time) error
	RotateRefreshToken(ctx context.Context, oldHash, newHash []byte, newExpiry time.Time) (uuid.UUID, error)
	RevokeFamilyByToken(ctx context.Context, hash []byte) error
}

// PgRepository implements Repository on PostgreSQL.
type PgRepository struct{ pool *pgxpool.Pool }

func NewPgRepository(pool *pgxpool.Pool) *PgRepository { return &PgRepository{pool: pool} }

const adminColumns = `id, email, name, password_hash, active, failed_attempts, locked_until, created_at`

func scanAdmin(row pgx.Row) (*Admin, error) {
	var a Admin
	err := row.Scan(&a.ID, &a.Email, &a.Name, &a.PasswordHash, &a.Active, &a.FailedAttempts, &a.LockedUntil, &a.CreatedAt)
	if err != nil {
		return nil, dberr.Translate(err)
	}
	return &a, nil
}

func (r *PgRepository) GetByEmail(ctx context.Context, email string) (*Admin, error) {
	return scanAdmin(r.pool.QueryRow(ctx, `SELECT `+adminColumns+` FROM admins WHERE email = $1`, email))
}

func (r *PgRepository) GetByID(ctx context.Context, id uuid.UUID) (*Admin, error) {
	return scanAdmin(r.pool.QueryRow(ctx, `SELECT `+adminColumns+` FROM admins WHERE id = $1`, id))
}

func (r *PgRepository) Create(ctx context.Context, a *Admin) (*Admin, error) {
	return scanAdmin(r.pool.QueryRow(ctx,
		`INSERT INTO admins (email, name, password_hash)
		 VALUES ($1, $2, $3)
		 RETURNING `+adminColumns,
		a.Email, a.Name, a.PasswordHash))
}

func (r *PgRepository) List(ctx context.Context) ([]Admin, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+adminColumns+` FROM admins ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}
	defer rows.Close()
	var out []Admin
	for rows.Next() {
		a, err := scanAdmin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// RecordFailedLogin atomically increments the counter and locks the account
// once maxAttempts is reached.
func (r *PgRepository) RecordFailedLogin(ctx context.Context, id uuid.UUID, maxAttempts int, lockFor time.Duration) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE admins
		    SET failed_attempts = failed_attempts + 1,
		        locked_until = CASE WHEN failed_attempts + 1 >= $2 THEN now() + $3::interval ELSE locked_until END,
		        updated_at = now()
		  WHERE id = $1`,
		id, maxAttempts, fmt.Sprintf("%d seconds", int(lockFor.Seconds())))
	return err
}

func (r *PgRepository) ResetLoginState(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE admins SET failed_attempts = 0, locked_until = NULL, updated_at = now() WHERE id = $1`, id)
	return err
}

func (r *PgRepository) InsertRefreshToken(ctx context.Context, adminID, familyID uuid.UUID, hash []byte, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (admin_id, family_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		adminID, familyID, hash, expiresAt)
	return err
}

// RotateRefreshToken atomically consumes oldHash and stores newHash in the same
// family. Presenting an already-used token revokes the whole family.
func (r *PgRepository) RotateRefreshToken(ctx context.Context, oldHash, newHash []byte, newExpiry time.Time) (uuid.UUID, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		adminID, familyID uuid.UUID
		expiresAt         time.Time
		usedAt, revokedAt *time.Time
		active            bool
	)
	err = tx.QueryRow(ctx,
		`SELECT rt.admin_id, rt.family_id, rt.expires_at, rt.used_at, rt.revoked_at, a.active
		   FROM refresh_tokens rt
		   JOIN admins a ON a.id = rt.admin_id
		  WHERE rt.token_hash = $1
		    FOR UPDATE OF rt`, oldHash).Scan(&adminID, &familyID, &expiresAt, &usedAt, &revokedAt, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrInvalidRefresh
	}
	if err != nil {
		return uuid.Nil, err
	}

	if usedAt != nil {
		if _, err := tx.Exec(ctx,
			`UPDATE refresh_tokens SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL`, familyID); err != nil {
			return uuid.Nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, err
		}
		return uuid.Nil, ErrRefreshReuse
	}
	if revokedAt != nil || !expiresAt.After(time.Now()) || !active {
		return uuid.Nil, ErrInvalidRefresh
	}

	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET used_at = now() WHERE token_hash = $1`, oldHash); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (admin_id, family_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		adminID, familyID, newHash, newExpiry); err != nil {
		return uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return adminID, nil
}

func (r *PgRepository) RevokeFamilyByToken(ctx context.Context, hash []byte) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now()
		  WHERE revoked_at IS NULL
		    AND family_id = (SELECT family_id FROM refresh_tokens WHERE token_hash = $1)`, hash)
	return err
}
