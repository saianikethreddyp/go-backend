package customer

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/otp"
	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
	"github.com/saianikethreddyp/thandra-api/internal/platform/phone"
	"github.com/saianikethreddyp/thandra-api/internal/platform/refreshstore"
	"github.com/saianikethreddyp/thandra-api/internal/platform/tokens"
)

// OTP is the code service the customer flow depends on.
type OTP interface {
	Issue(ctx context.Context, phone, intent string) error
	Verify(ctx context.Context, phone, intent, code string) (bool, error)
}

// RefreshStore persists rotating refresh tokens.
type RefreshStore interface {
	Insert(ctx context.Context, ownerID, familyID uuid.UUID, hash []byte, expiresAt time.Time) error
	Rotate(ctx context.Context, oldHash, newHash []byte, newExpiry time.Time) (uuid.UUID, error)
	RevokeFamily(ctx context.Context, hash []byte) error
	RevokeAll(ctx context.Context, ownerID uuid.UUID) error
}

var (
	errNoAccount      = &httpx.AppError{Status: 404, Code: "account_not_found", Message: "no account exists for this number, please sign up"}
	errAccountExists  = &httpx.AppError{Status: 409, Code: "account_exists", Message: "an account already exists for this number, please log in"}
	errInvalidCode    = httpx.Unauthorized("invalid or expired code")
	errInvalidRefresh = httpx.Unauthorized("invalid refresh token")
	errBadPhone       = httpx.Validation([]httpx.FieldError{{Field: "phone", Message: "must be a valid phone number"}})
)

// Service holds the customer account logic.
type Service struct {
	repo       Repository
	otp        OTP
	tokens     *tokens.Manager
	refresh    RefreshStore
	refreshTTL time.Duration
	now        func() time.Time
}

func NewService(repo Repository, o OTP, tm *tokens.Manager, refresh RefreshStore, refreshTTL time.Duration) *Service {
	return &Service{repo: repo, otp: o, tokens: tm, refresh: refresh, refreshTTL: refreshTTL, now: time.Now}
}

func normalizePhone(raw string) (string, error) {
	p, ok := phone.Normalize(raw)
	if !ok {
		return "", errBadPhone
	}
	return p, nil
}

func validateName(name string) (string, error) {
	n := strings.TrimSpace(name)
	if l := utf8.RuneCountInString(n); l < 1 || l > 100 {
		return "", httpx.Validation([]httpx.FieldError{{Field: "name", Message: "must be between 1 and 100 characters"}})
	}
	return n, nil
}

// RequestOTP sends a code. Logging in requires an existing account and signing
// up requires that none exists, so the app can steer the user correctly.
func (s *Service) RequestOTP(ctx context.Context, rawPhone, intent string) error {
	p, err := normalizePhone(rawPhone)
	if err != nil {
		return err
	}
	_, err = s.repo.GetByPhone(ctx, p)
	switch {
	case err == nil && intent == otp.IntentSignup:
		return errAccountExists
	case errors.Is(err, dberr.ErrNotFound) && intent == otp.IntentLogin:
		return errNoAccount
	case err != nil && !errors.Is(err, dberr.ErrNotFound):
		return err
	}
	return s.otp.Issue(ctx, p, intent)
}

// Verify checks the code, creating the account on signup, and returns tokens.
func (s *Service) Verify(ctx context.Context, rawPhone, intent, code, name string) (*LoginResult, error) {
	p, err := normalizePhone(rawPhone)
	if err != nil {
		return nil, err
	}
	var cleanName string
	if intent == otp.IntentSignup {
		if cleanName, err = validateName(name); err != nil {
			return nil, err
		}
	}

	ok, err := s.otp.Verify(ctx, p, intent, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errInvalidCode
	}

	var user *User
	if intent == otp.IntentSignup {
		user, err = s.repo.Create(ctx, p, &cleanName)
		if errors.Is(err, dberr.ErrConflict) {
			return nil, errAccountExists
		}
	} else {
		user, err = s.repo.GetByPhone(ctx, p)
		if errors.Is(err, dberr.ErrNotFound) {
			return nil, errNoAccount
		}
	}
	if err != nil {
		return nil, err
	}
	if !user.Active {
		return nil, httpx.Forbidden("this account has been disabled")
	}

	pair, err := s.issue(ctx, user.ID, uuid.New())
	if err != nil {
		return nil, err
	}
	return &LoginResult{Tokens: *pair, User: *user}, nil
}

