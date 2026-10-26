package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type DemoPackage struct {
	Auth        *DemoAuth
	Sessions    *DemoAuth
	Trust       *DemoTrust
	Offers      *DemoOffers
	Profiles    *DemoProfiles
	Apps        *DemoApplications
	Loans       *DemoLoans
	Review      *DemoReview
	Idempotency *DemoIdempotency
}

func NewDemoPackage() *DemoPackage {
	auth := &DemoAuth{
		challenges: make(map[string]domain.Challenge),
		sessions:   make(map[string]domain.Principal),
	}

	apps := make(map[string]*domain.Application)
	now := time.Now().Add(-2 * time.Hour)
	submittedAt := time.Now().Add(-1 * time.Hour)
	app1 := &domain.Application{
		ID:          "app_demo_101",
		Version:     1,
		CompanyINN:  "7700000001",
		ProductID:   "working-capital",
		Amount:      2000000,
		TermMonths:  24,
		Purpose:     "Пополнение оборотных средств",
		Status:      domain.StatusApproved,
		Form:        map[string]any{"org": "ooo", "purpose": "Пополнение оборотных средств"},
		Documents:   []domain.Document{},
		History:     []domain.HistoryEntry{{From: domain.StatusDraft, To: domain.StatusApproved, At: submittedAt, By: "system"}},
		SubmittedAt: &submittedAt,
		CreatedAt:   now,
		UpdatedAt:   submittedAt,
	}
	app2 := &domain.Application{
		ID:         "app_demo_102",
		Version:    1,
		CompanyINN: "7700000002",
		ProductID:  "equipment-leasing",
		Amount:     5000000,
		TermMonths: 36,
		Purpose:    "Закупка оборудования",
		Status:     domain.StatusManualReview,
		Form: map[string]any{
			"loan_params": map[string]any{
				"amount":      5000000,
				"term_months": 36,
				"purpose":     "Закупка оборудования",
			},
			"equipment": map[string]any{
				"equipment_type": "Производственная линия",
				"equipment_cost": 6500000,
				"supplier_inn":   "7700000099",
			},
			"consents": map[string]any{
				"bki_request":     true,
				"data_processing": true,
			},
		},
		Documents: []domain.Document{
			{
				ID:         "doc_demo_201",
				Kind:       "passport",
				Filename:   "Паспорт_руководителя.pdf",
				Size:       2450000,
				Status:     domain.DocumentUploaded,
				UploadedAt: submittedAt,
			},
			{
				ID:         "doc_demo_202",
				Kind:       "account_statement",
				Filename:   "Выписка_по_счету_6мес.pdf",
				Size:       5200000,
				Status:     domain.DocumentUploaded,
				UploadedAt: submittedAt,
			},
		},
		History:     []domain.HistoryEntry{{From: domain.StatusDraft, To: domain.StatusManualReview, At: submittedAt, By: "system"}},
		SubmittedAt: &submittedAt,
		CreatedAt:   now,
		UpdatedAt:   submittedAt,
		Decision: []byte(`{
			"score": 58,
			"model_version": "v1.2",
			"thresholds": {"approve": 65, "counter": 45},
			"debt_to_equity": 0.8,
			"reasons_positive": [
				{"factor": "C3", "delta": 10, "text": "Стабильный возраст бизнеса (более 3 лет)"},
				{"factor": "P5", "delta": 5, "text": "Наличие расчетного счета в банке"}
			],
			"reasons_negative": [
				{"factor": "C2", "delta": -12, "text": "Повышенная долговая нагрузка"},
				{"factor": "C1b", "delta": -5, "text": "Запросы в БКИ за последние 90 дней"}
			],
			"manual_review_reasons": ["LTV_BORDERLINE", "FINANCIALS_UNAUDITED"]
		}`),
	}
	app3 := &domain.Application{
		ID:          "app_demo_103",
		Version:     1,
		CompanyINN:  "7700000003",
		ProductID:   "investment",
		Amount:      10000000,
		TermMonths:  48,
		Purpose:     "Строительство склада",
		Status:      domain.StatusDraft,
		Form:        map[string]any{"org": "ooo", "purpose": "Строительство склада"},
		Documents:   []domain.Document{},
		History:     []domain.HistoryEntry{{From: domain.StatusDraft, To: domain.StatusDraft, At: now, By: "system"}},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	app4 := &domain.Application{
		ID:          "app_demo_104",
		Version:     1,
		CompanyINN:  "7700000004",
		ProductID:   "express-revolving",
		Amount:      1500000,
		TermMonths:  12,
		Purpose:     "Кассовый разрыв",
		Status:      domain.StatusRejected,
		Form:        map[string]any{"org": "ooo", "purpose": "Кассовый разрыв"},
		Documents:   []domain.Document{},
		History:     []domain.HistoryEntry{{From: domain.StatusDraft, To: domain.StatusRejected, At: submittedAt, By: "system"}},
		SubmittedAt: &submittedAt,
		CreatedAt:   now,
		UpdatedAt:   submittedAt,
	}
	app5 := &domain.Application{
		ID:          "app_demo_105",
		Version:     1,
		CompanyINN:  "770000000005",
		ProductID:   "micro-loan",
		Amount:      800000,
		TermMonths:  12,
		Purpose:     "Материалы и расходники",
		Status:      domain.StatusApproved,
		Form:        map[string]any{"org": "ip", "purpose": "Материалы и расходники"},
		Documents:   []domain.Document{},
		History:     []domain.HistoryEntry{{From: domain.StatusDraft, To: domain.StatusApproved, At: submittedAt, By: "system"}},
		SubmittedAt: &submittedAt,
		CreatedAt:   now,
		UpdatedAt:   submittedAt,
	}
	assignedTime := submittedAt.Add(15 * time.Minute)
	empID := "demo-emp-1"
	app6 := &domain.Application{
		ID:         "app_demo_106",
		Version:    1,
		CompanyINN: "7700000003",
		ProductID:  "investment",
		Amount:     8000000,
		TermMonths: 48,
		Purpose:    "Модернизация производства",
		Status:     domain.StatusManualReview,
		AssignedTo: &empID,
		AssignedAt: &assignedTime,
		Form: map[string]any{
			"loan_params": map[string]any{
				"amount":      8000000,
				"term_months": 48,
				"purpose":     "Модернизация производства",
			},
			"investment": map[string]any{
				"project_name": "Расширение цеха №2",
				"total_budget": 12000000,
			},
			"consents": map[string]any{
				"bki_request":     true,
				"data_processing": true,
			},
		},
		Documents: []domain.Document{
			{
				ID:         "doc_demo_601",
				Kind:       "passport",
				Filename:   "Паспорт_директора.pdf",
				Size:       1800000,
				Status:     domain.DocumentUploaded,
				UploadedAt: submittedAt,
			},
			{
				ID:         "doc_demo_602",
				Kind:       "financial_statements",
				Filename:   "Бухгалтерская_отчетность_2025.pdf",
				Size:       3400000,
				Status:     domain.DocumentUploaded,
				UploadedAt: submittedAt,
			},
		},
		History: []domain.HistoryEntry{
			{From: domain.StatusDraft, To: domain.StatusManualReview, At: submittedAt, By: "system"},
			{From: domain.StatusManualReview, To: domain.StatusManualReview, At: assignedTime, By: empID, Comment: "Взята в работу"},
		},
		SubmittedAt: &submittedAt,
		CreatedAt:   now,
		UpdatedAt:   assignedTime,
		Decision: []byte(`{
			"score": 62,
			"model_version": "v1.2",
			"thresholds": {"approve": 65, "counter": 45},
			"debt_to_equity": 1.1,
			"reasons_positive": [
				{"factor": "C3", "delta": 15, "text": "Успешная кредитная история компании"},
				{"factor": "P2", "delta": 8, "text": "Высокая стабильность оборота"}
			],
			"reasons_negative": [
				{"factor": "C2", "delta": -8, "text": "Коэффициент долг/капитал на границе нормы"}
			],
			"manual_review_reasons": ["COLLATERAL_VERIFICATION"]
		}`),
	}

	apps[app1.ID] = app1
	apps[app2.ID] = app2
	apps[app3.ID] = app3
	apps[app4.ID] = app4
	apps[app5.ID] = app5
	apps[app6.ID] = app6

	demoApps := &DemoApplications{applications: apps, idCounter: 106}
	demoReview := &DemoReview{apps: demoApps}

	return &DemoPackage{
		Auth:        auth,
		Sessions:    auth,
		Trust:       &DemoTrust{},
		Offers:      &DemoOffers{},
		Profiles:    &DemoProfiles{},
		Apps:        demoApps,
		Loans:       &DemoLoans{},
		Review:      demoReview,
		Idempotency: &DemoIdempotency{},
	}
}

// ------------------- DemoAuth -------------------

type DemoAuth struct {
	mu         sync.RWMutex
	challenges map[string]domain.Challenge
	sessions   map[string]domain.Principal
}

func (d *DemoAuth) Login(ctx context.Context, in LoginInput) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	b := make([]byte, 16)
	_, _ = rand.Read(b)

	role := domain.RoleClient
	if in.Role == domain.RoleEmployee || in.Identifier == "underwriter@example.test" {
		role = domain.RoleEmployee
	}

	userID := "demo-user-1"
	inn := "7700000001"
	if role == domain.RoleEmployee {
		userID = "demo-emp-1"
		inn = ""
	} else if in.Identifier != "" {
		switch in.Identifier {
		case "client1@example.test", "+79990000001":
			inn = "7700000001"
		case "client2@example.test", "+79990000002":
			inn = "7700000002"
		case "client3@example.test", "+79990000003":
			inn = "7700000003"
		case "client4@example.test", "+79990000004":
			inn = "7700000004"
		case "client5@example.test", "+79990000005":
			inn = "770000000005"
		}
	}

	challengePrefix := "ch_cln_"
	if role == domain.RoleEmployee {
		challengePrefix = "ch_emp_"
	} else if inn != "" {
		challengePrefix = "ch_" + inn + "_"
	}
	challengeID := challengePrefix + hex.EncodeToString(b)

	d.challenges[challengeID] = domain.Challenge{
		UserID:      userID,
		Role:        role,
		CompanyINN:  inn,
		Destination: in.Identifier,
	}
	return challengeID, nil
}

