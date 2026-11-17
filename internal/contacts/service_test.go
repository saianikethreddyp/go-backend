package contacts

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/bookings"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
	"github.com/saianikethreddyp/thandra-api/internal/platform/vault"
)

// memRepo is an in-memory Repository that behaves like the SQL one: contacts
// are keyed by (user, phone hash), so a repeated number replaces the old row.
type memRepo struct {
	rows    map[uuid.UUID]map[string][]byte
	deleted bool
}

func newMemRepo() *memRepo { return &memRepo{rows: map[uuid.UUID]map[string][]byte{}} }

func (m *memRepo) Upsert(_ context.Context, u uuid.UUID, hashes, payloads [][]byte) (int, int, error) {
	if m.rows[u] == nil {
		m.rows[u] = map[string][]byte{}
	}
	added, updated := 0, 0
	for i, h := range hashes {
		if _, ok := m.rows[u][string(h)]; ok {
			updated++
		} else {
			added++
		}
		m.rows[u][string(h)] = payloads[i]
	}
	return added, updated, nil
}
func (m *memRepo) Count(_ context.Context, u uuid.UUID) (int, error) { return len(m.rows[u]), nil }
func (m *memRepo) SyncedAt(context.Context, uuid.UUID) (*time.Time, error) {
	return nil, nil
}
func (m *memRepo) DeleteAll(_ context.Context, u uuid.UUID, _ *string) (int, error) {
	n := len(m.rows[u])
	delete(m.rows, u)
	m.deleted = true
	return n, nil
}
func (m *memRepo) Payloads(_ context.Context, u uuid.UUID) ([][]byte, error) {
	var out [][]byte
	for _, p := range m.rows[u] {
		out = append(out, p)
	}
	return out, nil
}

type fakeBookings struct {
	open    bool
	booking *bookings.Booking
}

func (f fakeBookings) Lookup(context.Context, uuid.UUID) (*bookings.Booking, error) {
	return f.booking, nil
}
func (f fakeBookings) HasOpenBooking(context.Context, uuid.UUID) (bool, error) { return f.open, nil }

type fakeAudit struct{ calls int }

func (f *fakeAudit) Record(context.Context, uuid.UUID, string, string, uuid.UUID) error {
	f.calls++
	return nil
}

func testVault(t *testing.T) *vault.Vault {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	v, err := vault.New(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func newService(t *testing.T, b fakeBookings) (*Service, *memRepo, *fakeAudit) {
	repo, a := newMemRepo(), &fakeAudit{}
	s := NewService(repo, testVault(t), b, a, 30)
	s.now = func() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) }
	return s, repo, a
}

func status(err error) int {
	var app *httpx.AppError
	if errors.As(err, &app) {
		return app.Status
	}
	return 0
}

