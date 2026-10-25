package handler

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/middleware"
)

type ApplicationService interface {
	Create(ctx context.Context, p domain.Principal, in domain.DraftPatch) (domain.Application, error)
	CreateFromOffer(ctx context.Context, p domain.Principal, offerID string, in domain.DraftPatch) (domain.Application, bool, error)
	Update(ctx context.Context, p domain.Principal, id string, version int, in domain.DraftPatch) (domain.Application, error)
	Get(ctx context.Context, p domain.Principal, id string) (domain.Application, error)
	List(ctx context.Context, p domain.Principal) ([]domain.ApplicationSummary, error)
	Submit(ctx context.Context, p domain.Principal, id string) (domain.Application, error)
	Accept(ctx context.Context, p domain.Principal, id string) (domain.Application, error)
	Decline(ctx context.Context, p domain.Principal, id string) (domain.Application, error)
	Resume(ctx context.Context, p domain.Principal, id string) (domain.Application, error)
	UploadDocument(ctx context.Context, p domain.Principal, id, kind, filename string, content io.Reader) (domain.Document, error)
	DeleteDocument(ctx context.Context, p domain.Principal, id, documentID string) error
}

type ApplicationHandler struct {
	svc ApplicationService
}

func NewApplicationHandler(svc ApplicationService) *ApplicationHandler {
	return &ApplicationHandler{svc: svc}
}

// draftRequest описывает тело создания и изменения черновика. Любое поле можно не передавать:
// указатель (или nil у form) означает «не менять». Version обязательна только при изменении.
// OfferID учитывается только при создании: продукт, сумма и срок тогда берутся из предложения.
type draftRequest struct {
	Version    *int           `json:"version"`
	OfferID    *string        `json:"offer_id"`
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
	if req.OfferID != nil {
		// По предварительному предложению: повторное «Оформить» возвращает уже созданный черновик (200).
		a, created, err := h.svc.CreateFromOffer(r.Context(), p, *req.OfferID, req.patch())
		if err != nil {
			h.fail(w, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, a)
		return
	}
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

// Accept — клиент принимает одобрение или встречное предложение: кредит выдаётся и становится ACTIVE.
func (h *ApplicationHandler) Accept(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	a, err := h.svc.Accept(r.Context(), p, chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// Decline — клиент отказывается от встречного предложения: заявка возвращается в черновик.
func (h *ApplicationHandler) Decline(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	a, err := h.svc.Decline(r.Context(), p, chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// Resume — клиент дополнил заявку по запросу банка (INFO_REQUESTED) и отправляет её обратно на проверку.
func (h *ApplicationHandler) Resume(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	a, err := h.svc.Resume(r.Context(), p, chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// UploadDocument — загрузка документа к черновику: multipart/form-data с полями kind (вид документа
// из каталога) и file (PDF, JPG или PNG до 10 МБ).
func (h *ApplicationHandler) UploadDocument(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, domain.MaxDocumentSize+1<<20) // запас на заголовки multipart
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "file is larger than 10 MB")
			return
		}
		writeError(w, http.StatusBadRequest, "expected multipart/form-data with kind and file")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	if header.Size > domain.MaxDocumentSize {
		writeError(w, http.StatusRequestEntityTooLarge, "file is larger than 10 MB")
		return
	}

	p, _ := middleware.PrincipalFrom(r.Context())
	doc, err := h.svc.UploadDocument(r.Context(), p, chi.URLParam(r, "id"), r.FormValue("kind"), header.Filename, file)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

// DeleteDocument удаляет документ черновика.
func (h *ApplicationHandler) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	if err := h.svc.DeleteDocument(r.Context(), p, chi.URLParam(r, "id"), chi.URLParam(r, "docId")); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ApplicationHandler) fail(w http.ResponseWriter, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "application not found")
	case errors.Is(err, domain.ErrDocumentNotFound):
		writeError(w, http.StatusNotFound, "document not found")
	case errors.Is(err, domain.ErrInvalidDocument):
		writeError(w, http.StatusBadRequest, "unknown document kind or file type (PDF, JPG or PNG expected)")
	case errors.Is(err, domain.ErrDocumentTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "file is larger than 10 MB")
	case errors.Is(err, domain.ErrNotInfoRequested):
		writeError(w, http.StatusConflict, "application is not waiting for additional documents")
	case errors.Is(err, domain.ErrNotAwaitingDecision):
		writeError(w, http.StatusConflict, "application is not awaiting client decision")
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
	case errors.Is(err, domain.ErrOfferNotFound):
		writeError(w, http.StatusNotFound, "offer not found")
	case errors.Is(err, domain.ErrOfferUsed):
		writeError(w, http.StatusConflict, "offer was already used or revoked")
	case errors.Is(err, domain.ErrOfferExpired):
		writeError(w, http.StatusGone, "offer has expired")
	case errors.Is(err, domain.ErrOfferInvalid):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "offer is no longer valid", "reason": "offer_expired"})
	case errors.Is(err, domain.ErrOfferAmountExceeded):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "amount exceeds the offer limit", "reason": "offer_amount_exceeded"})
	default:
		log.Printf("applications: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
