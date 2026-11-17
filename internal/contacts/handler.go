package contacts

import (
	"net/http"

	"github.com/saianikethreddyp/thandra-api/internal/auth"
	"github.com/saianikethreddyp/thandra-api/internal/customer"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

// syncBodyLimit allows a large phone book while still bounding memory use.
const syncBodyLimit = 4 << 20

// Handler exposes the contacts endpoints.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

type syncRequest struct {
	Contacts []Input `json:"contacts" validate:"required,max=10000,dive"`
}

type deleteRequest struct {
	Reason string `json:"reason" validate:"max=500"`
}

func (h *Handler) Sync(w http.ResponseWriter, r *http.Request) error {
	p, err := customer.MustPrincipal(r)
	if err != nil {
		return err
	}
	var req syncRequest
	if err := httpx.DecodeJSONLimit(w, r, &req, syncBodyLimit); err != nil {
		return err
	}
	res, err := h.svc.Sync(r.Context(), p.UserID, req.Contacts)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) error {
	p, err := customer.MustPrincipal(r)
	if err != nil {
		return err
	}
	st, err := h.svc.Status(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, st)
	return nil
}

// Delete is the post-ride "delete my contacts" form.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) error {
	p, err := customer.MustPrincipal(r)
	if err != nil {
		return err
	}
	var req deleteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	res, err := h.svc.Delete(r.Context(), p.UserID, req.Reason)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

// AdminBookingContacts handles GET /v1/bookings/{id}/contacts.
func (h *Handler) AdminBookingContacts(w http.ResponseWriter, r *http.Request) error {
	admin, err := auth.MustPrincipal(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	list, err := h.svc.BookingContacts(r.Context(), admin.AdminID, id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"booking_id": id, "contacts": list})
	return nil
}