func TestSyncDeduplicatesAcrossSpellingsAndRepeatedSyncs(t *testing.T) {
	s, repo, _ := newService(t, fakeBookings{})
	uid := uuid.New()

	// Three spellings of one number, one other number and one invalid number.
	res, err := s.Sync(context.Background(), uid, []Input{
		{Name: "Mom", Phone: "98765 43210"},
		{Name: "Mom (new)", Phone: "+91-9876543210"},
		{Name: "Mom again", Phone: "09876543210"},
		{Name: "Dad", Phone: "9123456789"},
		{Name: "Junk", Phone: "12345"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 2 || res.Skipped != 3 || res.Total != 2 {
		t.Fatalf("first sync = %+v, want added=2 skipped=3 total=2", res)
	}

	// Booking another car later syncs the same phone book again: no duplicates.
	res, err = s.Sync(context.Background(), uid, []Input{
		{Name: "Mom", Phone: "9876543210"},
		{Name: "Dad", Phone: "9123456789"},
		{Name: "New friend", Phone: "9000000001"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 || res.Updated != 2 || res.Total != 3 {
		t.Fatalf("second sync = %+v, want added=1 updated=2 total=3", res)
	}
	if len(repo.rows[uid]) != 3 {
		t.Fatalf("stored %d contacts, want 3", len(repo.rows[uid]))
	}
}

func TestSyncKeepsCustomersSeparate(t *testing.T) {
	s, repo, _ := newService(t, fakeBookings{})
	a, b := uuid.New(), uuid.New()
	for _, u := range []uuid.UUID{a, b} {
		if _, err := s.Sync(context.Background(), u, []Input{{Name: "Shared friend", Phone: "9876543210"}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(repo.rows[a]) != 1 || len(repo.rows[b]) != 1 {
		t.Fatal("each customer must keep their own copy")
	}
}

func TestSyncRejectsEmptyAndOversized(t *testing.T) {
	s, _, _ := newService(t, fakeBookings{})
	if _, err := s.Sync(context.Background(), uuid.New(), []Input{{Phone: "abc"}}); status(err) != 422 {
		t.Fatalf("no valid numbers should be 422, got %v", err)
	}
	big := make([]Input, MaxContactsPerSync+1)
	if _, err := s.Sync(context.Background(), uuid.New(), big); status(err) != 422 {
		t.Fatalf("too many contacts should be 422, got %v", err)
	}
}

func TestStoredContactsAreEncrypted(t *testing.T) {
	s, repo, _ := newService(t, fakeBookings{})
	uid := uuid.New()
	if _, err := s.Sync(context.Background(), uid, []Input{{Name: "Secret Name", Phone: "9876543210"}}); err != nil {
		t.Fatal(err)
	}
	for _, p := range repo.rows[uid] {
		if strings.Contains(string(p), "Secret Name") || strings.Contains(string(p), "9876543210") {
			t.Fatal("contact data must not be stored in the clear")
		}
	}
}

func TestDeleteBlockedDuringOpenBooking(t *testing.T) {
	s, repo, _ := newService(t, fakeBookings{open: true})
	if _, err := s.Delete(context.Background(), uuid.New(), "done"); status(err) != 409 {
		t.Fatalf("delete during a booking should be 409, got %v", err)
	}
	if repo.deleted {
		t.Fatal("nothing may be deleted while a booking is open")
	}

	s, repo, _ = newService(t, fakeBookings{open: false})
	uid := uuid.New()
	_, _ = s.Sync(context.Background(), uid, []Input{{Name: "A", Phone: "9876543210"}})
	res, err := s.Delete(context.Background(), uid, "ride finished")
	if err != nil || res.Deleted != 1 || len(repo.rows[uid]) != 0 {
		t.Fatalf("delete = %+v, %v", res, err)
	}
}

func TestAdminViewRules(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	recent, old := now.Add(-5*24*time.Hour), now.Add(-40*24*time.Hour)
	uid := uuid.New()

	cases := []struct {
		name    string
		booking bookings.Booking
		allowed bool
	}{
		{"confirmed", bookings.Booking{UserID: uid, Status: bookings.StatusConfirmed}, true},
		{"active", bookings.Booking{UserID: uid, Status: bookings.StatusActive}, true},
		{"completed recently", bookings.Booking{UserID: uid, Status: bookings.StatusCompleted, CompletedAt: &recent}, true},
		{"completed long ago", bookings.Booking{UserID: uid, Status: bookings.StatusCompleted, CompletedAt: &old}, false},
		{"cancelled", bookings.Booking{UserID: uid, Status: bookings.StatusCancelled}, false},
	}
	for _, tc := range cases {
		b := tc.booking
		s, _, audit := newService(t, fakeBookings{booking: &b})
		_, err := s.BookingContacts(context.Background(), uuid.New(), uuid.New())
		if tc.allowed && err != nil {
			t.Fatalf("%s: unexpected error %v", tc.name, err)
		}
		if !tc.allowed && status(err) != 403 {
			t.Fatalf("%s: want 403, got %v", tc.name, err)
		}
		if tc.allowed && audit.calls != 1 {
			t.Fatalf("%s: access must be audited exactly once", tc.name)
		}
		if !tc.allowed && audit.calls != 0 {
			t.Fatalf("%s: denied access must not be logged as a view", tc.name)
		}
	}
}

func TestAdminViewDecryptsContacts(t *testing.T) {
	uid := uuid.New()
	b := bookings.Booking{UserID: uid, Status: bookings.StatusActive}
	s, _, _ := newService(t, fakeBookings{booking: &b})
	if _, err := s.Sync(context.Background(), uid, []Input{{Name: "Mom", Phone: "9876543210"}}); err != nil {
		t.Fatal(err)
	}
	list, err := s.BookingContacts(context.Background(), uuid.New(), uuid.New())
	if err != nil || len(list) != 1 || list[0].Name != "Mom" || list[0].Phone != "+919876543210" {
		t.Fatalf("list = %+v, %v", list, err)
	}
}
