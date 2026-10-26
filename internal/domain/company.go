package domain

import (
	"encoding/json"
	"time"
)

type Company struct {
	INN              string
	OrgType          string
	Name             string
	OKVED            string
	OKVEDTitle       *string
	OGRN             *string
	AccountNumber    *string // расчётный счёт в нашем банке
	RegistrationDate *time.Time
	TotalDebt        *int64
	Equity           *int64
	DebtToEquity     *float64
	FetchedAt        *time.Time

	// Данные из реестров, БКИ и документов в том виде, в каком они сохранены (JSON-объекты, по умолчанию пустые).
	Registry      json.RawMessage
	CreditHistory json.RawMessage
	Financials    json.RawMessage
}

// OtherBankLoan — кредит компании в другом банке (из внешних источников, companies.credit_history.other_banks).
type OtherBankLoan struct {
	Bank string `json:"bank"`
	Kind string `json:"kind"`
	Debt int64  `json:"debt"`
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
