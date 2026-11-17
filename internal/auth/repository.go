package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
)

// Repository is the persistence port for the auth service.
type Repository interface {
	GetByEmail(ctx context.Context, email string) (*Admin, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Admin, error)
	Create(ctx context.Context, a *Admin) (*Admin, error)
	List(ctx context.Context) ([]Admin, error)
	RecordFailedLogin(ctx context.Context, id uuid.UUID, maxAttempts int, lockFor time.Duration) error
	RecordLogin(ctx context.Context, id uuid.UUID) error
	SetActive(ctx context.Context, id uuid.UUID, active bool) (*Admin, error)
	SetPassword(ctx context.Context, id uuid.UUID, hash string, mustChange bool) error
}

// PgRepository implements Repository on PostgreSQL.
type PgRepository struct{ pool *pgxpool.Pool }

func NewPgRepository(pool *pgxpool.Pool) *PgRepository { return &PgRepository{pool: pool} }

const adminColumns = `id, email, name, password_hash, active, failed_attempts, locked_until, last_login_at, must_change_password, created_at`

func scanAdmin(row pgx.Row) (*Admin, error) {
	var a Admin
	err := row.Scan(&a.ID, &a.Email, &a.Name, &a.PasswordHash, &a.Active, &a.FailedAttempts, &a.LockedUntil, &a.LastLoginAt, &a.MustChangePassword, &a.CreatedAt)
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

// RecordLogin clears the failure counter and stamps the last login time.
func (r *PgRepository) RecordLogin(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE admins SET failed_attempts = 0, locked_until = NULL, last_login_at = now(), updated_at = now() WHERE id = $1`, id)
	return err
}

// SetActive enables or disables an admin. Disabling is refused (no row
// returned) when it would leave no active admin.
func (r *PgRepository) SetActive(ctx context.Context, id uuid.UUID, active bool) (*Admin, error) {
	return scanAdmin(r.pool.QueryRow(ctx,
		`UPDATE admins SET active = $2, updated_at = now()
		  WHERE id = $1
		    AND ($2 OR EXISTS (SELECT 1 FROM admins o WHERE o.id <> $1 AND o.active))
		  RETURNING `+adminColumns, id, active))
}

// SetPassword replaces the password hash and clears any lockout.
func (r *PgRepository) SetPassword(ctx context.Context, id uuid.UUID, hash string, mustChange bool) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE admins SET password_hash = $2, must_change_password = $3, failed_attempts = 0, locked_until = NULL, updated_at = now()
		  WHERE id = $1`, id, hash, mustChange)
	return err
}
