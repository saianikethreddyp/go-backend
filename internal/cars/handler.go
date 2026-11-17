package cars

import (
	"errors"
	"io"
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
}

type updateRequest struct {
	Brand        *string `json:"brand" validate:"omitempty,min=1,max=60"`
	Model        *string `json:"model" validate:"omitempty,min=1,max=60"`
	Plate        *string `json:"plate" validate:"omitempty,max=20"`
	Year         *int    `json:"year" validate:"omitempty,gte=2000,lte=2100"`
	FuelType     *string `json:"fuel_type" validate:"omitempty,oneof=petrol diesel electric cng"`
	Transmission *string `json:"transmission" validate:"omitempty,oneof=manual automatic"`
	Seats        *int    `json:"seats" validate:"omitempty,gte=1,lte=20"`
	PricePerDay  *int    `json:"price_per_day" validate:"omitempty,gte=1,lte=1000000"`
	Status       *string `json:"status" validate:"omitempty,oneof=available maintenance retired"`
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
		Transmission: req.Transmission, Seats: req.Seats, PricePerDay: req.PricePerDay,
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
		Transmission: req.Transmission, Seats: req.Seats, PricePerDay: req.PricePerDay, Status: req.Status,
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

// UploadPhoto handles POST /v1/cars/{id}/photos (multipart form, field "file").
func (h *Handler) UploadPhoto(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxPhotoBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return httpx.PayloadTooLarge()
		}
		return httpx.BadRequest("expected a multipart form with a file field named \"file\"")
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return httpx.BadRequest("expected a multipart form with a file field named \"file\"")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxPhotoBytes+1))
	if err != nil {
		return httpx.BadRequest("could not read the uploaded file")
	}
	car, err := h.svc.UploadPhoto(r.Context(), id, data)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, car)
	return nil
}

type deletePhotoRequest struct {
	Key string `json:"key" validate:"required,max=300"`
}

// DeletePhoto handles DELETE /v1/cars/{id}/photos with a JSON body {"key": ...}.
func (h *Handler) DeletePhoto(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req deletePhotoRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	car, err := h.svc.DeletePhoto(r.Context(), id, req.Key)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, car)
	return nil
}