func (d *DemoAuth) Verify(ctx context.Context, challengeID, code string) (Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ch, ok := d.challenges[challengeID]
	if !ok {
		role := domain.RoleClient
		inn := "7700000001"
		userID := "demo-user-1"
		if strings.HasPrefix(challengeID, "ch_emp_") {
			role = domain.RoleEmployee
			inn = ""
			userID = "demo-emp-1"
		} else {
			parts := strings.Split(challengeID, "_")
			if len(parts) >= 3 && parts[0] == "ch" && len(parts[1]) >= 10 {
				inn = parts[1]
				userID = "demo-user-" + inn
			}
		}
		ch = domain.Challenge{
			UserID:     userID,
			Role:       role,
			CompanyINN: inn,
		}
	}

	b := make([]byte, 16)
	_, _ = rand.Read(b)
	tokenPrefix := "tok_"
	if ch.Role == domain.RoleEmployee {
		tokenPrefix = "tok_emp_"
	} else if ch.CompanyINN != "" {
		tokenPrefix = "tok_" + ch.CompanyINN + "_"
	}
	token := tokenPrefix + hex.EncodeToString(b)

	d.sessions[token] = domain.Principal{
		UserID:     ch.UserID,
		Role:       ch.Role,
		CompanyINN: ch.CompanyINN,
	}

	redirect := RedirectClient
	if ch.Role == domain.RoleEmployee {
		redirect = RedirectEmployee
	}

	return Session{
		Token:    token,
		Role:     ch.Role,
		Redirect: redirect,
	}, nil
}

