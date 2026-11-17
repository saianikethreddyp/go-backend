package bookings

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/customer"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

// Handler exposes the booking endpoints for customers and admins.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

type createRequest struct {
	CarID   uuid.UUID `json:"car_id" validate:"required"`
	StartAt time.Time `json:"start_at" validate:"required"`
	EndAt   time.Time `json:"end_at" validate:"required"`
}

// --- customer ---

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) error {
	p, err := customer.MustPrincipal(r)
	if err != nil {
		return err
	}
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	b, err := h.svc.Create(r.Context(), p.UserID, req.CarID, req.StartAt, req.EndAt)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, b)
	return nil
}

func (h *Handler) ListMine(w http.ResponseWriter, r *http.Request) error {
	p, err := customer.MustPrincipal(r)
	if err != nil {
		return err
	}
	limit, offset, err := httpx.Pagination(r)
	if err != nil {
		return err
	}
	list, total, err := h.svc.ListMine(r.Context(), p.UserID, limit, offset)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, httpx.Page[Booking]{Data: list, Total: total, Limit: limit, Offset: offset})
	return nil
}

func (h *Handler) GetMine(w http.ResponseWriter, r *http.Request) error {
	p, err := customer.MustPrincipal(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	b, err := h.svc.GetMine(r.Context(), p.UserID, id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, b)
	return nil
}

func (h *Handler) CancelMine(w http.ResponseWriter, r *http.Request) error {
	p, err := customer.MustPrincipal(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	b, err := h.svc.CancelMine(r.Context(), p.UserID, id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, b)
	return nil
}

// --- admin ---

func (h *Handler) AdminList(w http.ResponseWriter, r *http.Request) error {
	limit, offset, err := httpx.Pagination(r)
	if err != nil {
		return err
	}
	f := AdminFilter{Limit: limit, Offset: offset}
	if f.HubID, err = httpx.QueryUUID(r, "hub_id"); err != nil {
		return err
	}
	if f.UserID, err = httpx.QueryUUID(r, "user_id"); err != nil {
		return err
	}
	if s := r.URL.Query().Get("status"); s != "" {
		switch s {
		case StatusConfirmed, StatusActive, StatusCompleted, StatusCancelled:
			f.Status = &s
		default:
			return httpx.BadRequest("status must be one of: confirmed, active, completed, cancelled")
		}
	}
	if f.From, err = parseTime(r, "from"); err != nil {
		return err
	}
	if f.To, err = parseTime(r, "to"); err != nil {
		return err
	}
	f.Query = strings.TrimSpace(r.URL.Query().Get("q"))
	if len(f.Query) > 60 {
		return httpx.BadRequest("q is too long")
	}
	list, total, err := h.svc.AdminList(r.Context(), f)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, httpx.Page[AdminBooking]{Data: list, Total: total, Limit: limit, Offset: offset})
	return nil
}

func (h *Handler) AdminGet(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	b, err := h.svc.AdminGet(r.Context(), id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, b)
	return nil
}

func (h *Handler) adminAction(fn func(*Service, *http.Request, uuid.UUID) (*AdminBooking, error)) httpx.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		b, err := fn(h.svc, r, id)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, b)
		return nil
	}
}

func (h *Handler) AdminStart() httpx.HandlerFunc {
	return h.adminAction(func(s *Service, r *http.Request, id uuid.UUID) (*AdminBooking, error) {
		return s.AdminStart(r.Context(), id)
	})
}

func (h *Handler) AdminComplete() httpx.HandlerFunc {
	return h.adminAction(func(s *Service, r *http.Request, id uuid.UUID) (*AdminBooking, error) {
		return s.AdminComplete(r.Context(), id)
	})
}

func (h *Handler) AdminCancel() httpx.HandlerFunc {
	return h.adminAction(func(s *Service, r *http.Request, id uuid.UUID) (*AdminBooking, error) {
		return s.AdminCancel(r.Context(), id)
	})
}

func parseTime(r *http.Request, name string) (*time.Time, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, httpx.BadRequest(name + " must be an RFC 3339 timestamp, for example 2026-12-01T00:00:00+05:30")
	}
	return &t, nil
}
