package bookings

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

const (
	minDuration      = time.Hour
	maxDuration      = 30 * 24 * time.Hour
	maxAdvance       = 90 * 24 * time.Hour
	startGracePeriod = 5 * time.Minute
)

// Notifier sends a push notification to a customer's devices. It must not block.
type Notifier interface {
	NotifyUser(ctx context.Context, userID uuid.UUID, title, body string, data map[string]string)
}

// ContactsGate reports whether a customer has an active contact sync.
type ContactsGate func(ctx context.Context, userID uuid.UUID) (bool, error)

// Service holds the booking business rules.
type Service struct {
	repo            Repository
	notifier        Notifier
	contactsSynced  ContactsGate
	requireContacts bool
	now             func() time.Time
}

// NewService creates a Service. When requireContacts is true a booking is
// refused until the customer has synced their contacts.
func NewService(repo Repository, n Notifier, gate ContactsGate, requireContacts bool) *Service {
	return &Service{repo: repo, notifier: n, contactsSynced: gate, requireContacts: requireContacts, now: time.Now}
}

var (
	errCarUnavailable = &httpx.AppError{Status: 404, Code: "car_unavailable", Message: "car not found or not available for booking"}
	errCarBooked      = &httpx.AppError{Status: 409, Code: "car_booked", Message: "this car is already booked for the selected time"}
	errBadState       = httpx.Conflict("the booking cannot be changed from its current status")
)

func invalid(field, msg string) error {
	return httpx.Validation([]httpx.FieldError{{Field: field, Message: msg}})
}

// billableDays rounds the duration up to whole days.
func billableDays(d time.Duration) int {
	days := int(d / (24 * time.Hour))
	if d%(24*time.Hour) != 0 {
		days++
	}
	if days < 1 {
		days = 1
	}
	return days
}

// Create books a car for the customer.
func (s *Service) Create(ctx context.Context, userID, carID uuid.UUID, start, end time.Time) (*Booking, error) {
	now := s.now()
	switch {
	case start.Before(now.Add(-startGracePeriod)):
		return nil, invalid("start_at", "must not be in the past")
	case start.After(now.Add(maxAdvance)):
		return nil, invalid("start_at", "must be within the next 90 days")
	case !end.After(start):
		return nil, invalid("end_at", "must be after start_at")
	case end.Sub(start) < minDuration:
		return nil, invalid("end_at", "the booking must be at least 1 hour")
	case end.Sub(start) > maxDuration:
		return nil, invalid("end_at", "the booking must be at most 30 days")
	}

	if s.requireContacts {
		synced, err := s.contactsSynced(ctx, userID)
		if err != nil {
			return nil, err
		}
		if !synced {
			return nil, httpx.PreconditionRequired("contacts_required", "contacts access is required before booking a car")
		}
	}

	id, err := s.repo.Create(ctx, CreateInput{
		UserID: userID, CarID: carID, StartAt: start.UTC(), EndAt: end.UTC(), Days: billableDays(end.Sub(start)),
	})
	if err != nil {
		switch {
		case errors.Is(err, dberr.ErrNotFound):
			return nil, errCarUnavailable
		case errors.Is(err, dberr.ErrConflict):
			return nil, errCarBooked
		}
		return nil, err
	}
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.notify(ctx, d, "Booking confirmed",
		fmt.Sprintf("Your %s %s is booked from %s.", d.CarBrand, d.CarModel, d.StartAt.Local().Format("2 Jan, 3:04 PM")))
	return &d.Booking, nil
}

// ListMine returns the customer's bookings, newest first.
func (s *Service) ListMine(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Booking, int, error) {
	details, total, err := s.repo.ListByUser(ctx, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Booking, 0, len(details))
	for _, d := range details {
		out = append(out, d.Booking)
	}
	return out, total, nil
}

