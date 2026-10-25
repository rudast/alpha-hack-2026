package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/middleware"
	"github.com/rudast/alpha-hack-2026/internal/service"
)

type TrustService interface {
	EnsureTrustRatio(ctx context.Context, inn string) (domain.Company, error)
}

type OfferService interface {
	Current(ctx context.Context, inn string) (*domain.Offer, string, error)
	ProductName(id string) string
}

type ProfileService interface {
	Profile(ctx context.Context, p domain.Principal) (service.Profile, error)
}

type MeHandler struct {
	trust    TrustService
	offers   OfferService
	profiles ProfileService
}

func NewMeHandler(trust TrustService, offers OfferService, profiles ProfileService) *MeHandler {
	return &MeHandler{trust: trust, offers: offers, profiles: profiles}
}

type profileUser struct {
	FullName string  `json:"full_name"`
	Phone    *string `json:"phone"`
	Email    *string `json:"email"`
	Position *string `json:"position"`
}

type profileCompany struct {
	INN              string  `json:"inn"`
	OrgType          string  `json:"org_type"`
	Name             string  `json:"name"`
	OGRN             *string `json:"ogrn"`
	OKVED            string  `json:"okved"`
	OKVEDTitle       *string `json:"okved_title"`
	RegistrationDate *string `json:"registration_date"` // YYYY-MM-DD
	Director         *string `json:"director"`
	Address          *string `json:"address"`
	Status           *string `json:"status"`
	AccountNumber    *string `json:"account_number"`
}

type profileResponse struct {
	User    profileUser    `json:"user"`
	Company profileCompany `json:"company"`
}

// Me — профиль клиента и его компании: приветствие, профиль компании, подстановка в анкету.
func (h *MeHandler) Me(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())

	prof, err := h.profiles.Profile(r.Context(), p)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "profile not found")
		return
	case err != nil:
		log.Printf("me: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	u, c := prof.User, prof.Company
	writeJSON(w, http.StatusOK, profileResponse{
		User: profileUser{FullName: u.FullName, Phone: nullable(u.Phone), Email: nullable(u.Email), Position: u.Position},
		Company: profileCompany{
			INN: c.INN, OrgType: c.OrgType, Name: c.Name, OGRN: c.OGRN, OKVED: c.OKVED, OKVEDTitle: c.OKVEDTitle,
			RegistrationDate: dateOnly(c.RegistrationDate),
			Director:         nullable(prof.Director), Address: nullable(prof.Address), Status: nullable(prof.Status),
			AccountNumber: c.AccountNumber,
		},
	})
}

// nullable превращает пустую строку в null в ответе.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// dateOnly форматирует дату без времени (YYYY-MM-DD); nil остаётся null.
func dateOnly(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
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
