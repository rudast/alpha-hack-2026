package domain

import "encoding/json"

// ApplicationTask — задача воркеру скоринга: данные заявки и показатели компании.
// Это снимок на момент отправки, а не живая строка таблицы.
type ApplicationTask struct {
	ApplicationID string
	INN           string
	ProductID     string
	Amount        int64
	TermMonths    int
	Form          map[string]any
	Documents     json.RawMessage

	// Показатели долговой нагрузки компании; nil, если они ещё не получены.
	Debt         *int64
	Equity       *int64
	DebtToEquity *float64
}
