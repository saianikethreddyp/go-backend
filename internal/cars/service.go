package cars

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

// Indian registration plates, e.g. TS09EA1234. Spaces and dashes are stripped
// before matching. (BH-series plates are not covered yet.)
var plateRe = regexp.MustCompile(`^[A-Z]{2}[0-9]{1,2}[A-Z]{0,3}[0-9]{4}$`)

// NormalizePlate uppercases and strips spaces and dashes.
func NormalizePlate(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	return strings.NewReplacer(" ", "", "-", "").Replace(s)
}

func validatePlate(p string) error {
	if !plateRe.MatchString(p) {
		return httpx.Validation([]httpx.FieldError{{Field: "plate", Message: "must be a valid registration number like TS09EA1234"}})
	}
	return nil
}

// Service holds car business rules.
type Service struct {
	repo   Repository
	photos *Photos
}

func NewService(repo Repository, photos *Photos) *Service {
	return &Service{repo: repo, photos: photos}
}

var errPlateTaken = httpx.Conflict("a car with this plate already exists")

// List returns cars, optionally filtered.
func (s *Service) List(ctx context.Context, f Filter) ([]Car, int, error) {
	list, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	for i := range list {
		list[i].Photos = s.photos.Resolve(ctx, list[i].PhotoKeys)
	}
	return list, total, nil
}

// Get returns one car.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Car, error) {
	c, err := s.repo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			return nil, httpx.NotFound("car")
		}
		return nil, err
	}
	c.Photos = s.photos.Resolve(ctx, c.PhotoKeys)
	return c, nil
}

// Create adds a car to an active hub.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Car, error) {
	if in.HubID == uuid.Nil {
		return nil, httpx.Validation([]httpx.FieldError{{Field: "hub_id", Message: "is required"}})
	}
	in.Plate = NormalizePlate(in.Plate)
	if err := validatePlate(in.Plate); err != nil {
		return nil, err
	}
	in.Brand, in.Model = strings.TrimSpace(in.Brand), strings.TrimSpace(in.Model)

	c, err := s.repo.Create(ctx, in)
	if err != nil {
		switch {
		case errors.Is(err, dberr.ErrNotFound):
			return nil, httpx.Validation([]httpx.FieldError{{Field: "hub_id", Message: "hub does not exist or is inactive"}})
		case errors.Is(err, dberr.ErrConflict):
			return nil, errPlateTaken
		}
		return nil, err
	}
	c.Photos = s.photos.Resolve(ctx, c.PhotoKeys)
	return c, nil
}

// Update changes a car.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Car, error) {
	if in.Plate != nil {
		n := NormalizePlate(*in.Plate)
		if err := validatePlate(n); err != nil {
			return nil, err
		}
		in.Plate = &n
	}
	if in.Brand != nil {
		t := strings.TrimSpace(*in.Brand)
		in.Brand = &t
	}
	if in.Model != nil {
		t := strings.TrimSpace(*in.Model)
		in.Model = &t
	}
	c, err := s.repo.Update(ctx, id, in)
	if err != nil {
		switch {
		case errors.Is(err, dberr.ErrNotFound):
			return nil, httpx.NotFound("car")
		case errors.Is(err, dberr.ErrConflict):
			return nil, errPlateTaken
		}
		return nil, err
	}
	c.Photos = s.photos.Resolve(ctx, c.PhotoKeys)
	return c, nil
}

// Retire marks a car as retired. Cars are never hard-deleted because past
// bookings reference them.
func (s *Service) Retire(ctx context.Context, id uuid.UUID) error {
	status := StatusRetired
	_, err := s.Update(ctx, id, UpdateInput{Status: &status})
	return err
}
