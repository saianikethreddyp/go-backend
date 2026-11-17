package hubs

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
)

// Repository is the persistence port for hubs.
type Repository interface {
	List(ctx context.Context) ([]Hub, error)
	Get(ctx context.Context, id uuid.UUID) (*Hub, error)
	Create(ctx context.Context, in CreateInput) (*Hub, error)
	Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Hub, error)
}

// PgRepository implements Repository on PostgreSQL.
type PgRepository struct{ pool *pgxpool.Pool }

func NewPgRepository(pool *pgxpool.Pool) *PgRepository { return &PgRepository{pool: pool} }

const columns = `id, name, address, latitude, longitude, active, created_at, updated_at`

func scan(row pgx.Row) (*Hub, error) {
	var h Hub
	if err := row.Scan(&h.ID, &h.Name, &h.Address, &h.Latitude, &h.Longitude, &h.Active, &h.CreatedAt, &h.UpdatedAt); err != nil {
		return nil, dberr.Translate(err)
	}
	return &h, nil
}

// List returns all hubs ordered by name.
func (r *PgRepository) List(ctx context.Context) ([]Hub, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+columns+` FROM hubs ORDER BY lower(name)`)
	if err != nil {
		return nil, fmt.Errorf("list hubs: %w", err)
	}
	defer rows.Close()
	out := []Hub{}
	for rows.Next() {
		h, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

func (r *PgRepository) Get(ctx context.Context, id uuid.UUID) (*Hub, error) {
	return scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM hubs WHERE id = $1`, id))
}

func (r *PgRepository) Create(ctx context.Context, in CreateInput) (*Hub, error) {
	return scan(r.pool.QueryRow(ctx,
		`INSERT INTO hubs (name, address, latitude, longitude) VALUES ($1, $2, $3, $4) RETURNING `+columns,
		in.Name, in.Address, in.Latitude, in.Longitude))
}

func (r *PgRepository) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Hub, error) {
	return scan(r.pool.QueryRow(ctx,
		`UPDATE hubs SET
		    name      = COALESCE($2, name),
		    address   = COALESCE($3, address),
		    latitude  = COALESCE($4, latitude),
		    longitude = COALESCE($5, longitude),
		    active    = COALESCE($6, active),
		    updated_at = now()
		  WHERE id = $1
		  RETURNING `+columns,
		id, in.Name, in.Address, in.Latitude, in.Longitude, in.Active))
}
