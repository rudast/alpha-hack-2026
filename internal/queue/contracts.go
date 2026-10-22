package queue

// These payloads mirror the internal Python worker contract. Application
// routes are not exposed by the HTTP API yet.

type ApplicationSnapshot struct {
	INN        string `json:"inn"`
	OrgType    string `json:"org_type"`
	ProductID  string `json:"product_id"`
	Amount     int64  `json:"amount"`
	TermMonths int    `json:"term_months"`
	Purpose    string `json:"purpose"`
	BKIConsent bool   `json:"bki_consent"`
}

type CompanySnapshot struct {
	TotalDebt *int64 `json:"total_debt"`
	Equity    *int64 `json:"equity"`
}

type VerifyApplicationRequest struct {
	Application ApplicationSnapshot `json:"application"`
	Company     CompanySnapshot     `json:"company"`
}

type VerifyApplicationResult struct {
	Complete             bool     `json:"complete"`
	MissingFields        []string `json:"missing_fields"`
	DebtToEquity         *float64 `json:"debt_to_equity"`
	RiskFlags            []string `json:"risk_flags"`
	ManualReviewRequired bool     `json:"manual_review_required"`
	NextStatus           string   `json:"next_status"`
}

type DocumentRef struct {
	Kind     string `json:"kind"`
	Filename string `json:"filename"`
}

type ParseDocumentsRequest struct {
	ApplicationID string        `json:"application_id"`
	Documents     []DocumentRef `json:"documents"`
}

type ParsedDocument struct {
	Kind            string         `json:"kind"`
	Filename        string         `json:"filename"`
	Status          string         `json:"status"`
	ExtractedFields map[string]any `json:"extracted_fields"`
}

type ParseDocumentsResult struct {
	ApplicationID string           `json:"application_id"`
	Status        string           `json:"status"`
	Documents     []ParsedDocument `json:"documents"`
	Reason        string           `json:"reason"`
}

type CriticalRiskRequest struct {
	ApplicationID string   `json:"application_id"`
	DebtToEquity  *float64 `json:"debt_to_equity"`
}

type CriticalRiskResult struct {
	ApplicationID     string   `json:"application_id"`
	Status            string   `json:"status"`
	RiskFlags         []string `json:"risk_flags"`
	RecommendedStatus string   `json:"recommended_status"`
	Decision          *string  `json:"decision"`
	Reason            string   `json:"reason"`
}
