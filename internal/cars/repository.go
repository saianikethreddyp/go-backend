package cars

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
)

// Repository is the persistence port for cars.
type Repository interface {
	List(ctx context.Context, f Filter) ([]Car, int, error)
	Get(ctx context.Context, id uuid.UUID) (*Car, error)
	Create(ctx context.Context, in CreateInput) (*Car, error)
	Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Car, error)
}

// PgRepository implements Repository on PostgreSQL.
type PgRepository struct{ pool *pgxpool.Pool }

func NewPgRepository(pool *pgxpool.Pool) *PgRepository { return &PgRepository{pool: pool} }

const columns = `id, hub_id, brand, model, plate, year, fuel_type, transmission, seats, price_per_day, photos, status, created_at, updated_at`

func scan(row pgx.Row) (*Car, error) {
	var c Car
	if err := row.Scan(&c.ID, &c.HubID, &c.Brand, &c.Model, &c.Plate, &c.Year, &c.FuelType, &c.Transmission,
		&c.Seats, &c.PricePerDay, &c.Photos, &c.Status, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, dberr.Translate(err)
	}
	if c.Photos == nil {
		c.Photos = []string{}
	}
	return &c, nil
}

func (r *PgRepository) List(ctx context.Context, f Filter) ([]Car, int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+columns+`, count(*) OVER() AS total
		   FROM cars
		  WHERE ($1::uuid IS NULL OR hub_id = $1)
		    AND ($2::text IS NULL OR status = $2)
		  ORDER BY created_at DESC, id
		  LIMIT $3 OFFSET $4`, f.HubID, f.Status, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list cars: %w", err)
	}
	defer rows.Close()

	out := []Car{}
	total := 0
	for rows.Next() {
		var c Car
		if err := rows.Scan(&c.ID, &c.HubID, &c.Brand, &c.Model, &c.Plate, &c.Year, &c.FuelType, &c.Transmission,
			&c.Seats, &c.PricePerDay, &c.Photos, &c.Status, &c.CreatedAt, &c.UpdatedAt, &total); err != nil {
			return nil, 0, fmt.Errorf("scan car: %w", err)
		}
		if c.Photos == nil {
			c.Photos = []string{}
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

func (r *PgRepository) Get(ctx context.Context, id uuid.UUID) (*Car, error) {
	return scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM cars WHERE id = $1`, id))
}

// Create inserts a car only if its hub exists and is active; otherwise it
// returns dberr.ErrNotFound.
func (r *PgRepository) Create(ctx context.Context, in CreateInput) (*Car, error) {
	photos := in.Photos
	if photos == nil {
		photos = []string{}
	}
	return scan(r.pool.QueryRow(ctx,
		`INSERT INTO cars (hub_id, brand, model, plate, year, fuel_type, transmission, seats, price_per_day, photos)
		 SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		  WHERE EXISTS (SELECT 1 FROM hubs WHERE id = $1 AND active)
		 RETURNING `+columns,
		in.HubID, in.Brand, in.Model, in.Plate, in.Year, in.FuelType, in.Transmission, in.Seats, in.PricePerDay, photos))
}

func (r *PgRepository) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Car, error) {
	return scan(r.pool.QueryRow(ctx,
		`UPDATE cars SET
		    brand         = COALESCE($2, brand),
		    model         = COALESCE($3, model),
		    plate         = COALESCE($4, plate),
		    year          = COALESCE($5, year),
		    fuel_type     = COALESCE($6, fuel_type),
		    transmission  = COALESCE($7, transmission),
		    seats         = COALESCE($8, seats),
		    price_per_day = COALESCE($9, price_per_day),
		    photos        = COALESCE($10, photos),
		    status        = COALESCE($11, status),
		    updated_at    = now()
		  WHERE id = $1
		  RETURNING `+columns,
		id, in.Brand, in.Model, in.Plate, in.Year, in.FuelType, in.Transmission, in.Seats, in.PricePerDay, in.Photos, in.Status))
}
