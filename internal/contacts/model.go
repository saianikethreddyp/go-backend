// Package contacts stores a customer's phone contacts for one purpose only:
// tracing a car in case of theft or misuse. Contacts are encrypted at rest,
// deduplicated per customer, deleted on request once a ride is over, and
// deleted automatically after a retention period.
package contacts

import "time"

// Input is one contact sent by the app.
type Input struct {
	Name  string `json:"name" validate:"max=200"`
	Phone string `json:"phone" validate:"required,max=40"`
}

// SyncResult summarizes one sync.
type SyncResult struct {
	Received int `json:"received"`
	Added    int `json:"added"`
	Updated  int `json:"updated"`
	Skipped  int `json:"skipped"` // invalid numbers and in-request duplicates
	Total    int `json:"total"`   // contacts now stored for this customer
}

// Status describes the customer's stored contacts.
type Status struct {
	Synced    bool       `json:"synced"`
	SyncedAt  *time.Time `json:"synced_at"`
	Count     int        `json:"count"`
	CanDelete bool       `json:"can_delete"`
}

// DeleteResult is returned after a deletion request.
type DeleteResult struct {
	Deleted int `json:"deleted"`
}

// Contact is a decrypted contact, shown only to admins.
type Contact struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}
