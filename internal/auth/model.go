// Package auth implements admin authentication: password login, short-lived
// access tokens and rotating refresh tokens. There is a single admin level.
package auth

import (
	"time"

	"github.com/google/uuid"
)

// Admin is a staff account. Every admin has full access.
type Admin struct {
	ID                 uuid.UUID
	Email              string
	Name               string
	PasswordHash       string
	Active             bool
	FailedAttempts     int
	LockedUntil        *time.Time
	LastLoginAt        *time.Time
	MustChangePassword bool
	CreatedAt          time.Time
}

// AdminResponse is the public representation of an Admin (no secrets).
type AdminResponse struct {
	ID                 uuid.UUID  `json:"id"`
	Email              string     `json:"email"`
	Name               string     `json:"name"`
	Active             bool       `json:"active"`
	LastLoginAt        *time.Time `json:"last_login_at"`
	MustChangePassword bool       `json:"must_change_password"`
	CreatedAt          time.Time  `json:"created_at"`
}

func (a *Admin) Response() AdminResponse {
	return AdminResponse{
		ID: a.ID, Email: a.Email, Name: a.Name, Active: a.Active,
		LastLoginAt: a.LastLoginAt, MustChangePassword: a.MustChangePassword, CreatedAt: a.CreatedAt,
	}
}

// Principal is the authenticated caller, derived from a verified access token.
type Principal struct {
	AdminID uuid.UUID
}

// TokenPair is returned on login and refresh.
type TokenPair struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}