func (d *DemoAuth) GetSession(ctx context.Context, token string) (domain.Principal, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	p, ok := d.sessions[token]
	if !ok {
		if strings.HasPrefix(token, "tok_emp") {
			return domain.Principal{
				UserID: "demo-emp-1",
				Role:   domain.RoleEmployee,
			}, nil
		}
		if token != "" {
			inn := "7700000001"
			parts := strings.Split(token, "_")
			if len(parts) >= 3 && parts[0] == "tok" && len(parts[1]) >= 10 {
				inn = parts[1]
			}
			return domain.Principal{
				UserID:     "demo-user-" + inn,
				Role:       domain.RoleClient,
				CompanyINN: inn,
			}, nil
		}
		return domain.Principal{}, domain.ErrUnauthorized
	}
	return p, nil
}

// ------------------- DemoTrust -------------------

type DemoTrust struct{}

func (t *DemoTrust) EnsureTrustRatio(ctx context.Context, inn string) (domain.Company, error) {
	debt := int64(3000000)
	equity := int64(5000000)
	ratio := float64(0.6)
	now := time.Now()
	name := "ООО «Демо Торг»"
	orgType := "OOO"
	switch inn {
	case "7700000001":
		name = "ООО «Демо Торг»"
	case "7700000002":
		name = "ООО «Демо Строй»"
	case "7700000003":
		name = "ООО «Демо Логистик»"
	case "7700000004":
		name = "ООО «Демо Долг»"
	case "770000000005":
		name = "ИП Иванов Демо"
		orgType = "IP"
	}
	return domain.Company{
		INN:          inn,
		OrgType:      orgType,
		Name:         name,
		OKVED:        "47.19",
		TotalDebt:    &debt,
		Equity:       &equity,
		DebtToEquity: &ratio,
		FetchedAt:    &now,
	}, nil
}

