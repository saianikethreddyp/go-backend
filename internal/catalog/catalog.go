// Package catalog serves the public, read-only view of hubs and cars that the
// customer app browses before logging in. It never exposes plates, internal
// status or photo storage keys.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saianikethreddyp/thandra-api/internal/cars"
	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

// Hub is a pickup location.
type Hub struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
}

// Car is a bookable car.
type Car struct {
	ID           uuid.UUID `json:"id"`
	HubID        uuid.UUID `json:"hub_id"`
	HubName      string    `json:"hub_name"`
	Brand        string    `json:"brand"`
	Model        string    `json:"model"`
	Year         int       `json:"year"`
	FuelType     string    `json:"fuel_type"`
	Transmission string    `json:"transmission"`
	Seats        int       `json:"seats"`
	PricePerDay  int       `json:"price_per_day"`
	Photos       []string  `json:"photos"` // temporary download URLs
}

type row struct {
	Car
	keys []string
}

// Filter narrows the car list. When From and To are set only cars free for
// the whole window are returned.
type Filter struct {
	HubID  *uuid.UUID
	From   *time.Time
	To     *time.Time
	Limit  int
	Offset int
}

// Service reads the catalog.
type Service struct {
	pool   *pgxpool.Pool
	photos *cars.Photos
}

func NewService(pool *pgxpool.Pool, photos *cars.Photos) *Service {
	return &Service{pool: pool, photos: photos}
}

const carColumns = `c.id, c.hub_id, h.name, c.brand, c.model, c.year, c.fuel_type, c.transmission, c.seats, c.price_per_day, c.photos`

func (s *Service) urls(ctx context.Context, keys []string) []string {
	out := []string{}
	for _, p := range s.photos.Resolve(ctx, keys) {
		if p.URL != "" {
			out = append(out, p.URL)
		}
	}
	return out
}

// Hubs returns the active hubs.
func (s *Service) Hubs(ctx context.Context) ([]Hub, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, address, latitude, longitude FROM hubs WHERE active ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Hub{}
	for rows.Next() {
		var h Hub
		if err := rows.Scan(&h.ID, &h.Name, &h.Address, &h.Latitude, &h.Longitude); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Cars returns available cars, optionally for one hub and a time window.
func (s *Service) Cars(ctx context.Context, f Filter) ([]Car, int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+carColumns+`, count(*) OVER()
		   FROM cars c JOIN hubs h ON h.id = c.hub_id
		  WHERE c.status = 'available' AND h.active
		    AND ($1::uuid IS NULL OR c.hub_id = $1)
		    AND ($2::timestamptz IS NULL OR NOT EXISTS (
		          SELECT 1 FROM bookings b
		           WHERE b.car_id = c.id AND b.status IN ('confirmed', 'active')
		             AND tstzrange(b.start_at, b.end_at) && tstzrange($2, $3)))
		  ORDER BY c.price_per_day, c.id
		  LIMIT $4 OFFSET $5`, f.HubID, f.From, f.To, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list catalog cars: %w", err)
	}
	defer rows.Close()

	var raw []row
	total := 0
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.HubID, &r.HubName, &r.Brand, &r.Model, &r.Year, &r.FuelType, &r.Transmission,
			&r.Seats, &r.PricePerDay, &r.keys, &total); err != nil {
			return nil, 0, err
		}
		raw = append(raw, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	out := make([]Car, 0, len(raw))
	for _, r := range raw {
		r.Photos = s.urls(ctx, r.keys)
		out = append(out, r.Car)
	}
	return out, total, nil
}

// Car returns one available car.
func (s *Service) Car(ctx context.Context, id uuid.UUID) (*Car, error) {
	var r row
	err := s.pool.QueryRow(ctx,
		`SELECT `+carColumns+`
		   FROM cars c JOIN hubs h ON h.id = c.hub_id
		  WHERE c.id = $1 AND c.status = 'available' AND h.active`, id).
		Scan(&r.ID, &r.HubID, &r.HubName, &r.Brand, &r.Model, &r.Year, &r.FuelType, &r.Transmission,
			&r.Seats, &r.PricePerDay, &r.keys)
	if err != nil {
		if errors.Is(dberr.Translate(err), dberr.ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.NotFound("car")
		}
		return nil, err
	}
	r.Photos = s.urls(ctx, r.keys)
	return &r.Car, nil
}

// Handler exposes the public catalog.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Hubs(w http.ResponseWriter, r *http.Request) error {
	hubs, err := h.svc.Hubs(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": hubs})
	return nil
}

func (h *Handler) Cars(w http.ResponseWriter, r *http.Request) error {
	limit, offset, err := httpx.Pagination(r)
	if err != nil {
		return err
	}
	f := Filter{Limit: limit, Offset: offset}
	if f.HubID, err = httpx.QueryUUID(r, "hub_id"); err != nil {
		return err
	}
	q := r.URL.Query()
	if q.Get("from") != "" || q.Get("to") != "" {
		from, err1 := time.Parse(time.RFC3339, q.Get("from"))
		to, err2 := time.Parse(time.RFC3339, q.Get("to"))
		if err1 != nil || err2 != nil {
			return httpx.BadRequest("from and to must both be RFC 3339 timestamps, for example 2026-12-01T09:00:00Z")
		}
		if !to.After(from) {
			return httpx.BadRequest("to must be after from")
		}
		f.From, f.To = &from, &to
	}
	list, total, err := h.svc.Cars(r.Context(), f)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, httpx.Page[Car]{Data: list, Total: total, Limit: limit, Offset: offset})
	return nil
}

func (h *Handler) Car(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	c, err := h.svc.Car(r.Context(), id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, c)
	return nil
}
