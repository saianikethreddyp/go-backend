package customer

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
)

// Repository is the persistence port for customers.
type Repository interface {
	GetByPhone(ctx context.Context, phone string) (*User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	Create(ctx context.Context, phone string, name *string) (*User, error)
	UpdateName(ctx context.Context, id uuid.UUID, name string) (*User, error)
	List(ctx context.Context, query string, limit, offset int) ([]User, int, error)
	ContactsSynced(ctx context.Context, id uuid.UUID) (bool, error)
	SetActive(ctx context.Context, id uuid.UUID, active bool) (*User, error)
}

// PgRepository implements Repository on PostgreSQL.
type PgRepository struct{ pool *pgxpool.Pool }

func NewPgRepository(pool *pgxpool.Pool) *PgRepository { return &PgRepository{pool: pool} }

const columns = `id, phone, name, active, contacts_synced_at, created_at`

func scan(row pgx.Row) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Phone, &u.Name, &u.Active, &u.ContactsSyncedAt, &u.CreatedAt); err != nil {
		return nil, dberr.Translate(err)
	}
	return &u, nil
}

func (r *PgRepository) GetByPhone(ctx context.Context, phone string) (*User, error) {
	return scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM users WHERE phone = $1`, phone))
}

func (r *PgRepository) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM users WHERE id = $1`, id))
}

func (r *PgRepository) Create(ctx context.Context, phone string, name *string) (*User, error) {
	return scan(r.pool.QueryRow(ctx,
		`INSERT INTO users (phone, name) VALUES ($1, $2) RETURNING `+columns, phone, name))
}

func (r *PgRepository) UpdateName(ctx context.Context, id uuid.UUID, name string) (*User, error) {
	return scan(r.pool.QueryRow(ctx,
		`UPDATE users SET name = $2, updated_at = now() WHERE id = $1 RETURNING `+columns, id, name))
}

// likeEscape makes user input safe to embed in a LIKE pattern.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// List returns customers whose phone or name contains query.
func (r *PgRepository) List(ctx context.Context, query string, limit, offset int) ([]User, int, error) {
	pattern := ""
	if query != "" {
		pattern = likeEscape(query)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+columns+`, count(*) OVER() AS total
		   FROM users
		  WHERE $1 = '' OR phone LIKE '%' || $1 || '%' OR name ILIKE '%' || $1 || '%'
		  ORDER BY created_at DESC, id
		  LIMIT $2 OFFSET $3`, pattern, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	out, total := []User{}, 0
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Phone, &u.Name, &u.Active, &u.ContactsSyncedAt, &u.CreatedAt, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

func (r *PgRepository) ContactsSynced(ctx context.Context, id uuid.UUID) (bool, error) {
	var synced bool
	err := r.pool.QueryRow(ctx, `SELECT contacts_synced_at IS NOT NULL FROM users WHERE id = $1`, id).Scan(&synced)
	return synced, dberr.Translate(err)
}

// SetActive blocks or unblocks a customer.
func (r *PgRepository) SetActive(ctx context.Context, id uuid.UUID, active bool) (*User, error) {
	return scan(r.pool.QueryRow(ctx,
		`UPDATE users SET active = $2, updated_at = now() WHERE id = $1 RETURNING `+columns, id, active))
}