// ------------------- DemoOffers -------------------

type DemoOffers struct{}

func (o *DemoOffers) Current(ctx context.Context, inn string) (*domain.Offer, string, error) {
	now := time.Now()
	validUntil := now.Add(30 * 24 * time.Hour)
	return &domain.Offer{
		ID:         "offer_demo_1",
		CompanyINN: inn,
		ProductID:  "working-capital",
		MaxAmount:  5000000,
		TermMonths: 36,
		RateFrom:   12.5,
		Status:     domain.OfferActive,
		ValidUntil: validUntil,
		CreatedAt:  now.Add(-24 * time.Hour),
	}, "", nil
}

func (o *DemoOffers) ProductName(id string) string {
	switch id {
	case "working-capital":
		return "Оборотный кредит"
	case "equipment-leasing":
		return "Лизинг оборудования"
	case "investment":
		return "Инвестиционный кредит"
	default:
		return "Кредит для бизнеса"
	}
}

// ------------------- DemoProfiles -------------------

type DemoProfiles struct{}

func (p *DemoProfiles) Profile(ctx context.Context, pr domain.Principal) (Profile, error) {
	phone := "+7 (999) 000-00-01"
	email := "demo@example.com"
	pos := "Генеральный директор"
	name := "ООО «Демо Торг»"
	orgType := "OOO"
	if pr.CompanyINN == "770000000005" {
		name = "ИП Иванов Демо"
		orgType = "IP"
		pos = "Индивидуальный предприниматель"
	}
	debt := int64(3000000)
	equity := int64(5000000)
	ratio := float64(0.6)
	now := time.Now()
	ogrn := "1027700000001"
	director := "Иванов Иван Иванович"
	address := "г. Москва, ул. Ленина, д. 10"
	status := "Действующая"

	return Profile{
		User: domain.User{
			ID:       pr.UserID,
			Role:     pr.Role,
			FullName: "Иванов Иван Иванович",
			Phone:    phone,
			Email:    email,
			Position: &pos,
		},
		Company: domain.Company{
			INN:          pr.CompanyINN,
			OrgType:      orgType,
			Name:         name,
			OGRN:         &ogrn,
			OKVED:        "47.19",
			TotalDebt:    &debt,
			Equity:       &equity,
			DebtToEquity: &ratio,
			FetchedAt:    &now,
		},
		Director: director,
		Address:  address,
		Status:   status,
	}, nil
}

// ------------------- DemoLoans -------------------

type DemoLoans struct{}

