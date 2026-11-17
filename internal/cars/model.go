// Package cars manages the rental car inventory. Every car belongs to a hub.
package cars

import (
	"time"

	"github.com/google/uuid"
)

const (
	StatusAvailable   = "available"
	StatusMaintenance = "maintenance"
	StatusRetired     = "retired"
)

// Car is a rentable vehicle. Whether it is currently booked is derived from
// bookings, not stored here; Status only holds the admin-controlled state.
type Car struct {
	ID           uuid.UUID `json:"id"`
	HubID        uuid.UUID `json:"hub_id"`
	Brand        string    `json:"brand"`
	Model        string    `json:"model"`
	Plate        string    `json:"plate"`
	Year         int       `json:"year"`
	FuelType     string    `json:"fuel_type"`
	Transmission string    `json:"transmission"`
	Seats        int       `json:"seats"`
	PricePerDay  int       `json:"price_per_day"` // INR, whole rupees
	Photos       []string  `json:"photos"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CreateInput is the validated input for adding a car.
type CreateInput struct {
	HubID        uuid.UUID
	Brand        string
	Model        string
	Plate        string
	Year         int
	FuelType     string
	Transmission string
	Seats        int
	PricePerDay  int
	Photos       []string
}

// UpdateInput holds optional changes; nil fields are left untouched.
type UpdateInput struct {
	Brand        *string
	Model        *string
	Plate        *string
	Year         *int
	FuelType     *string
	Transmission *string
	Seats        *int
	PricePerDay  *int
	Photos       []string // nil = unchanged, empty = clear
	Status       *string
}

// Filter narrows a list query.
type Filter struct {
	HubID  *uuid.UUID
	Status *string
	Limit  int
	Offset int
}
