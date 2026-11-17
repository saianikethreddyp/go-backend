package cars

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

// Handler exposes the car endpoints.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

type createRequest struct {
	HubID        *uuid.UUID `json:"hub_id"`
	Brand        string     `json:"brand" validate:"required,min=1,max=60"`
	Model        string     `json:"model" validate:"required,min=1,max=60"`
	Plate        string     `json:"plate" validate:"required,max=20"`
	Year         int        `json:"year" validate:"required,gte=2000,lte=2100"`
	FuelType     string     `json:"fuel_type" validate:"required,oneof=petrol diesel electric cng"`
	Transmission string     `json:"transmission" validate:"required,oneof=manual automatic"`
	Seats        int        `json:"seats" validate:"required,gte=1,lte=20"`
	PricePerDay  int        `json:"price_per_day" validate:"required,gte=1,lte=1000000"`
	Photos       []string   `json:"photos" validate:"omitempty,max=10,dive,https_url,max=500"`
}

type updateRequest struct {
	Brand        *string  `json:"brand" validate:"omitempty,min=1,max=60"`
	Model        *string  `json:"model" validate:"omitempty,min=1,max=60"`
	Plate        *string  `json:"plate" validate:"omitempty,max=20"`
	Year         *int     `json:"year" validate:"omitempty,gte=2000,lte=2100"`
	FuelType     *string  `json:"fuel_type" validate:"omitempty,oneof=petrol diesel electric cng"`
	Transmission *string  `json:"transmission" validate:"omitempty,oneof=manual automatic"`
	Seats        *int     `json:"seats" validate:"omitempty,gte=1,lte=20"`
	PricePerDay  *int     `json:"price_per_day" validate:"omitempty,gte=1,lte=1000000"`
	Photos       []string `json:"photos" validate:"omitempty,max=10,dive,https_url,max=500"`
	Status       *string  `json:"status" validate:"omitempty,oneof=available maintenance retired"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) error {
	limit, offset, err := httpx.Pagination(r)
	if err != nil {
		return err
	}
	hubID, err := httpx.QueryUUID(r, "hub_id")
	if err != nil {
		return err
	}
	f := Filter{HubID: hubID, Limit: limit, Offset: offset}
	if s := r.URL.Query().Get("status"); s != "" {
		switch s {
		case StatusAvailable, StatusMaintenance, StatusRetired:
			f.Status = &s
		default:
			return httpx.BadRequest("status must be one of: available, maintenance, retired")
		}
	}
	list, total, err := h.svc.List(r.Context(), f)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, httpx.Page[Car]{Data: list, Total: total, Limit: limit, Offset: offset})
	return nil
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	c, err := h.svc.Get(r.Context(), id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, c)
	return nil
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) error {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	in := CreateInput{
		Brand: req.Brand, Model: req.Model, Plate: req.Plate, Year: req.Year, FuelType: req.FuelType,
		Transmission: req.Transmission, Seats: req.Seats, PricePerDay: req.PricePerDay, Photos: req.Photos,
	}
	if req.HubID != nil {
		in.HubID = *req.HubID
	}
	c, err := h.svc.Create(r.Context(), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, c)
	return nil
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req updateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	c, err := h.svc.Update(r.Context(), id, UpdateInput{
		Brand: req.Brand, Model: req.Model, Plate: req.Plate, Year: req.Year, FuelType: req.FuelType,
		Transmission: req.Transmission, Seats: req.Seats, PricePerDay: req.PricePerDay, Photos: req.Photos, Status: req.Status,
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, c)
	return nil
}

// Retire handles DELETE: cars are retired, not removed.
func (h *Handler) Retire(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.svc.Retire(r.Context(), id); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}
