package handler

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/middleware"
)

type ApplicationService interface {
	Create(ctx context.Context, p domain.Principal, in domain.DraftPatch) (domain.Application, error)
	Update(ctx context.Context, p domain.Principal, id string, version int, in domain.DraftPatch) (domain.Application, error)
	Get(ctx context.Context, p domain.Principal, id string) (domain.Application, error)
	List(ctx context.Context, p domain.Principal) ([]domain.ApplicationSummary, error)
	Submit(ctx context.Context, p domain.Principal, id string) (domain.Application, error)
}

type ApplicationHandler struct {
	svc ApplicationService
}

func NewApplicationHandler(svc ApplicationService) *ApplicationHandler {
	return &ApplicationHandler{svc: svc}
}

// draftRequest описывает тело создания и изменения черновика. Любое поле можно не передавать:
// указатель (или nil у form) означает «не менять». Version обязательна только при изменении.
type draftRequest struct {
	Version    *int           `json:"version"`
	ProductID  *string        `json:"product_id"`
	Amount     *int64         `json:"amount"`
	TermMonths *int           `json:"term_months"`
	Purpose    *string        `json:"purpose"`
	Form       map[string]any `json:"form"`
}

func (r draftRequest) patch() domain.DraftPatch {
	return domain.DraftPatch{
		ProductID: r.ProductID, Amount: r.Amount, TermMonths: r.TermMonths, Purpose: r.Purpose, Form: r.Form,
	}
}

// Create — первое сохранение формы: создаёт черновик заявки.
func (h *ApplicationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req draftRequest
	if !decode(w, r, &req) {
		return
	}
	p, _ := middleware.PrincipalFrom(r.Context())
	a, err := h.svc.Create(r.Context(), p, req.patch())
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

// Update — автосохранение черновика.
func (h *ApplicationHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req draftRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Version == nil {
		writeError(w, http.StatusBadRequest, "version is required")
		return
	}
	p, _ := middleware.PrincipalFrom(r.Context())
	a, err := h.svc.Update(r.Context(), p, chi.URLParam(r, "id"), *req.Version, req.patch())
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *ApplicationHandler) List(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	list, err := h.svc.List(r.Context(), p)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *ApplicationHandler) Get(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	a, err := h.svc.Get(r.Context(), p, chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *ApplicationHandler) Submit(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	a, err := h.svc.Submit(r.Context(), p, chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *ApplicationHandler) fail(w http.ResponseWriter, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "application not found")
	case errors.Is(err, domain.ErrConflict):
		writeError(w, http.StatusConflict, "application is not a draft")
	case errors.Is(err, domain.ErrVersionConflict):
		writeError(w, http.StatusConflict, "application was changed by another request, reload it")
	case errors.Is(err, domain.ErrUnknownProduct):
		writeError(w, http.StatusBadRequest, "unknown product")
	case errors.Is(err, domain.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "invalid field value")
	case errors.Is(err, domain.ErrProductUnavailable):
		writeError(w, http.StatusUnprocessableEntity, "product is not available for this company or parameters")
	case errors.As(err, &validation):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"missing_fields": validation.Fields})
	default:
		log.Printf("applications: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