func (s *Service) issue(ctx context.Context, userID, familyID uuid.UUID) (*TokenPair, error) {
	now := s.now()
	access, accessExp, err := s.tokens.Issue(userID, now)
	if err != nil {
		return nil, err
	}
	raw, hash, err := refreshstore.NewToken()
	if err != nil {
		return nil, err
	}
	exp := now.Add(s.refreshTTL)
	if err := s.refresh.Insert(ctx, userID, familyID, hash, exp); err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: raw, RefreshExpiresAt: exp}, nil
}

// Refresh rotates a refresh token.
func (s *Service) Refresh(ctx context.Context, rawRefresh string) (*TokenPair, error) {
	now := s.now()
	raw, newHash, err := refreshstore.NewToken()
	if err != nil {
		return nil, err
	}
	exp := now.Add(s.refreshTTL)
	userID, err := s.refresh.Rotate(ctx, refreshstore.Hash(rawRefresh), newHash, exp)
	if err != nil {
		if errors.Is(err, refreshstore.ErrInvalid) || errors.Is(err, refreshstore.ErrReuse) {
			return nil, errInvalidRefresh
		}
		return nil, err
	}
	access, accessExp, err := s.tokens.Issue(userID, now)
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: raw, RefreshExpiresAt: exp}, nil
}

// Logout revokes the refresh token family. It is idempotent.
func (s *Service) Logout(ctx context.Context, rawRefresh string) error {
	return s.refresh.RevokeFamily(ctx, refreshstore.Hash(rawRefresh))
}

// Me returns the account.
func (s *Service) Me(ctx context.Context, id uuid.UUID) (*User, error) {
	u, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			return nil, httpx.Unauthorized("account no longer exists")
		}
		return nil, err
	}
	return u, nil
}

// UpdateMe changes the account name.
func (s *Service) UpdateMe(ctx context.Context, id uuid.UUID, name string) (*User, error) {
	n, err := validateName(name)
	if err != nil {
		return nil, err
	}
	u, err := s.repo.UpdateName(ctx, id, n)
	if err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			return nil, httpx.Unauthorized("account no longer exists")
		}
		return nil, err
	}
	return u, nil
}

// AdminList returns customers for the admin directory.
func (s *Service) AdminList(ctx context.Context, query string, limit, offset int) ([]User, int, error) {
	q := strings.TrimSpace(query)
	// A full phone number in any common spelling is matched exactly as E.164.
	if p, ok := phone.Normalize(q); ok {
		q = p
	}
	return s.repo.List(ctx, q, limit, offset)
}

// AdminGet returns one customer.
func (s *Service) AdminGet(ctx context.Context, id uuid.UUID) (*User, error) {
	u, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			return nil, httpx.NotFound("customer")
		}
		return nil, err
	}
	return u, nil
}

// ContactsSynced reports whether the customer has an active contact sync.
func (s *Service) ContactsSynced(ctx context.Context, id uuid.UUID) (bool, error) {
	return s.repo.ContactsSynced(ctx, id)
}

// AdminSetActive blocks or unblocks a customer. Blocking also ends all of
// their sessions; existing bookings are left untouched.
func (s *Service) AdminSetActive(ctx context.Context, id uuid.UUID, active bool) (*User, error) {
	u, err := s.repo.SetActive(ctx, id, active)
	if err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			return nil, httpx.NotFound("customer")
		}
		return nil, err
	}
	if !active {
		if err := s.refresh.RevokeAll(ctx, id); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// IsActive reports whether the customer exists and is not blocked.
func (s *Service) IsActive(ctx context.Context, id uuid.UUID) (bool, error) {
	u, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return u.Active, nil
}
