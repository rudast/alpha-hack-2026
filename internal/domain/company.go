package domain

import "time"

type Company struct {
	INN          string
	OrgType      string
	Name         string
	OKVED        string
	TotalDebt    *int64
	Equity       *int64
	DebtToEquity *float64
	FetchedAt    *time.Time
}

// NeedsRefresh сообщает, что показатели долговой нагрузки не запрашивались
// или устарели.
func (c Company) NeedsRefresh(now time.Time, ttl time.Duration) bool {
	return c.FetchedAt == nil || now.Sub(*c.FetchedAt) >= ttl
}

// DebtToEquity — общий долг / собственный капитал. Возвращает nil, если капитал
// нулевой или отрицательный: коэффициент не определён, скоринг считает это максимальным риском.
func DebtToEquity(debt, equity int64) *float64 {
	if equity <= 0 {
		return nil
	}
	v := float64(debt) / float64(equity)
	return &v
}
