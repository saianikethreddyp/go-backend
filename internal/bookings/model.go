// Package bookings manages car bookings: creating them with a database
// guarantee against double-booking, history, cancellation and the admin
// ride lifecycle (start, complete).
package bookings

import (
	"time"

	"github.com/google/uuid"
)

// Booking statuses.
const (
	StatusConfirmed = "confirmed"
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
)

// Booking is a reservation of one car for a time window.
type Booking struct {
	ID           uuid.UUID  `json:"id"`
	UserID       uuid.UUID  `json:"user_id"`
	CarID        uuid.UUID  `json:"car_id"`
	HubID        uuid.UUID  `json:"hub_id"`
	StartAt      time.Time  `json:"start_at"`
	EndAt        time.Time  `json:"end_at"`
	Status       string     `json:"status"`
	PricePerDay  int        `json:"price_per_day"` // INR, snapshot at booking time
	TotalPrice   int        `json:"total_price"`   // INR
	StartedAt    *time.Time `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at"`
	CancelledAt  *time.Time `json:"cancelled_at"`
	CreatedAt    time.Time  `json:"created_at"`
	CarBrand     string     `json:"car_brand"`
	CarModel     string     `json:"car_model"`
	CarPlate     string     `json:"car_plate"`
	HubName      string     `json:"hub_name"`
	HubAddress   string     `json:"hub_address"`
	HubLatitude  float64    `json:"hub_latitude"`
	HubLongitude float64    `json:"hub_longitude"`
}

// Detail is a booking together with the customer, for admin views.
type Detail struct {
	Booking
	CustomerName  *string
	CustomerPhone string
}

// CustomerInfo is the customer shown to admins.
type CustomerInfo struct {
	Name  *string `json:"name"`
	Phone string  `json:"phone"`
}

// AdminBooking is a booking as seen by an admin.
type AdminBooking struct {
	Booking
	Customer CustomerInfo `json:"customer"`
}

func (d Detail) ForAdmin() AdminBooking {
	return AdminBooking{Booking: d.Booking, Customer: CustomerInfo{Name: d.CustomerName, Phone: d.CustomerPhone}}
}

// CreateInput is a validated booking request.
type CreateInput struct {
	UserID  uuid.UUID
	CarID   uuid.UUID
	StartAt time.Time
	EndAt   time.Time
	Days    int // billable days, at least 1
}

// AdminFilter narrows the admin booking list.
type AdminFilter struct {
	HubID  *uuid.UUID
	UserID *uuid.UUID
	Status *string
	Limit  int
	Offset int
}
