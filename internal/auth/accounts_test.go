package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/platform/crypto"
	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
	"github.com/saianikethreddyp/thandra-api/internal/platform/tokens"
)

// memRepo is an in-memory admin store that mimics the SQL rules.
type memRepo struct{ admins map[uuid.UUID]*Admin }

func (m *memRepo) GetByEmail(context.Context, string) (*Admin, error) { return nil, dberr.ErrNotFound }
func (m *memRepo) GetByID(_ context.Context, id uuid.UUID) (*Admin, error) {
	if a, ok := m.admins[id]; ok {
		c := *a
		return &c, nil
	}
	return nil, &dberr.Error{Kind: dberr.ErrNotFound, Err: errors.New("none")}
}
func (m *memRepo) Create(context.Context, *Admin) (*Admin, error) { return nil, nil }
func (m *memRepo) List(context.Context) ([]Admin, error)          { return nil, nil }
func (m *memRepo) RecordLogin(context.Context, uuid.UUID) error   { return nil }
func (m *memRepo) RecordFailedLogin(_ context.Context, id uuid.UUID, _ int, _ time.Duration) error {
	m.admins[id].FailedAttempts++
	return nil
}
func (m *memRepo) SetActive(_ context.Context, id uuid.UUID, active bool) (*Admin, error) {
	a, ok := m.admins[id]
	if !ok {
		return nil, &dberr.Error{Kind: dberr.ErrNotFound, Err: errors.New("none")}
	}
	if !active {
		others := false
		for oid, o := range m.admins {
			if oid != id && o.Active {
				others = true
			}
		}
		if !others {
			return nil, &dberr.Error{Kind: dberr.ErrNotFound, Err: errors.New("last admin")}
		}
	}
	a.Active = active
	c := *a
	return &c, nil
}
func (m *memRepo) SetPassword(_ context.Context, id uuid.UUID, hash string, mustChange bool) error {
	m.admins[id].PasswordHash, m.admins[id].MustChangePassword = hash, mustChange
	return nil
}

type memRefresh struct{ revoked []uuid.UUID }

func (m *memRefresh) Insert(context.Context, uuid.UUID, uuid.UUID, []byte, time.Time) error {
	return nil
}
func (m *memRefresh) Rotate(context.Context, []byte, []byte, time.Time) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (m *memRefresh) RevokeFamily(context.Context, []byte) error { return nil }
func (m *memRefresh) RevokeAll(_ context.Context, id uuid.UUID) error {
	m.revoked = append(m.revoked, id)
	return nil
}

func newAccountsService(t *testing.T, passwords ...string) (*Service, *memRepo, *memRefresh, []uuid.UUID) {
	t.Helper()
	repo, refresh := &memRepo{admins: map[uuid.UUID]*Admin{}}, &memRefresh{}
	var ids []uuid.UUID
	for _, pw := range passwords {
		hash, err := crypto.HashPassword(pw)
		if err != nil {
			t.Fatal(err)
		}
		a := &Admin{ID: uuid.New(), Email: "a@example.com", Active: true, PasswordHash: hash}
		repo.admins[a.ID] = a
		ids = append(ids, a.ID)
	}
	tm := tokens.New("0123456789abcdef0123456789abcdef", time.Minute, tokens.AudienceAdmin)
	return NewService(repo, tm, refresh, time.Hour), repo, refresh, ids
}

func statusOf(err error) int {
	var app *httpx.AppError
	if errors.As(err, &app) {
		return app.Status
	}
	return 0
}

