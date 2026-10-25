package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
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
		ID:          "app_demo_102",
		Version:     1,
		CompanyINN:  "7700000002",
		ProductID:   "equipment-leasing",
		Amount:      5000000,
		TermMonths:  36,
		Purpose:     "Закупка оборудования",
		Status:      domain.StatusManualReview,
		Form:        map[string]any{"org": "ooo", "purpose": "Закупка оборудования"},
		Documents:   []domain.Document{},
		History:     []domain.HistoryEntry{{From: domain.StatusDraft, To: domain.StatusManualReview, At: submittedAt, By: "system"}},
		SubmittedAt: &submittedAt,
		CreatedAt:   now,
		UpdatedAt:   submittedAt,
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
	apps[app1.ID] = app1
	apps[app2.ID] = app2
	apps[app3.ID] = app3
	apps[app4.ID] = app4
	apps[app5.ID] = app5

	return &DemoPackage{
		Auth:        auth,
		Sessions:    auth,
		Trust:       &DemoTrust{},
		Offers:      &DemoOffers{},
		Profiles:    &DemoProfiles{},
		Apps:        &DemoApplications{applications: apps, idCounter: 105},
		Loans:       &DemoLoans{},
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
	challengeID := "ch_" + hex.EncodeToString(b)

	inn := "7700000001"
	if in.Identifier != "" {
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

	d.challenges[challengeID] = domain.Challenge{
		UserID:      "demo-user-1",
		Role:        domain.RoleClient,
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
		ch = domain.Challenge{
			UserID:     "demo-user-1",
			Role:       domain.RoleClient,
			CompanyINN: "7700000001",
		}
	}

	b := make([]byte, 16)
	_, _ = rand.Read(b)
	token := "tok_" + hex.EncodeToString(b)

	d.sessions[token] = domain.Principal{
		UserID:     ch.UserID,
		Role:       ch.Role,
		CompanyINN: ch.CompanyINN,
	}

	return Session{
		Token:    token,
		Role:     ch.Role,
		Redirect: "/personalAccount",
	}, nil
}

func (d *DemoAuth) GetSession(ctx context.Context, token string) (domain.Principal, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	p, ok := d.sessions[token]
	if !ok {
		if token != "" {
			return domain.Principal{
				UserID:     "demo-user-1",
				Role:       domain.RoleClient,
				CompanyINN: "7700000001",
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
