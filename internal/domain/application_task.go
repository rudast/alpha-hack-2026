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
	Documents     []Document // загруженные файлы; воркер читает их из тома по Path

	// Показатели долговой нагрузки компании; nil, если они ещё не получены.
	Debt         *int64
	Equity       *int64
	DebtToEquity *float64

	Company CompanyProfile
}

// CompanyProfile — сведения о компании, которые нужны скорингу и которые уже хранятся у нас.
// Воркер не подключается к базе, поэтому получает их в задаче. Объекты Registry, CreditHistory и Financials
// передаются как сохранены (пустой объект, если данных нет).
type CompanyProfile struct {
	OrgType          string          `json:"org_type"`
	OKVED            string          `json:"okved"`
	RegistrationDate string          `json:"registration_date,omitempty"` // YYYY-MM-DD
	Registry         json.RawMessage `json:"registry"`
	CreditHistory    json.RawMessage `json:"credit_history"`
	Financials       json.RawMessage `json:"financials"`
}