func (l *DemoLoans) Loans(ctx context.Context, inn string) (Loans, error) {
	now := time.Now()
	issued := now.Add(-60 * 24 * time.Hour)
	maturity := issued.Add(24 * 30 * 24 * time.Hour)
	nextDate := now.Add(10 * 24 * time.Hour)

	return Loans{
		Active: []Loan{
			{
				ID:             "loan_demo_1",
				ProductID:      "working-capital",
				ProductName:    "Оборотный кредит",
				ContractNumber: "КР-2026/01-001",
				Amount:         2000000,
				Rate:           14.5,
				TermMonths:     24,
				IssuedAt:       issued,
				MaturityDate:   maturity,
				Status:         domain.StatusActive,
				MonthlyPayment: 96500,
				PrincipalLeft:  1850000,
				PaidTotal:      193000,
				PaymentsMade:   2,
				NextPayment: &Payment{
					Number:       3,
					Date:         nextDate,
					Amount:       96500,
					Principal:    74000,
					Interest:     22500,
					BalanceAfter: 1776000,
					Status:       PaymentNext,
				},
				Schedule: []Payment{
					{Number: 1, Date: issued.Add(30 * 24 * time.Hour), Amount: 96500, Principal: 72000, Interest: 24500, BalanceAfter: 1928000, Status: PaymentPaid},
					{Number: 2, Date: issued.Add(60 * 24 * time.Hour), Amount: 96500, Principal: 73000, Interest: 23500, BalanceAfter: 1855000, Status: PaymentPaid},
					{Number: 3, Date: nextDate, Amount: 96500, Principal: 74000, Interest: 22500, BalanceAfter: 1781000, Status: PaymentNext},
				},
			},
		},
		Closed:     []Loan{},
		OtherBanks: []OtherBankLoan{},
	}, nil
}

// ------------------- DemoApplications -------------------

type DemoApplications struct {
	mu           sync.RWMutex
	applications map[string]*domain.Application
	idCounter    int
}

