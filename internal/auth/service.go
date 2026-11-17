package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/platform/crypto"
	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
	"github.com/saianikethreddyp/thandra-api/internal/platform/refreshstore"
	"github.com/saianikethreddyp/thandra-api/internal/platform/tokens"
)

const (
	maxFailedLogins = 5
	lockDuration    = 15 * time.Minute

	MinPasswordLen = 12
	MaxPasswordLen = 128
)

// One shared message for every login failure, so responses never reveal
// whether an email exists, is locked or is disabled.
var errInvalidCredentials = httpx.Unauthorized("invalid email or password")
var errInvalidRefreshToken = httpx.Unauthorized("invalid refresh token")

// RefreshStore persists rotating refresh tokens.
type RefreshStore interface {
	Insert(ctx context.Context, ownerID, familyID uuid.UUID, hash []byte, expiresAt time.Time) error
	Rotate(ctx context.Context, oldHash, newHash []byte, newExpiry time.Time) (uuid.UUID, error)
	RevokeFamily(ctx context.Context, hash []byte) error
}

// Service holds the auth business logic.
type Service struct {
	repo       Repository
	tokens     *tokens.Manager
	refresh    RefreshStore
	refreshTTL time.Duration
	now        func() time.Time
}

func NewService(repo Repository, tm *tokens.Manager, refresh RefreshStore, refreshTTL time.Duration) *Service {
	return &Service{repo: repo, tokens: tm, refresh: refresh, refreshTTL: refreshTTL, now: time.Now}
}

// LoginResult is returned by a successful login.
type LoginResult struct {
	Tokens TokenPair     `json:"tokens"`
	Admin  AdminResponse `json:"admin"`
}

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// Login verifies credentials and issues a token pair.
func (s *Service) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	admin, err := s.repo.GetByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			crypto.DummyVerify(password)
			return nil, errInvalidCredentials
		}
		return nil, err
	}

	now := s.now()
	if !admin.Active || (admin.LockedUntil != nil && admin.LockedUntil.After(now)) {
		crypto.DummyVerify(password)
		return nil, errInvalidCredentials
	}

	ok, err := crypto.VerifyPassword(password, admin.PasswordHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		if err := s.repo.RecordFailedLogin(ctx, admin.ID, maxFailedLogins, lockDuration); err != nil {
			slog.ErrorContext(ctx, "record failed login", "error", err)
		}
		return nil, errInvalidCredentials
	}

	if admin.FailedAttempts > 0 || admin.LockedUntil != nil {
		if err := s.repo.ResetLoginState(ctx, admin.ID); err != nil {
			slog.ErrorContext(ctx, "reset login state", "error", err)
		}
	}

	pair, err := s.issueNewFamily(ctx, admin, now)
	if err != nil {
		return nil, err
	}
	return &LoginResult{Tokens: *pair, Admin: admin.Response()}, nil
}

func (s *Service) issueNewFamily(ctx context.Context, admin *Admin, now time.Time) (*TokenPair, error) {
	access, accessExp, err := s.tokens.Issue(admin.ID, now)
	if err != nil {
		return nil, err
	}
	raw, hash, err := refreshstore.NewToken()
	if err != nil {
		return nil, err
	}
	refreshExp := now.Add(s.refreshTTL)
	if err := s.refresh.Insert(ctx, admin.ID, uuid.New(), hash, refreshExp); err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: raw, RefreshExpiresAt: refreshExp}, nil
}

// Refresh rotates a refresh token and returns a new token pair.
func (s *Service) Refresh(ctx context.Context, rawRefresh string) (*TokenPair, error) {
	now := s.now()
	raw, newHash, err := refreshstore.NewToken()
	if err != nil {
		return nil, err
	}
	refreshExp := now.Add(s.refreshTTL)

	adminID, err := s.refresh.Rotate(ctx, refreshstore.Hash(rawRefresh), newHash, refreshExp)
	if err != nil {
		if errors.Is(err, refreshstore.ErrInvalid) || errors.Is(err, refreshstore.ErrReuse) {
			if errors.Is(err, refreshstore.ErrReuse) {
				slog.WarnContext(ctx, "refresh token reuse detected; token family revoked")
			}
			return nil, errInvalidRefreshToken
		}
		return nil, err
	}

	admin, err := s.repo.GetByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	access, accessExp, err := s.tokens.Issue(admin.ID, now)
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: raw, RefreshExpiresAt: refreshExp}, nil
}

// Logout revokes the refresh token family. It is idempotent and never reveals
// whether the token existed.
func (s *Service) Logout(ctx context.Context, rawRefresh string) error {
	return s.refresh.RevokeFamily(ctx, refreshstore.Hash(rawRefresh))
}

// Me returns the current admin.
func (s *Service) Me(ctx context.Context, p *Principal) (*AdminResponse, error) {
	a, err := s.repo.GetByID(ctx, p.AdminID)
	if err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			return nil, httpx.Unauthorized("account no longer exists")
		}
		return nil, err
	}
	r := a.Response()
	return &r, nil
}

// CreateAdminInput is the validated input for creating an admin.
type CreateAdminInput struct {
	Email    string
	Name     string
	Password string
}

// ValidatePassword enforces the password policy.
func ValidatePassword(pw string) error {
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordLen {
		return httpx.Validation([]httpx.FieldError{{Field: "password", Message: "must be at least 12 characters"}})
	}
	if n > MaxPasswordLen {
		return httpx.Validation([]httpx.FieldError{{Field: "password", Message: "must be at most 128 characters"}})
	}
	return nil
}

// CreateAdmin creates a new admin account.
func (s *Service) CreateAdmin(ctx context.Context, in CreateAdminInput) (*AdminResponse, error) {
	if err := ValidatePassword(in.Password); err != nil {
		return nil, err
	}
	hash, err := crypto.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	created, err := s.repo.Create(ctx, &Admin{
		Email: normalizeEmail(in.Email), Name: strings.TrimSpace(in.Name), PasswordHash: hash,
	})
	if err != nil {
		if errors.Is(err, dberr.ErrConflict) {
			return nil, httpx.Conflict("an admin with this email already exists")
		}
		return nil, err
	}
	r := created.Response()
	return &r, nil
}

// ListAdmins returns all admins.
func (s *Service) ListAdmins(ctx context.Context) ([]AdminResponse, error) {
	admins, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AdminResponse, 0, len(admins))
	for i := range admins {
		out = append(out, admins[i].Response())
	}
	return out, nil
}

// SeedAdmin creates the first admin if the email is not taken.
// It returns created=false when the account already exists.
func (s *Service) SeedAdmin(ctx context.Context, email, name, password string) (created bool, err error) {
	if err := ValidatePassword(password); err != nil {
		return false, err
	}
	hash, err := crypto.HashPassword(password)
	if err != nil {
		return false, err
	}
	_, err = s.repo.Create(ctx, &Admin{Email: normalizeEmail(email), Name: strings.TrimSpace(name), PasswordHash: hash})
	if err != nil {
		if errors.Is(err, dberr.ErrConflict) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
