package bookings

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

func TestBillableDays(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want int
	}{
		{time.Hour, 1},
		{24 * time.Hour, 1},
		{24*time.Hour + time.Minute, 2},
		{48 * time.Hour, 2},
		{30 * 24 * time.Hour, 30},
	} {
		if got := billableDays(tc.d); got != tc.want {
			t.Fatalf("billableDays(%v) = %d, want %d", tc.d, got, tc.want)
		}
	}
}

// fakeRepo records calls and returns canned results.
type fakeRepo struct {
	createErr error
	created   *CreateInput
	detail    *Detail
}

func (f *fakeRepo) Create(_ context.Context, in CreateInput) (uuid.UUID, error) {
	f.created = &in
	return uuid.New(), f.createErr
}
func (f *fakeRepo) Get(context.Context, uuid.UUID) (*Detail, error) { return f.detail, nil }
func (f *fakeRepo) ListByUser(context.Context, uuid.UUID, int, int) ([]Detail, int, error) {
	return nil, 0, nil
}
func (f *fakeRepo) ListAll(context.Context, AdminFilter) ([]Detail, int, error) { return nil, 0, nil }
func (f *fakeRepo) Transition(context.Context, uuid.UUID, string, []string) (bool, error) {
	return true, nil
}
func (f *fakeRepo) HasOpen(context.Context, uuid.UUID) (bool, error) { return false, nil }

func newTestService(repo *fakeRepo, synced bool, require bool) *Service {
	s := NewService(repo, nil, func(context.Context, uuid.UUID) (bool, error) { return synced, nil }, require)
	s.now = func() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) }
	return s
}

func statusOf(t *testing.T, err error) int {
	t.Helper()
	var app *httpx.AppError
	if !errors.As(err, &app) {
		t.Fatalf("expected AppError, got %v", err)
	}
	return app.Status
}

func TestCreateValidatesTimeWindow(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	s := newTestService(&fakeRepo{detail: &Detail{}}, true, true)
	uid, cid := uuid.New(), uuid.New()

	cases := map[string][2]time.Time{
		"past start":    {now.Add(-time.Hour), now.Add(time.Hour)},
		"end before":    {now.Add(time.Hour), now.Add(30 * time.Minute)},
		"too short":     {now.Add(time.Hour), now.Add(time.Hour + 30*time.Minute)},
		"too long":      {now.Add(time.Hour), now.Add(32 * 24 * time.Hour)},
		"too far ahead": {now.Add(100 * 24 * time.Hour), now.Add(101 * 24 * time.Hour)},
	}
	for name, w := range cases {
		if _, err := s.Create(context.Background(), uid, cid, w[0], w[1]); statusOf(t, err) != 422 {
			t.Fatalf("%s: want 422, got %v", name, err)
		}
	}
}

func TestCreateRequiresContacts(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	s := newTestService(&fakeRepo{detail: &Detail{}}, false, true)
	_, err := s.Create(context.Background(), uuid.New(), uuid.New(), now.Add(time.Hour), now.Add(5*time.Hour))
	if statusOf(t, err) != 428 {
		t.Fatalf("want 428 when contacts are missing, got %v", err)
	}

	// With the requirement switched off the booking goes through.
	repo := &fakeRepo{detail: &Detail{}}
	s = newTestService(repo, false, false)
	if _, err := s.Create(context.Background(), uuid.New(), uuid.New(), now.Add(time.Hour), now.Add(5*time.Hour)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.created == nil || repo.created.Days != 1 {
		t.Fatalf("created = %+v", repo.created)
	}
}

func TestCreateMapsRepositoryErrors(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	start, end := now.Add(time.Hour), now.Add(5*time.Hour)

	s := newTestService(&fakeRepo{createErr: &dberr.Error{Kind: dberr.ErrConflict, Err: errors.New("x")}}, true, true)
	if _, err := s.Create(context.Background(), uuid.New(), uuid.New(), start, end); statusOf(t, err) != 409 {
		t.Fatalf("overlap should be 409, got %v", err)
	}

	s = newTestService(&fakeRepo{createErr: &dberr.Error{Kind: dberr.ErrNotFound, Err: errors.New("x")}}, true, true)
	if _, err := s.Create(context.Background(), uuid.New(), uuid.New(), start, end); statusOf(t, err) != 404 {
		t.Fatalf("unavailable car should be 404, got %v", err)
	}
}

func TestGetMineHidesOtherUsersBookings(t *testing.T) {
	owner := uuid.New()
	s := newTestService(&fakeRepo{detail: &Detail{Booking: Booking{UserID: owner}}}, true, true)
	if _, err := s.GetMine(context.Background(), uuid.New(), uuid.New()); statusOf(t, err) != 404 {
		t.Fatalf("someone else's booking must look missing, got %v", err)
	}
	if _, err := s.GetMine(context.Background(), owner, uuid.New()); err != nil {
		t.Fatalf("owner should see their booking: %v", err)
	}
}
