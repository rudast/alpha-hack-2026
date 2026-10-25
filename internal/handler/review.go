package handler

import (
	"context"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/middleware"
	"github.com/rudast/alpha-hack-2026/internal/service"
)

type ReviewService interface {
	Pool(ctx context.Context, p domain.Principal, mine bool) ([]domain.ReviewSummary, error)
	Card(ctx context.Context, id string) (service.ReviewCard, error)
	Take(ctx context.Context, p domain.Principal, id string) (service.ReviewCard, error)
	Release(ctx context.Context, p domain.Principal, id string) (service.ReviewCard, error)
	Decide(ctx context.Context, p domain.Principal, id string, d domain.ReviewDecision) (service.ReviewCard, error)
	OpenDocument(ctx context.Context, id, documentID string) (io.ReadCloser, domain.Document, error)
}

// ReviewHandler — рабочее место сотрудника банка: заявки на ручной проверке.
type ReviewHandler struct {
	svc ReviewService
}

func NewReviewHandler(svc ReviewService) *ReviewHandler {
	return &ReviewHandler{svc: svc}
}

type reviewCompany struct {
	INN              string   `json:"inn"`
	OrgType          string   `json:"org_type"`
	Name             string   `json:"name"`
	OKVED            string   `json:"okved"`
	OKVEDTitle       *string  `json:"okved_title"`
	RegistrationDate *string  `json:"registration_date"`
	TotalDebt        *int64   `json:"total_debt"`
	Equity           *int64   `json:"equity"`
	DebtToEquity     *float64 `json:"debt_to_equity"`
}

type reviewCardResponse struct {
	domain.Application
	ProductName  string        `json:"product_name"`
	Company      reviewCompany `json:"company"`
	AssignedTo   *string       `json:"assigned_to"`
	AssignedName *string       `json:"assigned_name"`
	AssignedAt   *string       `json:"assigned_at"`
}

func reviewCard(c service.ReviewCard) reviewCardResponse {
	a, co := c.Application, c.Company
	resp := reviewCardResponse{
		Application: a,
		ProductName: c.ProductName,
		Company: reviewCompany{
			INN: co.INN, OrgType: co.OrgType, Name: co.Name, OKVED: co.OKVED, OKVEDTitle: co.OKVEDTitle,
			RegistrationDate: dateOnly(co.RegistrationDate), TotalDebt: co.TotalDebt, Equity: co.Equity, DebtToEquity: co.DebtToEquity,
		},
		AssignedTo:   a.AssignedTo,
		AssignedName: nullable(c.AssignedName),
	}
	if a.AssignedAt != nil {
		s := a.AssignedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		resp.AssignedAt = &s
	}
	return resp
}

// List — общий пул заявок на ручной проверке (никем не взятые), старые первыми.
// С параметром ?mine=true — заявки, которые взял текущий сотрудник.
func (h *ReviewHandler) List(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	mine, _ := strconv.ParseBool(r.URL.Query().Get("mine"))
	list, err := h.svc.Pool(r.Context(), p, mine)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// Get — карточка заявки: данные заявки и формы, решение скоринга, история, документы, компания
// и ФИО сотрудника, который взял заявку в работу.
func (h *ReviewHandler) Get(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.Card(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reviewCard(c))
}

// Take — взять заявку из общего пула в работу.
func (h *ReviewHandler) Take(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	c, err := h.svc.Take(r.Context(), p, chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reviewCard(c))
}

// Release — вернуть взятую заявку в общий пул.
func (h *ReviewHandler) Release(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	c, err := h.svc.Release(r.Context(), p, chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reviewCard(c))
}

type reviewDecisionRequest struct {
	Action      string  `json:"action"` // approve, reject, request_info, counter_offer
	Reason      string  `json:"reason"`
	InfoRequest string  `json:"info_request"`
	ProductID   string  `json:"product_id"`
	Amount      int64   `json:"amount"`
	TermMonths  int     `json:"term_months"`
	Rate        float64 `json:"rate"`
}

// Decide — решение сотрудника по заявке, которую он взял в работу.
func (h *ReviewHandler) Decide(w http.ResponseWriter, r *http.Request) {
	var req reviewDecisionRequest
	if !decode(w, r, &req) {
		return
	}
	p, _ := middleware.PrincipalFrom(r.Context())
	c, err := h.svc.Decide(r.Context(), p, chi.URLParam(r, "id"), domain.ReviewDecision{
		Action: req.Action, Reason: req.Reason, InfoRequest: req.InfoRequest,
		ProductID: req.ProductID, Amount: req.Amount, TermMonths: req.TermMonths, Rate: req.Rate,
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reviewCard(c))
}

// Document — скачать документ заявки (PDF или изображение из тома).
func (h *ReviewHandler) Document(w http.ResponseWriter, r *http.Request) {
	f, doc, err := h.svc.OpenDocument(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "docId"))
	if err != nil {
		h.fail(w, err)
		return
	}
	defer f.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	w.Header().Set("Content-Type", http.DetectContentType(head[:n]))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": doc.Filename}))
	w.Header().Set("Content-Length", strconv.FormatInt(doc.Size, 10))
	_, _ = w.Write(head[:n])
	_, _ = io.Copy(w, f)
}

func (h *ReviewHandler) fail(w http.ResponseWriter, err error) {
	var validation *domain.ValidationError
	var assigned *domain.AssignedError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "application not found")
	case errors.Is(err, domain.ErrDocumentNotFound):
		writeError(w, http.StatusNotFound, "document not found")
	case errors.As(err, &assigned):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "application is already taken", "assigned_name": assigned.Name})
	case errors.Is(err, domain.ErrNotInReview):
		writeError(w, http.StatusConflict, "application is not under manual review")
	case errors.Is(err, domain.ErrNotAssignedToYou):
		writeError(w, http.StatusConflict, "take the application before deciding on it")
	case errors.As(err, &validation):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"missing_fields": validation.Fields})
	case errors.Is(err, domain.ErrUnknownProduct):
		writeError(w, http.StatusBadRequest, "unknown product")
	case errors.Is(err, domain.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "invalid action or field value")
	case errors.Is(err, domain.ErrProductUnavailable):
		writeError(w, http.StatusUnprocessableEntity, "product is not available for this company or parameters")
	default:
		log.Printf("review: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