// GetMine returns one of the customer's bookings. Other people's bookings look missing.
func (s *Service) GetMine(ctx context.Context, userID, id uuid.UUID) (*Booking, error) {
	d, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if d.UserID != userID {
		return nil, httpx.NotFound("booking")
	}
	return &d.Booking, nil
}

// CancelMine lets a customer cancel a booking that has not started.
func (s *Service) CancelMine(ctx context.Context, userID, id uuid.UUID) (*Booking, error) {
	d, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if d.UserID != userID {
		return nil, httpx.NotFound("booking")
	}
	return s.transition(ctx, d, StatusCancelled, []string{StatusConfirmed},
		"Booking cancelled", fmt.Sprintf("Your %s %s booking was cancelled.", d.CarBrand, d.CarModel))
}

// Lookup returns a booking without ownership checks, for internal callers.
func (s *Service) Lookup(ctx context.Context, id uuid.UUID) (*Booking, error) {
	d, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &d.Booking, nil
}

// HasOpenBooking reports whether the customer has a confirmed or active booking.
func (s *Service) HasOpenBooking(ctx context.Context, userID uuid.UUID) (bool, error) {
	return s.repo.HasOpen(ctx, userID)
}

// AdminList returns bookings for the admin console.
func (s *Service) AdminList(ctx context.Context, f AdminFilter) ([]AdminBooking, int, error) {
	details, total, err := s.repo.ListAll(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	out := make([]AdminBooking, 0, len(details))
	for _, d := range details {
		out = append(out, d.ForAdmin())
	}
	return out, total, nil
}

// AdminGet returns one booking with the customer.
func (s *Service) AdminGet(ctx context.Context, id uuid.UUID) (*AdminBooking, error) {
	d, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	a := d.ForAdmin()
	return &a, nil
}

// AdminStart marks the ride as started (the customer has the car).
func (s *Service) AdminStart(ctx context.Context, id uuid.UUID) (*AdminBooking, error) {
	return s.adminTransition(ctx, id, StatusActive, []string{StatusConfirmed},
		"Ride started", "Enjoy your ride! Drive safe.")
}

// AdminComplete marks the ride as finished.
func (s *Service) AdminComplete(ctx context.Context, id uuid.UUID) (*AdminBooking, error) {
	return s.adminTransition(ctx, id, StatusCompleted, []string{StatusActive},
		"Ride completed", "Thanks for riding with Thandra. You can now delete your shared contacts from the app.")
}

// AdminCancel cancels a booking that has not started.
func (s *Service) AdminCancel(ctx context.Context, id uuid.UUID) (*AdminBooking, error) {
	return s.adminTransition(ctx, id, StatusCancelled, []string{StatusConfirmed},
		"Booking cancelled", "Your booking was cancelled by Thandra. Please contact us for details.")
}

func (s *Service) adminTransition(ctx context.Context, id uuid.UUID, to string, from []string, title, body string) (*AdminBooking, error) {
	d, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	b, err := s.transition(ctx, d, to, from, title, body)
	if err != nil {
		return nil, err
	}
	a := AdminBooking{Booking: *b, Customer: CustomerInfo{Name: d.CustomerName, Phone: d.CustomerPhone}}
	return &a, nil
}

func (s *Service) transition(ctx context.Context, d *Detail, to string, from []string, title, body string) (*Booking, error) {
	ok, err := s.repo.Transition(ctx, d.ID, to, from)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errBadState
	}
	updated, err := s.get(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	s.notify(ctx, updated, title, body)
	return &updated.Booking, nil
}

func (s *Service) get(ctx context.Context, id uuid.UUID) (*Detail, error) {
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			return nil, httpx.NotFound("booking")
		}
		return nil, err
	}
	return d, nil
}

func (s *Service) notify(ctx context.Context, d *Detail, title, body string) {
	if s.notifier == nil {
		return
	}
	s.notifier.NotifyUser(ctx, d.UserID, title, body, map[string]string{
		"type": "booking", "booking_id": d.ID.String(), "status": d.Status,
	})
}
