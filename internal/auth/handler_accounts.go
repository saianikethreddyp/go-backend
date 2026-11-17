package auth

import (
	"net/http"

	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password" validate:"required,max=128"`
	NewPassword     string `json:"new_password" validate:"required,max=128"`
}

type setActiveRequest struct {
	Active *bool `json:"active" validate:"required"`
}

type resetPasswordRequest struct {
	NewPassword string `json:"new_password" validate:"required,max=128"`
}

// ChangePassword handles POST /v1/auth/change-password.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) error {
	p, err := MustPrincipal(r)
	if err != nil {
		return err
	}
	var req changePasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	pair, err := h.svc.ChangePassword(r.Context(), p.AdminID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, pair)
	return nil
}

// SetActive handles PATCH /v1/admins/{id}.
func (h *Handler) SetActive(w http.ResponseWriter, r *http.Request) error {
	p, err := MustPrincipal(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req setActiveRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	updated, err := h.svc.SetActive(r.Context(), p.AdminID, id, *req.Active)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, updated)
	return nil
}

// ResetPassword handles POST /v1/admins/{id}/reset-password.
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) error {
	p, err := MustPrincipal(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req resetPasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.svc.ResetPassword(r.Context(), p.AdminID, id, req.NewPassword); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}
