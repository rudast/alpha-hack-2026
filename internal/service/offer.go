package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/catalog"
	"github.com/rudast/alpha-hack-2026/internal/domain"
)

// offerRuleVersion меняется, когда меняется формула; сохраняется в basis предложения.
const offerRuleVersion = 1

type OfferRepository interface {
	Active(ctx context.Context, inn string) (domain.Offer, error)
	GetForCompany(ctx context.Context, id, inn string) (domain.Offer, error)
	Create(ctx context.Context, o domain.Offer) (domain.Offer, bool, error)
	Close(ctx context.Context, id, status string) error
}

// OfferService считает и хранит предварительное предложение компании (баннер в кабинете).
// Это упрощённая проверка по уже известным данным компании и каталогу, а не скоринг:
// окончательное решение принимает воркер скоринга при подаче заявки. См. docs/preapproved-offer.md.
type OfferService struct {
	offers    OfferRepository
	companies CompanyGetter
	catalog   *catalog.Catalog
	now       func() time.Time
}

func NewOfferService(offers OfferRepository, companies CompanyGetter, cat *catalog.Catalog) *OfferService {
	return &OfferService{offers: offers, companies: companies, catalog: cat, now: time.Now}
}

// Current возвращает действующее предложение компании. Если его нет, оно истекло или данные компании
// обновились после расчёта, считает новое (старое при этом закрывается). Если предложения быть не может,
// возвращает nil и причину (domain.OfferReason*).
func (s *OfferService) Current(ctx context.Context, inn string) (*domain.Offer, string, error) {
	company, err := s.companies.GetByINN(ctx, inn)
	if err != nil {
		return nil, "", err
	}
	now := s.now()

	active, err := s.offers.Active(ctx, inn)
	switch {
	case errors.Is(err, domain.ErrNotFound):
	case err != nil:
		return nil, "", err
	case !now.Before(active.ValidUntil):
		if err := s.offers.Close(ctx, active.ID, domain.OfferExpired); err != nil {
			return nil, "", err
		}
	case !sameTime(active.Basis.FinancialsFetchedAt, company.FetchedAt),
		company.TotalDebt != nil && *company.TotalDebt != active.Basis.TotalDebt,
		company.Equity != nil && *company.Equity != active.Basis.Equity:
		// Данные компании изменились после расчёта: обновились внешние данные или выдан новый кредит.
		if err := s.offers.Close(ctx, active.ID, domain.OfferRevoked); err != nil {
			return nil, "", err
		}
	default:
		return &active, "", nil
	}

	offer, reason := s.calculate(company, now)
	if reason != "" {
		return nil, reason, nil
	}
	created, ok, err := s.offers.Create(ctx, offer)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		// Параллельный запрос успел сохранить своё предложение: отдаём его.
		active, err := s.offers.Active(ctx, inn)
		if err != nil {
			return nil, "", err
		}
		return &active, "", nil
	}
	return &created, "", nil
}

// ProductName возвращает название продукта предложения для показа клиенту.
func (s *OfferService) ProductName(id string) string {
	p, _ := s.catalog.Product(id)
	return p.Name
}

// creditHistory и registry — поля из сохранённых ответов БКИ и реестров, нужные для стоп-условий.
type creditHistory struct {
	CurrentOverdueDays *int `json:"current_overdue_days"`
}

type registryFlags struct {
	RedFlags []string `json:"red_flags"`
}

// calculate считает предложение по данным компании (docs/preapproved-offer.md, раздел 4).
// Возвращает предложение или причину, по которой его нет.
func (s *OfferService) calculate(c domain.Company, now time.Time) (domain.Offer, string) {
	rules := s.catalog.OfferRules()

	if c.TotalDebt == nil || c.Equity == nil {
		return domain.Offer{}, domain.OfferReasonNoData
	}
	if *c.Equity <= 0 {
		return domain.Offer{}, domain.OfferReasonNoEquity
	}
	// Проверка БКИ выполнена, только если в истории есть сведения о просрочке. Пустой объект или
	// только список кредитов в других банках означает «сведений нет», а не «всё чисто»: лимит ограничивается.
	var history creditHistory
	_ = json.Unmarshal(c.CreditHistory, &history)
	bkiChecked := history.CurrentOverdueDays != nil
	if bkiChecked && *history.CurrentOverdueDays > 0 {
		return domain.Offer{}, domain.OfferReasonOverdue
	}
	var registry registryFlags
	_ = json.Unmarshal(c.Registry, &registry)
	if len(registry.RedFlags) > 0 {
		return domain.Offer{}, domain.OfferReasonRegistryFlags
	}

	age := businessAgeMonths(c.RegistrationDate, now)
	young := age < rules.YoungBusinessMonths
	headroom := (rules.MaxDebtToEquity*float64(*c.Equity) - float64(*c.TotalDebt)) * rules.HeadroomShare

	var best catalog.Product
	var bestLimit int64
	eligible := false
	for _, id := range rules.Products {
		p, _ := s.catalog.Product(id)
		if len(p.OrgTypes) > 0 && !slices.Contains(p.OrgTypes, c.OrgType) {
			continue
		}
		if age < p.MinBusinessAgeMonths {
			continue
		}
		eligible = true

		limit := math.Min(headroom, float64(p.MaxAmount))
		if young && p.YoungMaxAmount > 0 {
			limit = math.Min(limit, float64(p.YoungMaxAmount))
		}
		if !bkiChecked {
			limit = math.Min(limit, float64(rules.NoBKICap))
		}
		rounded := int64(math.Floor(limit/float64(rules.RoundTo))) * rules.RoundTo
		if rounded < max(rules.MinAmount, p.MinAmount) {
			continue
		}
		// Наибольший лимит, при равенстве — наименьшая ставка.
		if rounded > bestLimit || (rounded == bestLimit && p.BaseRate < best.BaseRate) {
			best, bestLimit = p, rounded
		}
	}
	if bestLimit == 0 {
		if !eligible {
			return domain.Offer{}, domain.OfferReasonNoProduct
		}
		return domain.Offer{}, domain.OfferReasonHighDebtLoad
	}

	return domain.Offer{
		CompanyINN: c.INN,
		ProductID:  best.ID,
		MaxAmount:  bestLimit,
		TermMonths: min(max(rules.TermMonths, best.MinTermMonths), best.MaxTermMonths),
		RateFrom:   math.Round((best.BaseRate-rules.RateDiscount)*100) / 100,
		Basis: domain.OfferBasis{
			TotalDebt:           *c.TotalDebt,
			Equity:              *c.Equity,
			DebtToEquity:        c.DebtToEquity,
			FinancialsFetchedAt: c.FetchedAt,
			BusinessAgeMonths:   age,
			BKIChecked:          bkiChecked,
			RuleVersion:         offerRuleVersion,
		},
		ValidUntil: now.AddDate(0, 0, rules.ValidityDays),
	}, ""
}

// businessAgeMonths — полных месяцев с даты регистрации. Если дата неизвестна, бизнес считается
// новым (0): это осторожнее, чем считать его зрелым.
func businessAgeMonths(registered *time.Time, now time.Time) int {
	if registered == nil || registered.After(now) {
		return 0
	}
	months := (now.Year()-registered.Year())*12 + int(now.Month()-registered.Month())
	if now.Day() < registered.Day() {
		months--
	}
	return max(months, 0)
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
