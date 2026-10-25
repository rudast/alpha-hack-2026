package domain

import "time"

// Статусы предварительного предложения.
const (
	OfferActive  = "ACTIVE"
	OfferUsed    = "USED"    // по предложению отправлена заявка
	OfferExpired = "EXPIRED" // прошёл срок действия
	OfferRevoked = "REVOKED" // данные компании изменились после расчёта
)

// Причины, по которым предложения нет. Нужны фронтенду и ассистенту, в баннере не показываются.
const (
	OfferReasonNoData        = "no_data"
	OfferReasonNoEquity      = "no_equity"
	OfferReasonOverdue       = "overdue"
	OfferReasonRegistryFlags = "registry_flags"
	OfferReasonNoProduct     = "no_product"
	OfferReasonHighDebtLoad  = "high_debt_load"
)

// Offer — предварительное предложение компании: до какой суммы, на какой срок и по какой ставке
// она может рассчитывать. Окончательное решение принимает скоринг при подаче заявки.
type Offer struct {
	ID         string
	CompanyINN string
	ProductID  string
	MaxAmount  int64
	TermMonths int
	RateFrom   float64
	Status     string
	Basis      OfferBasis
	ValidUntil time.Time
	CreatedAt  time.Time
	UsedAt     *time.Time
}

// Usable сообщает, можно ли ещё подать заявку по предложению.
func (o Offer) Usable(now time.Time) bool {
	return o.Status == OfferActive && now.Before(o.ValidUntil)
}

// OfferBasis — снимок данных компании, по которым посчитано предложение. По нему видно,
// устарело ли предложение (данные компании обновились после расчёта).
type OfferBasis struct {
	TotalDebt           int64      `json:"total_debt"`
	Equity              int64      `json:"equity"`
	DebtToEquity        *float64   `json:"debt_to_equity"`
	FinancialsFetchedAt *time.Time `json:"financials_fetched_at"`
	BusinessAgeMonths   int        `json:"business_age_months"`
	BKIChecked          bool       `json:"bki_checked"` // false: кредитной истории не было, лимит ограничен
	RuleVersion         int        `json:"rule_version"`
}
