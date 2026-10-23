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

type MeHandler struct {
	trust TrustService
}

func NewMeHandler(trust TrustService) *MeHandler {
	return &MeHandler{trust: trust}
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
