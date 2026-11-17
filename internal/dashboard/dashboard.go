// Package dashboard computes the headline numbers for the admin home screen.
// "Today" and "this month" are measured in Indian Standard Time.
package dashboard

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

// Stats is the dashboard payload.
type Stats struct {
	Bookings  BookingStats  `json:"bookings"`
	Customers CustomerStats `json:"customers"`
	Hubs      []HubStats    `json:"hubs"`
}

// BookingStats summarizes bookings.
type BookingStats struct {
	PickupsToday       int `json:"pickups_today"`      // confirmed or active, starting today
	ActiveRides        int `json:"active_rides"`       // currently out
	UpcomingConfirmed  int `json:"upcoming_confirmed"` // confirmed, not yet started
	CompletedThisMonth int `json:"completed_this_month"`
	RevenueThisMonth   int `json:"revenue_this_month"` // INR, completed rides
}

// CustomerStats summarizes customers.
type CustomerStats struct {
	Total        int `json:"total"`
	NewLast7Days int `json:"new_last_7_days"`
}

// HubStats is the fleet status of one hub.
type HubStats struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Active      bool      `json:"active"`
	TotalCars   int       `json:"total_cars"` // not retired
	Available   int       `json:"available"`  // ready to rent, not out right now
	InUse       int       `json:"in_use"`     // on an active ride
	Maintenance int       `json:"maintenance"`
}

// Service reads the numbers.
type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

const bookingSQL = `
WITH bounds AS (
  SELECT (date_trunc('day',   now() AT TIME ZONE 'Asia/Kolkata') AT TIME ZONE 'Asia/Kolkata') AS day_start,
         (date_trunc('month', now() AT TIME ZONE 'Asia/Kolkata') AT TIME ZONE 'Asia/Kolkata') AS month_start
)
SELECT
  count(*) FILTER (WHERE b.status IN ('confirmed', 'active')
                     AND b.start_at >= bounds.day_start AND b.start_at < bounds.day_start + interval '1 day'),
  count(*) FILTER (WHERE b.status = 'active'),
  count(*) FILTER (WHERE b.status = 'confirmed' AND b.start_at >= now()),
  count(*) FILTER (WHERE b.status = 'completed' AND b.completed_at >= bounds.month_start),
  COALESCE(sum(b.total_price) FILTER (WHERE b.status = 'completed' AND b.completed_at >= bounds.month_start), 0)
FROM bookings b CROSS JOIN bounds`

const hubSQL = `
SELECT h.id, h.name, h.active,
       count(c.id) FILTER (WHERE c.status <> 'retired'),
       count(c.id) FILTER (WHERE c.status = 'available' AND ride.car_id IS NULL),
       count(c.id) FILTER (WHERE c.status <> 'retired' AND ride.car_id IS NOT NULL),
       count(c.id) FILTER (WHERE c.status = 'maintenance')
  FROM hubs h
  LEFT JOIN cars c ON c.hub_id = h.id
  LEFT JOIN (SELECT DISTINCT car_id FROM bookings WHERE status = 'active') ride ON ride.car_id = c.id
 GROUP BY h.id
 ORDER BY lower(h.name)`

// Stats gathers all dashboard numbers.
func (s *Service) Stats(ctx context.Context) (*Stats, error) {
	var st Stats
	b := &st.Bookings
	if err := s.pool.QueryRow(ctx, bookingSQL).
		Scan(&b.PickupsToday, &b.ActiveRides, &b.UpcomingConfirmed, &b.CompletedThisMonth, &b.RevenueThisMonth); err != nil {
		return nil, err
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE created_at >= now() - interval '7 days') FROM users`).
		Scan(&st.Customers.Total, &st.Customers.NewLast7Days); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, hubSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	st.Hubs = []HubStats{}
	for rows.Next() {
		var h HubStats
		if err := rows.Scan(&h.ID, &h.Name, &h.Active, &h.TotalCars, &h.Available, &h.InUse, &h.Maintenance); err != nil {
			return nil, err
		}
		st.Hubs = append(st.Hubs, h)
	}
	return &st, rows.Err()
}

// Handler serves GET /v1/dashboard.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) error {
	st, err := h.svc.Stats(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, st)
	return nil
}
