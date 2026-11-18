package hubs

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

// Service holds hub business rules.
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

var errNameTaken = httpx.Conflict("a hub with this name already exists")

// List returns all hubs, including inactive ones.
func (s *Service) List(ctx context.Context) ([]Hub, error) { return s.repo.List(ctx) }

// Get returns one hub.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Hub, error) {
	h, err := s.repo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			return nil, httpx.NotFound("hub")
		}
		return nil, err
	}
	return h, nil
}

// Create adds a hub.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Hub, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Address = strings.TrimSpace(in.Address)
	h, err := s.repo.Create(ctx, in)
	if err != nil {
		if errors.Is(err, dberr.ErrConflict) {
			return nil, errNameTaken
		}
		return nil, err
	}
	return h, nil
}

// Update changes a hub. Latitude and longitude must change together.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Hub, error) {
	if (in.Latitude == nil) != (in.Longitude == nil) {
		return nil, httpx.Validation([]httpx.FieldError{{Field: "latitude", Message: "latitude and longitude must be updated together"}})
	}
	if in.Name != nil {
		t := strings.TrimSpace(*in.Name)
		in.Name = &t
	}
	if in.Address != nil {
		t := strings.TrimSpace(*in.Address)
		in.Address = &t
	}
	h, err := s.repo.Update(ctx, id, in)
	if err != nil {
		switch {
		case errors.Is(err, dberr.ErrNotFound):
			return nil, httpx.NotFound("hub")
		case errors.Is(err, dberr.ErrConflict):
			return nil, errNameTaken
		}
		return nil, err
	}
	return h, nil
}

// Delete permanently removes a hub that was never used. A hub that has (or ever had)
// cars or bookings is kept for history, so the admin must deactivate it instead.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	err := s.repo.Delete(ctx, id)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, dberr.ErrNotFound):
		return httpx.NotFound("hub")
	case errors.Is(err, dberr.ErrInvalidReference):
		return httpx.Conflict("this hub has cars or bookings, so it cannot be deleted. Mark it inactive instead")
	}
	return err
}
