// Package hubs manages pickup hubs (Madhapur, LB Nagar, KPHB, ...).
package hubs

import (
	"time"

	"github.com/google/uuid"
)

// Hub is a physical location cars are parked at.
type Hub struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateInput is the validated input for creating a hub.
type CreateInput struct {
	Name      string
	Address   string
	Latitude  float64
	Longitude float64
}

// UpdateInput holds optional changes; nil fields are left untouched.
type UpdateInput struct {
	Name      *string
	Address   *string
	Latitude  *float64
	Longitude *float64
	Active    *bool
}
