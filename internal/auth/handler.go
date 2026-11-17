package auth

import (
	"net/http"

	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

// Handler exposes the auth and admin-management endpoints.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

type loginRequest struct {
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,max=128"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required,min=20,max=200"`
}

type createAdminRequest struct {
	Email    string `json:"email" validate:"required,email,max=254"`
	Name     string `json:"name" validate:"required,min=1,max=100"`
	Password string `json:"password" validate:"required"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) error {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	res, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) error {
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	pair, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, pair)
	return nil
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) error {
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) error {
	p, err := MustPrincipal(r)
	if err != nil {
		return err
	}
	me, err := h.svc.Me(r.Context(), p)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, me)
	return nil
}

func (h *Handler) CreateAdmin(w http.ResponseWriter, r *http.Request) error {
	var req createAdminRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	created, err := h.svc.CreateAdmin(r.Context(), CreateAdminInput{
		Email: req.Email, Name: req.Name, Password: req.Password,
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, created)
	return nil
}

func (h *Handler) ListAdmins(w http.ResponseWriter, r *http.Request) error {
	admins, err := h.svc.ListAdmins(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": admins})
	return nil
}
