package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/middleware"
)

type TrustService interface {
	EnsureTrustRatio(ctx context.Context, inn string) (domain.Company, error)
}

type OfferService interface {
	Current(ctx context.Context, inn string) (*domain.Offer, string, error)
	ProductName(id string) string
}

type MeHandler struct {
	trust  TrustService
	offers OfferService
}

func NewMeHandler(trust TrustService, offers OfferService) *MeHandler {
	return &MeHandler{trust: trust, offers: offers}
}

type bootstrapResponse struct {
	INN          string     `json:"inn"`
	TotalDebt    *int64     `json:"total_debt"`
	Equity       *int64     `json:"equity"`
	DebtToEquity *float64   `json:"debt_to_equity"`
	FetchedAt    *time.Time `json:"fetched_at"`
}

// Bootstrap — шаг после входа клиента: обновляет показатели долговой нагрузки, если они
// устарели (старше заданного интервала) или ещё не запрашивались. Блокируется до ответа
// воркера или таймаута.
func (h *MeHandler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())

	c, err := h.trust.EnsureTrustRatio(r.Context(), p.CompanyINN)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "company not found")
	case errors.Is(err, domain.ErrUpstreamTimeout), errors.Is(err, domain.ErrUpstreamFailed):
		log.Printf("bootstrap: credit history unavailable: %v", err)
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, "credit history is not available yet, try again")
	case err != nil:
		log.Printf("bootstrap: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		writeJSON(w, http.StatusOK, bootstrapResponse{
			INN:          c.INN,
			TotalDebt:    c.TotalDebt,
			Equity:       c.Equity,
			DebtToEquity: c.DebtToEquity,
			FetchedAt:    c.FetchedAt,
		})
	}
}

type offerView struct {
	ID          string  `json:"id"`
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	MaxAmount   int64   `json:"max_amount"`
	TermMonths  int     `json:"term_months"`
	RateFrom    float64 `json:"rate_from"`
	ValidUntil  string  `json:"valid_until"` // дата YYYY-MM-DD
}

type offerResponse struct {
	Offer  *offerView `json:"offer"`
	Reason string     `json:"reason,omitempty"`
}

// Offer возвращает предварительное предложение компании для баннера в кабинете или причину,
// по которой предложения нет. Считает по уже сохранённым данным компании и воркер не вызывает:
// показатели обновляет /me/bootstrap.
func (h *MeHandler) Offer(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())

	o, reason, err := h.offers.Current(r.Context(), p.CompanyINN)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "company not found")
	case err != nil:
		log.Printf("offer: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	case o == nil:
		writeJSON(w, http.StatusOK, offerResponse{Reason: reason})
	default:
		writeJSON(w, http.StatusOK, offerResponse{Offer: &offerView{
			ID:          o.ID,
			ProductID:   o.ProductID,
			ProductName: h.offers.ProductName(o.ProductID),
			MaxAmount:   o.MaxAmount,
			TermMonths:  o.TermMonths,
			RateFrom:    o.RateFrom,
			ValidUntil:  o.ValidUntil.Format(time.DateOnly),
		}})
	}
}
