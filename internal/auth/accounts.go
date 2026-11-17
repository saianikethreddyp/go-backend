package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/platform/crypto"
	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

// IsActive reports whether the admin exists and is enabled. The auth
// middleware calls it on every request so deactivation takes effect at once.
func (s *Service) IsActive(ctx context.Context, id uuid.UUID) (bool, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return a.Active, nil
}

// ChangePassword lets an admin change their own password. All other sessions
// are signed out and a fresh token pair is returned for the current device.
func (s *Service) ChangePassword(ctx context.Context, id uuid.UUID, current, next string) (*TokenPair, error) {
	admin, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			return nil, httpx.Unauthorized("account no longer exists")
		}
		return nil, err
	}
	ok, err := crypto.VerifyPassword(current, admin.PasswordHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		// Count it like a failed login so this form cannot be used to guess the password.
		if err := s.repo.RecordFailedLogin(ctx, id, maxFailedLogins, lockDuration); err != nil {
			return nil, err
		}
		return nil, httpx.Validation([]httpx.FieldError{{Field: "current_password", Message: "is incorrect"}})
	}
	if err := ValidatePassword(next); err != nil {
		return nil, err
	}
	if next == current {
		return nil, httpx.Validation([]httpx.FieldError{{Field: "new_password", Message: "must be different from the current password"}})
	}

	hash, err := crypto.HashPassword(next)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetPassword(ctx, id, hash, false); err != nil {
		return nil, err
	}
	if err := s.refresh.RevokeAll(ctx, id); err != nil {
		return nil, err
	}
	return s.issueNewFamily(ctx, admin, s.now())
}

// SetActive enables or disables another admin. You cannot disable yourself,
// and the last active admin can never be disabled.
func (s *Service) SetActive(ctx context.Context, callerID, id uuid.UUID, active bool) (*AdminResponse, error) {
	if id == callerID && !active {
		return nil, httpx.Conflict("you cannot deactivate your own account")
	}
	updated, err := s.repo.SetActive(ctx, id, active)
	if err != nil {
		if !errors.Is(err, dberr.ErrNotFound) {
			return nil, err
		}
		// No row: either the admin does not exist or it is the last active one.
		if _, getErr := s.repo.GetByID(ctx, id); getErr != nil {
			if errors.Is(getErr, dberr.ErrNotFound) {
				return nil, httpx.NotFound("admin")
			}
			return nil, getErr
		}
		return nil, httpx.Conflict("at least one active admin is required")
	}
	if !active {
		if err := s.refresh.RevokeAll(ctx, id); err != nil {
			return nil, err
		}
	}
	r := updated.Response()
	return &r, nil
}

// ResetPassword sets a temporary password for another admin. They are signed
// out everywhere and are expected to change it at their next login.
func (s *Service) ResetPassword(ctx context.Context, callerID, id uuid.UUID, newPassword string) error {
	if id == callerID {
		return httpx.Conflict("use change password for your own account")
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		if errors.Is(err, dberr.ErrNotFound) {
			return httpx.NotFound("admin")
		}
		return err
	}
	hash, err := crypto.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.repo.SetPassword(ctx, id, hash, true); err != nil {
		return err
	}
	return s.refresh.RevokeAll(ctx, id)
}
