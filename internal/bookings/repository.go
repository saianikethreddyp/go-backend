package bookings

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
)

// Repository is the persistence port for bookings.
type Repository interface {
	Create(ctx context.Context, in CreateInput) (uuid.UUID, error)
	Get(ctx context.Context, id uuid.UUID) (*Detail, error)
	ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Detail, int, error)
	ListAll(ctx context.Context, f AdminFilter) ([]Detail, int, error)
	Transition(ctx context.Context, id uuid.UUID, to string, from []string) (bool, error)
	HasOpen(ctx context.Context, userID uuid.UUID) (bool, error)
}

// PgRepository implements Repository on PostgreSQL.
type PgRepository struct{ pool *pgxpool.Pool }

func NewPgRepository(pool *pgxpool.Pool) *PgRepository { return &PgRepository{pool: pool} }

const selectDetail = `
SELECT b.id, b.user_id, b.car_id, b.hub_id, b.start_at, b.end_at, b.status, b.price_per_day, b.total_price,
       b.started_at, b.completed_at, b.cancelled_at, b.created_at,
       c.brand, c.model, c.plate, h.name, h.address, h.latitude, h.longitude,
       u.name, u.phone`

const fromDetail = `
  FROM bookings b
  JOIN cars  c ON c.id = b.car_id
  JOIN hubs  h ON h.id = b.hub_id
  JOIN users u ON u.id = b.user_id`

func scanTargets(d *Detail) []any {
	return []any{
		&d.ID, &d.UserID, &d.CarID, &d.HubID, &d.StartAt, &d.EndAt, &d.Status, &d.PricePerDay, &d.TotalPrice,
		&d.StartedAt, &d.CompletedAt, &d.CancelledAt, &d.CreatedAt,
		&d.CarBrand, &d.CarModel, &d.CarPlate, &d.HubName, &d.HubAddress, &d.HubLatitude, &d.HubLongitude,
		&d.CustomerName, &d.CustomerPhone,
	}
}

// Create inserts a booking for a car that is available and in an active hub.
// It returns dberr.ErrNotFound when the car cannot be booked at all and
// dberr.ErrConflict when the time window overlaps an open booking.
func (r *PgRepository) Create(ctx context.Context, in CreateInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx,
		`INSERT INTO bookings (user_id, car_id, hub_id, start_at, end_at, price_per_day, total_price)
		 SELECT $1, c.id, c.hub_id, $3, $4, c.price_per_day, c.price_per_day * $5
		   FROM cars c JOIN hubs h ON h.id = c.hub_id
		  WHERE c.id = $2 AND c.status = 'available' AND h.active
		 RETURNING id`,
		in.UserID, in.CarID, in.StartAt, in.EndAt, in.Days).Scan(&id)
	if err != nil {
		return uuid.Nil, dberr.Translate(err)
	}
	return id, nil
}

func (r *PgRepository) Get(ctx context.Context, id uuid.UUID) (*Detail, error) {
	var d Detail
	err := r.pool.QueryRow(ctx, selectDetail+fromDetail+` WHERE b.id = $1`, id).Scan(scanTargets(&d)...)
	if err != nil {
		return nil, dberr.Translate(err)
	}
	return &d, nil
}

func (r *PgRepository) list(ctx context.Context, where string, args []any, limit, offset int) ([]Detail, int, error) {
	args = append(args, limit, offset)
	n := len(args)
	rows, err := r.pool.Query(ctx,
		selectDetail+`, count(*) OVER() AS total`+fromDetail+where+
			fmt.Sprintf(` ORDER BY b.created_at DESC, b.id LIMIT $%d OFFSET $%d`, n-1, n), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list bookings: %w", err)
	}
	defer rows.Close()

	out, total := []Detail{}, 0
	for rows.Next() {
		var d Detail
		if err := rows.Scan(append(scanTargets(&d), &total)...); err != nil {
			return nil, 0, fmt.Errorf("scan booking: %w", err)
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

func (r *PgRepository) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Detail, int, error) {
	return r.list(ctx, ` WHERE b.user_id = $1`, []any{userID}, limit, offset)
}

func (r *PgRepository) ListAll(ctx context.Context, f AdminFilter) ([]Detail, int, error) {
	return r.list(ctx,
		` WHERE ($1::uuid IS NULL OR b.hub_id = $1)
		    AND ($2::uuid IS NULL OR b.user_id = $2)
		    AND ($3::text IS NULL OR b.status = $3)
		    AND ($4::timestamptz IS NULL OR b.start_at >= $4)
		    AND ($5::timestamptz IS NULL OR b.start_at <  $5)
		    AND ($6 = '' OR u.phone LIKE '%' || $6 || '%' OR u.name ILIKE '%' || $6 || '%')`,
		[]any{f.HubID, f.UserID, f.Status, f.From, f.To, likeEscape(f.Query)}, f.Limit, f.Offset)
}

// Transition moves a booking to a new status if it currently has one of the
// allowed statuses. It reports whether a row changed.
func (r *PgRepository) Transition(ctx context.Context, id uuid.UUID, to string, from []string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE bookings SET status = $2, updated_at = now(),
		        started_at   = CASE WHEN $2 = 'active'    THEN now() ELSE started_at   END,
		        completed_at = CASE WHEN $2 = 'completed' THEN now() ELSE completed_at END,
		        cancelled_at = CASE WHEN $2 = 'cancelled' THEN now() ELSE cancelled_at END
		  WHERE id = $1 AND status = ANY($3)`, id, to, from)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// HasOpen reports whether the user has a confirmed or active booking.
func (r *PgRepository) HasOpen(ctx context.Context, userID uuid.UUID) (bool, error) {
	var open bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM bookings WHERE user_id = $1 AND status IN ('confirmed', 'active'))`, userID).Scan(&open)
	if err != nil && err != pgx.ErrNoRows {
		return false, err
	}
	return open, nil
}

// likeEscape makes user input safe to embed in a LIKE pattern.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
