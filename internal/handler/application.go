package handler

import (
	"encoding/base64"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/middleware"
	"github.com/rudast/alpha-hack-2026/internal/service"
)

type ApplicationHandler struct{ svc *service.ApplicationService }

func NewApplicationHandler(svc *service.ApplicationService) *ApplicationHandler {
	return &ApplicationHandler{svc: svc}
}

func (h *ApplicationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in service.ApplicationInput
	if !decode(w, r, &in) {
		return
	}
	p, _ := middleware.PrincipalFrom(r.Context())
	a, err := h.svc.Create(r.Context(), p, in)
	if err != nil {
		applicationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (h *ApplicationHandler) Get(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	a, err := h.svc.Get(r.Context(), p, chi.URLParam(r, "id"))
	if err != nil {
		applicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

type uploadDocumentRequest struct {
	Kind          string `json:"kind"`
	Filename      string `json:"filename"`
	MediaType     string `json:"media_type"`
	ContentBase64 string `json:"content_base64"`
}

func (h *ApplicationHandler) AddDocument(w http.ResponseWriter, r *http.Request) {
	var req uploadDocumentRequest
	if !decodeWithLimit(w, r, &req, 14<<20) {
		return
	}
	content, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid base64 document")
		return
	}
	p, _ := middleware.PrincipalFrom(r.Context())
	a, err := h.svc.AddDocument(r.Context(), p, chi.URLParam(r, "id"), req.Kind, req.Filename, req.MediaType, content)
	if err != nil {
		applicationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (h *ApplicationHandler) Submit(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	a, missing, err := h.svc.Submit(r.Context(), p, chi.URLParam(r, "id"))
	if len(missing) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"missing_fields": missing})
		return
	}
	if err != nil {
		applicationError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, a)
}

func applicationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "application not found")
	case errors.Is(err, domain.ErrConflict):
		writeError(w, http.StatusConflict, "application is not a draft")
	case errors.Is(err, domain.ErrInvalidApplication):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		log.Printf("application: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
