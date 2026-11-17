package contacts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/bookings"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
	"github.com/saianikethreddyp/thandra-api/internal/platform/phone"
	"github.com/saianikethreddyp/thandra-api/internal/platform/vault"
)

const (
	MaxContactsPerSync = 10000
	maxNameRunes       = 100
)

// Bookings is the part of the booking service contacts depends on.
type Bookings interface {
	Lookup(ctx context.Context, id uuid.UUID) (*bookings.Booking, error)
	HasOpenBooking(ctx context.Context, userID uuid.UUID) (bool, error)
}

// Auditor records admin access to contacts.
type Auditor interface {
	Record(ctx context.Context, adminID uuid.UUID, action, targetType string, targetID uuid.UUID) error
}

// Service holds the contacts business rules.
type Service struct {
	repo          Repository
	vault         *vault.Vault
	bookings      Bookings
	audit         Auditor
	retentionDays int
	now           func() time.Time
}

func NewService(repo Repository, v *vault.Vault, b Bookings, a Auditor, retentionDays int) *Service {
	return &Service{repo: repo, vault: v, bookings: b, audit: a, retentionDays: retentionDays, now: time.Now}
}

type payload struct {
	Name  string `json:"n"`
	Phone string `json:"p"`
}

func aad(userID uuid.UUID) []byte { return userID[:] }

// Sync stores the customer's contacts. Numbers are normalized and deduplicated,
// so syncing the same phone book again, or after another booking, never
// creates duplicates; it only adds new numbers and refreshes changed names.
func (s *Service) Sync(ctx context.Context, userID uuid.UUID, in []Input) (*SyncResult, error) {
	res := &SyncResult{Received: len(in)}
	if len(in) > MaxContactsPerSync {
		return nil, httpx.Validation([]httpx.FieldError{{Field: "contacts", Message: fmt.Sprintf("at most %d contacts per sync", MaxContactsPerSync)}})
	}

	unique := make(map[string]string, len(in)) // normalized phone -> name
	order := make([]string, 0, len(in))
	for _, c := range in {
		p, ok := phone.Normalize(c.Phone)
		if !ok {
			res.Skipped++
			continue
		}
		name := strings.TrimSpace(c.Name)
		if utf8.RuneCountInString(name) > maxNameRunes {
			name = string([]rune(name)[:maxNameRunes])
		}
		if _, dup := unique[p]; dup {
			res.Skipped++
		} else {
			order = append(order, p)
		}
		unique[p] = name
	}
	if len(order) == 0 {
		return nil, httpx.Validation([]httpx.FieldError{{Field: "contacts", Message: "no valid phone numbers found"}})
	}

	hashes := make([][]byte, 0, len(order))
	payloads := make([][]byte, 0, len(order))
	for _, p := range order {
		raw, err := json.Marshal(payload{Name: unique[p], Phone: p})
		if err != nil {
			return nil, err
		}
		sealed, err := s.vault.Seal(raw, aad(userID))
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, s.vault.Hash(p))
		payloads = append(payloads, sealed)
	}

	added, updated, err := s.repo.Upsert(ctx, userID, hashes, payloads)
	if err != nil {
		return nil, err
	}
	res.Added, res.Updated = added, updated
	if res.Total, err = s.repo.Count(ctx, userID); err != nil {
		return nil, err
	}
	return res, nil
}

// Status reports what is stored for the customer.
func (s *Service) Status(ctx context.Context, userID uuid.UUID) (*Status, error) {
	count, err := s.repo.Count(ctx, userID)
	if err != nil {
		return nil, err
	}
	at, err := s.repo.SyncedAt(ctx, userID)
	if err != nil {
		return nil, err
	}
	open, err := s.bookings.HasOpenBooking(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &Status{Synced: at != nil, SyncedAt: at, Count: count, CanDelete: !open && count > 0}, nil
}

// Delete removes the customer's contacts. It is refused while a booking is
// confirmed or active, because the contacts are needed during the ride.
func (s *Service) Delete(ctx context.Context, userID uuid.UUID, reason string) (*DeleteResult, error) {
	open, err := s.bookings.HasOpenBooking(ctx, userID)
	if err != nil {
		return nil, err
	}
	if open {
		return nil, httpx.Conflict("your contacts can be deleted once your current booking is completed")
	}
	var r *string
	if t := strings.TrimSpace(reason); t != "" {
		r = &t
	}
	n, err := s.repo.DeleteAll(ctx, userID, r)
	if err != nil {
		return nil, err
	}
	return &DeleteResult{Deleted: n}, nil
}

// BookingContacts lets an admin see the contacts of the customer behind a
// booking. It is allowed while the booking is open and for the retention period
// after it completes. Access is audited before any data is returned.
func (s *Service) BookingContacts(ctx context.Context, adminID, bookingID uuid.UUID) ([]Contact, error) {
	b, err := s.bookings.Lookup(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if !s.viewable(b) {
		return nil, httpx.Forbidden("contacts are not available for this booking")
	}
	if err := s.audit.Record(ctx, adminID, "view_contacts", "booking", bookingID); err != nil {
		return nil, fmt.Errorf("audit contacts access: %w", err)
	}

	raws, err := s.repo.Payloads(ctx, b.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]Contact, 0, len(raws))
	for _, sealed := range raws {
		plain, err := s.vault.Open(sealed, aad(b.UserID))
		if err != nil {
			return nil, err
		}
		var p payload
		if err := json.Unmarshal(plain, &p); err != nil {
			return nil, err
		}
		out = append(out, Contact{Name: p.Name, Phone: p.Phone})
	}
	return out, nil
}

func (s *Service) viewable(b *bookings.Booking) bool {
	switch b.Status {
	case bookings.StatusConfirmed, bookings.StatusActive:
		return true
	case bookings.StatusCompleted:
		return b.CompletedAt != nil && s.now().Before(b.CompletedAt.Add(time.Duration(s.retentionDays)*24*time.Hour))
	default:
		return false
	}
}