func TestChangePassword(t *testing.T) {
	s, repo, refresh, ids := newAccountsService(t, "old-password-123")
	ctx := context.Background()

	if _, err := s.ChangePassword(ctx, ids[0], "wrong-password!!", "new-password-456"); statusOf(err) != 422 {
		t.Fatalf("wrong current password should be 422, got %v", err)
	}
	if repo.admins[ids[0]].FailedAttempts != 1 {
		t.Fatal("a wrong current password must count as a failed attempt")
	}
	if _, err := s.ChangePassword(ctx, ids[0], "old-password-123", "short"); statusOf(err) != 422 {
		t.Fatalf("weak new password should be 422, got %v", err)
	}
	if _, err := s.ChangePassword(ctx, ids[0], "old-password-123", "old-password-123"); statusOf(err) != 422 {
		t.Fatalf("unchanged password should be 422, got %v", err)
	}

	pair, err := s.ChangePassword(ctx, ids[0], "old-password-123", "new-password-456")
	if err != nil || pair == nil || pair.AccessToken == "" {
		t.Fatalf("change failed: %v", err)
	}
	if len(refresh.revoked) != 1 || refresh.revoked[0] != ids[0] {
		t.Fatal("changing the password must sign out other sessions")
	}
	if ok, _ := crypto.VerifyPassword("new-password-456", repo.admins[ids[0]].PasswordHash); !ok {
		t.Fatal("new password was not stored")
	}
}

func TestDeactivationRules(t *testing.T) {
	s, repo, refresh, ids := newAccountsService(t, "password-one-111", "password-two-222")
	ctx := context.Background()
	a, b := ids[0], ids[1]

	if _, err := s.SetActive(ctx, a, a, false); statusOf(err) != 409 {
		t.Fatalf("deactivating yourself should be 409, got %v", err)
	}
	if _, err := s.SetActive(ctx, a, uuid.New(), false); statusOf(err) != 404 {
		t.Fatalf("unknown admin should be 404, got %v", err)
	}
	if _, err := s.SetActive(ctx, a, b, false); err != nil {
		t.Fatalf("deactivating another admin failed: %v", err)
	}
	if repo.admins[b].Active || len(refresh.revoked) != 1 || refresh.revoked[0] != b {
		t.Fatal("deactivation must disable the account and end its sessions")
	}
	// b is now inactive, so a is the last active admin.
	if _, err := s.SetActive(ctx, b, a, false); statusOf(err) != 409 {
		t.Fatalf("the last active admin must be protected, got %v", err)
	}
	if _, err := s.SetActive(ctx, a, b, true); err != nil || !repo.admins[b].Active {
		t.Fatalf("reactivation failed: %v", err)
	}
}

func TestResetPassword(t *testing.T) {
	s, repo, refresh, ids := newAccountsService(t, "password-one-111", "password-two-222")
	ctx := context.Background()

	if err := s.ResetPassword(ctx, ids[0], ids[0], "temporary-pass-1"); statusOf(err) != 409 {
		t.Fatalf("resetting your own password here should be 409, got %v", err)
	}
	if err := s.ResetPassword(ctx, ids[0], ids[1], "short"); statusOf(err) != 422 {
		t.Fatalf("weak temporary password should be 422, got %v", err)
	}
	if err := s.ResetPassword(ctx, ids[0], uuid.New(), "temporary-pass-1"); statusOf(err) != 404 {
		t.Fatalf("unknown admin should be 404, got %v", err)
	}
	if err := s.ResetPassword(ctx, ids[0], ids[1], "temporary-pass-1"); err != nil {
		t.Fatal(err)
	}
	if !repo.admins[ids[1]].MustChangePassword || len(refresh.revoked) != 1 {
		t.Fatal("reset must force a password change and sign the admin out")
	}
}

func TestIsActive(t *testing.T) {
	s, repo, _, ids := newAccountsService(t, "password-one-111")
	if ok, _ := s.IsActive(context.Background(), ids[0]); !ok {
		t.Fatal("active admin reported inactive")
	}
	repo.admins[ids[0]].Active = false
	if ok, _ := s.IsActive(context.Background(), ids[0]); ok {
		t.Fatal("inactive admin reported active")
	}
	if ok, _ := s.IsActive(context.Background(), uuid.New()); ok {
		t.Fatal("unknown admin reported active")
	}
}
