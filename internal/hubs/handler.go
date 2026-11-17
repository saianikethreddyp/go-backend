package hubs

import (
	"net/http"

	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

// Handler exposes the hub endpoints.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

type createRequest struct {
	Name      string   `json:"name" validate:"required,min=2,max=100"`
	Address   string   `json:"address" validate:"required,min=5,max=300"`
	Latitude  *float64 `json:"latitude" validate:"required,gte=-90,lte=90"`
	Longitude *float64 `json:"longitude" validate:"required,gte=-180,lte=180"`
}

type updateRequest struct {
	Name      *string  `json:"name" validate:"omitempty,min=2,max=100"`
	Address   *string  `json:"address" validate:"omitempty,min=5,max=300"`
	Latitude  *float64 `json:"latitude" validate:"omitempty,gte=-90,lte=90"`
	Longitude *float64 `json:"longitude" validate:"omitempty,gte=-180,lte=180"`
	Active    *bool    `json:"active"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) error {
	list, err := h.svc.List(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": list})
	return nil
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	hub, err := h.svc.Get(r.Context(), id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, hub)
	return nil
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) error {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	hub, err := h.svc.Create(r.Context(), CreateInput{
		Name: req.Name, Address: req.Address, Latitude: *req.Latitude, Longitude: *req.Longitude,
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, hub)
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
	hub, err := h.svc.Update(r.Context(), id, UpdateInput{
		Name: req.Name, Address: req.Address, Latitude: req.Latitude, Longitude: req.Longitude, Active: req.Active,
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, hub)
	return nil
}