func (a *DemoApplications) Create(ctx context.Context, p domain.Principal, in domain.DraftPatch) (domain.Application, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.idCounter++
	id := fmt.Sprintf("app_demo_%d", a.idCounter)
	now := time.Now()

	pID := "working-capital"
	if in.ProductID != nil && *in.ProductID != "" {
		pID = *in.ProductID
	}
	var amt int64 = 1000000
	if in.Amount != nil && *in.Amount > 0 {
		amt = *in.Amount
	}
	term := 12
	if in.TermMonths != nil && *in.TermMonths > 0 {
		term = *in.TermMonths
	}
	purpose := "Развитие бизнеса"
	if in.Purpose != nil && *in.Purpose != "" {
		purpose = *in.Purpose
	}

	app := &domain.Application{
		ID:         id,
		Version:    1,
		CompanyINN: p.CompanyINN,
		ProductID:  pID,
		Amount:     amt,
		TermMonths: term,
		Purpose:    purpose,
		Status:     domain.StatusDraft,
		Form:       in.Form,
		Documents:  []domain.Document{},
		History:    []domain.HistoryEntry{{From: domain.StatusDraft, To: domain.StatusDraft, At: now, By: p.UserID}},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	a.applications[id] = app
	return *app, nil
}

func (a *DemoApplications) CreateFromOffer(ctx context.Context, p domain.Principal, offerID string, in domain.DraftPatch) (domain.Application, bool, error) {
	app, err := a.Create(ctx, p, in)
	if err != nil {
		return domain.Application{}, false, err
	}
	return app, true, nil
}

func (a *DemoApplications) Update(ctx context.Context, p domain.Principal, id string, version int, in domain.DraftPatch) (domain.Application, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	app, ok := a.applications[id]
	if !ok {
		return domain.Application{}, domain.ErrNotFound
	}
	if in.ProductID != nil {
		app.ProductID = *in.ProductID
	}
	if in.Amount != nil {
		app.Amount = *in.Amount
	}
	if in.TermMonths != nil {
		app.TermMonths = *in.TermMonths
	}
	if in.Purpose != nil {
		app.Purpose = *in.Purpose
	}
	if in.Form != nil {
		app.Form = in.Form
	}
	app.Version++
	app.UpdatedAt = time.Now()
	return *app, nil
}

func (a *DemoApplications) Get(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	app, ok := a.applications[id]
	if !ok {
		return domain.Application{}, domain.ErrNotFound
	}
	return *app, nil
}

func (a *DemoApplications) List(ctx context.Context, p domain.Principal) ([]domain.ApplicationSummary, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var res []domain.ApplicationSummary
	for _, app := range a.applications {
		if app.CompanyINN == p.CompanyINN {
			res = append(res, domain.ApplicationSummary{
				ID:          app.ID,
				ProductID:   app.ProductID,
				ProductName: "Кредит для бизнеса",
				Amount:      app.Amount,
				TermMonths:  app.TermMonths,
				Status:      app.Status,
				SubmittedAt: app.SubmittedAt,
				CreatedAt:   app.CreatedAt,
			})
		}
	}
	return res, nil
}

func (a *DemoApplications) Submit(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	app, ok := a.applications[id]
	if !ok {
		return domain.Application{}, domain.ErrNotFound
	}

	now := time.Now()
	app.Status = domain.StatusApproved
	app.SubmittedAt = &now
	app.UpdatedAt = now
	app.History = append(app.History, domain.HistoryEntry{
		From: domain.StatusDraft,
		To:   domain.StatusApproved,
		At:   now,
		By:   p.UserID,
	})
	return *app, nil
}

func (a *DemoApplications) Accept(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	app, ok := a.applications[id]
	if !ok {
		return domain.Application{}, domain.ErrNotFound
	}
	now := time.Now()
	app.Status = domain.StatusActive
	app.UpdatedAt = now
	return *app, nil
}

func (a *DemoApplications) Decline(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	app, ok := a.applications[id]
	if !ok {
		return domain.Application{}, domain.ErrNotFound
	}
	now := time.Now()
	app.Status = domain.StatusDraft
	app.UpdatedAt = now
	return *app, nil
}

func (a *DemoApplications) Resume(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	app, ok := a.applications[id]
	if !ok {
		return domain.Application{}, domain.ErrNotFound
	}
	now := time.Now()
	app.Status = domain.StatusDataCheck
	app.UpdatedAt = now
	return *app, nil
}

func (a *DemoApplications) UploadDocument(ctx context.Context, p domain.Principal, id, kind, filename string, content io.Reader) (domain.Document, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	app, ok := a.applications[id]
	if !ok {
		return domain.Document{}, domain.ErrNotFound
	}
	doc := domain.Document{
		ID:         fmt.Sprintf("doc_%d", time.Now().UnixNano()),
		Kind:       kind,
		Filename:   filename,
		Size:       1024,
		Status:     domain.DocumentUploaded,
		UploadedAt: time.Now(),
	}
	app.Documents = append(app.Documents, doc)
	return doc, nil
}

func (a *DemoApplications) DeleteDocument(ctx context.Context, p domain.Principal, id, documentID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	app, ok := a.applications[id]
	if !ok {
		return domain.ErrNotFound
	}
	for i, doc := range app.Documents {
		if doc.ID == documentID {
			app.Documents = append(app.Documents[:i], app.Documents[i+1:]...)
			return nil
		}
	}
	return domain.ErrDocumentNotFound
}

// ------------------- DemoIdempotency -------------------

type DemoIdempotency struct{}

func (i *DemoIdempotency) Begin(ctx context.Context, scope, key, fingerprint string) (domain.IdempotencyOutcome, domain.StoredResponse, error) {
	return domain.IdempotencyAcquired, domain.StoredResponse{}, nil
}

func (i *DemoIdempotency) Finish(ctx context.Context, scope, key, fingerprint string, resp domain.StoredResponse) error {
	return nil
}

func (i *DemoIdempotency) Abort(ctx context.Context, scope, key string) error {
	return nil
}

// ------------------- DemoReview -------------------

type DemoReview struct {
	apps *DemoApplications
}

func (r *DemoReview) Employee(ctx context.Context, p domain.Principal) (domain.User, error) {
	pos := "Андеррайтер МСБ"
	email := "underwriter@example.test"
	phone := "+7 (999) 000-00-99"
	return domain.User{
		ID:       p.UserID,
		Role:     domain.RoleEmployee,
		FullName: "Сидорова Анна Сергеевна",
		Phone:    phone,
		Email:    email,
		Position: &pos,
	}, nil
}

func (r *DemoReview) Pool(ctx context.Context, p domain.Principal, mine bool) ([]domain.ReviewSummary, error) {
	r.apps.mu.RLock()
	defer r.apps.mu.RUnlock()

	var result []domain.ReviewSummary
	for _, app := range r.apps.applications {
		if app.Status != domain.StatusManualReview {
			continue
		}
		if mine {
			if app.AssignedTo == nil || *app.AssignedTo != p.UserID {
				continue
			}
		} else {
			if app.AssignedTo != nil && *app.AssignedTo != "" {
				continue
			}
		}

		card := r.cardLocked(app)
		var assignedName *string
		if card.AssignedName != "" {
			name := card.AssignedName
			assignedName = &name
		}
		result = append(result, domain.ReviewSummary{
			ID:           app.ID,
			CompanyINN:   app.CompanyINN,
			CompanyName:  card.Company.Name,
			ProductID:    app.ProductID,
			ProductName:  card.ProductName,
			Amount:       app.Amount,
			TermMonths:   app.TermMonths,
			SubmittedAt:  app.SubmittedAt,
			AssignedTo:   app.AssignedTo,
			AssignedName: assignedName,
			AssignedAt:   app.AssignedAt,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].SubmittedAt == nil {
			return false
		}
		if result[j].SubmittedAt == nil {
			return true
		}
		return result[i].SubmittedAt.Before(*result[j].SubmittedAt)
	})

	return result, nil
}

func (r *DemoReview) Card(ctx context.Context, id string) (ReviewCard, error) {
	r.apps.mu.RLock()
	defer r.apps.mu.RUnlock()

	app, ok := r.apps.applications[id]
	if !ok {
		return ReviewCard{}, domain.ErrNotFound
	}
	return r.cardLocked(app), nil
}

func (r *DemoReview) Take(ctx context.Context, p domain.Principal, id string) (ReviewCard, error) {
	r.apps.mu.Lock()
	defer r.apps.mu.Unlock()

	app, ok := r.apps.applications[id]
	if !ok {
		return ReviewCard{}, domain.ErrNotFound
	}
	if app.Status != domain.StatusManualReview {
		return ReviewCard{}, domain.ErrNotInReview
	}
	if app.AssignedTo != nil && *app.AssignedTo != "" && *app.AssignedTo != p.UserID {
		return ReviewCard{}, &domain.AssignedError{Name: "Другой сотрудник"}
	}
	now := time.Now()
	app.AssignedTo = &p.UserID
	app.AssignedAt = &now
	app.UpdatedAt = now
	app.History = append(app.History, domain.HistoryEntry{
		From:    domain.StatusManualReview,
		To:      domain.StatusManualReview,
		At:      now,
		By:      p.UserID,
		Comment: "Взята в работу",
	})
	return r.cardLocked(app), nil
}

func (r *DemoReview) Release(ctx context.Context, p domain.Principal, id string) (ReviewCard, error) {
	r.apps.mu.Lock()
	defer r.apps.mu.Unlock()

	app, ok := r.apps.applications[id]
	if !ok {
		return ReviewCard{}, domain.ErrNotFound
	}
	if app.AssignedTo == nil || *app.AssignedTo != p.UserID {
		return ReviewCard{}, domain.ErrNotAssignedToYou
	}
	now := time.Now()
	app.AssignedTo = nil
	app.AssignedAt = nil
	app.UpdatedAt = now
	app.History = append(app.History, domain.HistoryEntry{
		From:    domain.StatusManualReview,
		To:      domain.StatusManualReview,
		At:      now,
		By:      p.UserID,
		Comment: "Возвращена в общий пул",
	})
	return r.cardLocked(app), nil
}

func (r *DemoReview) Decide(ctx context.Context, p domain.Principal, id string, d domain.ReviewDecision) (ReviewCard, error) {
	r.apps.mu.Lock()
	defer r.apps.mu.Unlock()

	app, ok := r.apps.applications[id]
	if !ok {
		return ReviewCard{}, domain.ErrNotFound
	}
	if app.Status != domain.StatusManualReview {
		return ReviewCard{}, domain.ErrNotInReview
	}

	now := time.Now()
	switch d.Action {
	case domain.ReviewApprove:
		app.Status = domain.StatusApproved
		app.History = append(app.History, domain.HistoryEntry{
			From:    domain.StatusManualReview,
			To:      domain.StatusApproved,
			At:      now,
			By:      p.UserID,
			Comment: strings.TrimSpace(d.Reason),
		})
	case domain.ReviewReject:
		reason := strings.TrimSpace(d.Reason)
		if reason == "" {
			return ReviewCard{}, &domain.ValidationError{Fields: []string{"reason"}}
		}
		app.Status = domain.StatusRejected
		app.History = append(app.History, domain.HistoryEntry{
			From:    domain.StatusManualReview,
			To:      domain.StatusRejected,
			At:      now,
			By:      p.UserID,
			Comment: reason,
		})
	case domain.ReviewRequestInfo:
		text := strings.TrimSpace(d.InfoRequest)
		if text == "" {
			return ReviewCard{}, &domain.ValidationError{Fields: []string{"info_request"}}
		}
		app.Status = domain.StatusInfoRequested
		app.InfoRequest = &text
		app.History = append(app.History, domain.HistoryEntry{
			From:    domain.StatusManualReview,
			To:      domain.StatusInfoRequested,
			At:      now,
			By:      p.UserID,
			Comment: text,
		})
	case domain.ReviewCounterOffer:
		app.Status = domain.StatusCounterOffer
		if d.ProductID != "" {
			app.ProductID = d.ProductID
		}
		if d.Amount > 0 {
			app.Amount = d.Amount
		}
		if d.TermMonths > 0 {
			app.TermMonths = d.TermMonths
		}
		counterData, _ := json.Marshal(map[string]any{
			"product_id":  app.ProductID,
			"amount":      app.Amount,
			"term_months": app.TermMonths,
			"rate":        d.Rate,
			"reason":      d.Reason,
		})
		app.Decision = counterData
		app.History = append(app.History, domain.HistoryEntry{
			From:    domain.StatusManualReview,
			To:      domain.StatusCounterOffer,
			At:      now,
			By:      p.UserID,
			Comment: strings.TrimSpace(d.Reason),
		})
	default:
		return ReviewCard{}, domain.ErrInvalidInput
	}
	app.UpdatedAt = now
	return r.cardLocked(app), nil
}

func (r *DemoReview) OpenDocument(ctx context.Context, id, documentID string) (io.ReadCloser, domain.Document, error) {
	r.apps.mu.RLock()
	defer r.apps.mu.RUnlock()

	app, ok := r.apps.applications[id]
	if !ok {
		return nil, domain.Document{}, domain.ErrNotFound
	}
	for _, doc := range app.Documents {
		if doc.ID == documentID {
			pdfData := []byte("%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj 2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj 3 0 obj<</Type/Page/MediaBox[0 0 300 144]/Parent 2 0 R/Resources<<>>>>endobj\nxref\n0 4\n0000000000 65535 f\n0000000010 00000 n\n0000000053 00000 n\n0000000102 00000 n\ntrailer<</Size 4/Root 1 0 R>>\nstartxref\n178\n%%EOF\n")
			doc.Size = int64(len(pdfData))
			return io.NopCloser(bytes.NewReader(pdfData)), doc, nil
		}
	}
	return nil, domain.Document{}, domain.ErrDocumentNotFound
}

func (r *DemoReview) cardLocked(app *domain.Application) ReviewCard {
	var companyName string
	var orgType = "OOO"
	if len(app.CompanyINN) == 12 {
		orgType = "IP"
	}
	switch app.CompanyINN {
	case "7700000001":
		companyName = "ООО «Демо Торг»"
	case "7700000002":
		companyName = "ООО «Демо Строй»"
	case "7700000003":
		companyName = "ООО «Демо Логистик»"
	case "7700000004":
		companyName = "ООО «Демо Долг»"
	case "770000000005":
		companyName = "ИП Иванов Демо"
	default:
		companyName = "ООО «Бизнес»"
	}
	var productName string
	switch app.ProductID {
	case "working-capital":
		productName = "Оборотный кредит"
	case "equipment-leasing":
		productName = "Лизинг оборудования"
	case "investment":
		productName = "Инвестиционный кредит"
	case "express-revolving":
		productName = "Экспресс-овердрафт"
	case "micro-loan":
		productName = "Микрокредит"
	default:
		productName = "Кредит для бизнеса"
	}
	debt := int64(3000000)
	equity := int64(5000000)
	ratio := float64(0.6)
	now := time.Now()
	co := domain.Company{
		INN:          app.CompanyINN,
		OrgType:      orgType,
		Name:         companyName,
		OKVED:        "47.19",
		TotalDebt:    &debt,
		Equity:       &equity,
		DebtToEquity: &ratio,
		FetchedAt:    &now,
	}
	var assignedName string
	if app.AssignedTo != nil && *app.AssignedTo != "" {
		if *app.AssignedTo == "demo-emp-1" {
			assignedName = "Сидорова Анна Сергеевна"
		} else {
			assignedName = "Сотрудник банка"
		}
	}
	return ReviewCard{
		Application:  *app,
		ProductName:  productName,
		Company:      co,
		AssignedName: assignedName,
	}
}
