// Package customer manages customer accounts: phone-number login with an OTP,
// profile, and the admin-side customer directory.
package customer

import (
	"time"

	"github.com/google/uuid"
)

// User is a customer account.
type User struct {
	ID               uuid.UUID  `json:"id"`
	Phone            string     `json:"phone"`
	Name             *string    `json:"name"`
	Active           bool       `json:"active"`
	ContactsSyncedAt *time.Time `json:"contacts_synced_at"`
	CreatedAt        time.Time  `json:"created_at"`
}

// Principal is the authenticated customer.
type Principal struct {
	UserID uuid.UUID
}

// TokenPair is returned on login and refresh.
type TokenPair struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// LoginResult is returned by a successful verify.
type LoginResult struct {
	Tokens TokenPair `json:"tokens"`
	User   User      `json:"user"`
}
