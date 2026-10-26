package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"math"
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

var (
	globalDemoOnce sync.Once
	globalDemoPkg  *DemoPackage
)

func NewDemoPackage() *DemoPackage {
	globalDemoOnce.Do(func() {
		globalDemoPkg = initDemoPackage()
	})
	return globalDemoPkg
}

func initDemoPackage() *DemoPackage {
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
	demoLoans := &DemoLoans{apps: demoApps}

	return &DemoPackage{
		Auth:        auth,
		Sessions:    auth,
		Trust:       &DemoTrust{},
		Offers:      &DemoOffers{},
		Profiles:    &DemoProfiles{},
		Apps:        demoApps,
		Loans:       demoLoans,
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

// ------------------- Demo Data & Helpers -------------------

type demoCompanyInfo struct {
	Name             string
	OrgType          string
	OGRN             string
	OKVED            string
	OKVEDTitle       string
	RegistrationDate string // YYYY-MM-DD
	Director         string
	Address          string
	AccountNumber    string
	Debt             int64
	Equity           int64
	DebtToEquity     float64
	UserFullName     string
	UserPhone        string
	UserEmail        string
	UserPosition     string
}

var demoCompanies = map[string]demoCompanyInfo{
	"7700000001": {
		Name:             "ООО «Демо Торг»",
		OrgType:          "OOO",
		OGRN:             "1027700000001",
		OKVED:            "47.19",
		OKVEDTitle:       "Прочая розничная торговля в неспециализированных магазинах",
		RegistrationDate: "2016-03-14",
		Director:         "Петров Пётр Петрович",
		Address:          "г. Москва, ул. Демонстрационная, д. 1",
		AccountNumber:    "40702810900000000001",
		Debt:             3000000,
		Equity:           5000000,
		DebtToEquity:     0.60,
		UserFullName:     "Петров Пётр Петрович",
		UserPhone:        "+79990000001",
		UserEmail:        "client1@example.test",
		UserPosition:     "Генеральный директор",
	},
	"7700000002": {
		Name:             "ООО «Демо Строй»",
		OrgType:          "OOO",
		OGRN:             "1027700000002",
		OKVED:            "41.20",
		OKVEDTitle:       "Строительство жилых и нежилых зданий",
		RegistrationDate: "2022-09-01",
		Director:         "Смирнова Ольга Ивановна",
		Address:          "г. Москва, ул. Демонстрационная, д. 2",
		AccountNumber:    "40702810900000000002",
		Debt:             2400000,
		Equity:           6000000,
		DebtToEquity:     0.40,
		UserFullName:     "Смирнова Ольга Ивановна",
		UserPhone:        "+79990000002",
		UserEmail:        "client2@example.test",
		UserPosition:     "Генеральный директор",
	},
	"7700000003": {
		Name:             "ООО «Демо Логистик»",
		OrgType:          "OOO",
		OGRN:             "1027700000003",
		OKVED:            "49.41",
		OKVEDTitle:       "Деятельность автомобильного грузового транспорта",
		RegistrationDate: "2019-06-20",
		Director:         "Кузнецов Андрей Викторович",
		Address:          "г. Москва, ул. Демонстрационная, д. 3",
		AccountNumber:    "40702810900000000003",
		Debt:             1500000,
		Equity:           3000000,
		DebtToEquity:     0.50,
		UserFullName:     "Кузнецов Андрей Викторович",
		UserPhone:        "+79990000003",
		UserEmail:        "client3@example.test",
		UserPosition:     "Генеральный директор",
	},
	"7700000004": {
		Name:             "ООО «Демо Долг»",
		OrgType:          "OOO",
		OGRN:             "1027700000004",
		OKVED:            "46.90",
		OKVEDTitle:       "Торговля оптовая неспециализированная",
		RegistrationDate: "2012-11-05",
		Director:         "Попов Сергей Николаевич",
		Address:          "г. Москва, ул. Демонстрационная, д. 4",
		AccountNumber:    "40702810900000000004",
		Debt:             7000000,
		Equity:           2000000,
		DebtToEquity:     3.50,
		UserFullName:     "Попов Сергей Николаевич",
		UserPhone:        "+79990000004",
		UserEmail:        "client4@example.test",
		UserPosition:     "Генеральный директор",
	},
	"770000000005": {
		Name:             "ИП Иванов Демо",
		OrgType:          "IP",
		OGRN:             "304770000000005",
		OKVED:            "56.10",
		OKVEDTitle:       "Деятельность ресторанов и услуги по доставке продуктов питания",
		RegistrationDate: "2020-02-10",
		Director:         "Иванов Иван Иванович",
		Address:          "г. Москва, ул. Демонстрационная, д. 5",
		AccountNumber:    "40802810900000000005",
		Debt:             800000,
		Equity:           2000000,
		DebtToEquity:     0.40,
		UserFullName:     "Иванов Иван Иванович",
		UserPhone:        "+79990000005",
		UserEmail:        "client5@example.test",
		UserPosition:     "Индивидуальный предприниматель",
	},
}

func demoProductName(id string) string {
	switch id {
	case "working_capital", "working-capital":
		return "Кредит на пополнение оборотных средств"
	case "equipment_loan", "equipment-leasing":
		return "Кредит на оборудование"
	case "investment":
		return "Инвестиционный кредит"
	case "express", "express-revolving":
		return "Экспресс-кредит для бизнеса"
	case "micro-loan":
		return "Микрокредит"
	case "ooo_loan":
		return "Кредит для ООО"
	case "ip_loan":
		return "Кредит для ИП"
	case "auto_loan":
		return "Автокредит для бизнеса"
	case "overdraft":
		return "Овердрафт"
	case "credit_line":
		return "Возобновляемая кредитная линия (ВКЛ)"
	case "loan_up_to_1m":
		return "Кредит до 1 млн ₽"
	case "marketplace_seller":
		return "Кредит для селлеров маркетплейсов"
	case "secured_property_equipment":
		return "Кредит под залог недвижимости или оборудования"
	case "business_purchase":
		return "Кредит на покупку бизнеса"
	case "mortgage_ip":
		return "Ипотека для ИП"
	case "mortgage_business":
		return "Ипотека для бизнеса"
	default:
		return "Кредит для бизнеса"
	}
}

// ------------------- DemoTrust -------------------

type DemoTrust struct{}

func (t *DemoTrust) EnsureTrustRatio(ctx context.Context, inn string) (domain.Company, error) {
	info, ok := demoCompanies[inn]
	if !ok {
		info = demoCompanies["7700000001"]
	}
	now := time.Now()
	regDate, _ := time.Parse("2006-01-02", info.RegistrationDate)
	debt := info.Debt
	equity := info.Equity
	ratio := info.DebtToEquity
	ogrn := info.OGRN
	acc := info.AccountNumber
	title := info.OKVEDTitle

	return domain.Company{
		INN:              inn,
		OrgType:          info.OrgType,
		Name:             info.Name,
		OKVED:            info.OKVED,
		OKVEDTitle:       &title,
		OGRN:             &ogrn,
		AccountNumber:    &acc,
		RegistrationDate: &regDate,
		TotalDebt:        &debt,
		Equity:           &equity,
		DebtToEquity:     &ratio,
		FetchedAt:        &now,
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
	return demoProductName(id)
}

// ------------------- DemoProfiles -------------------

type DemoProfiles struct{}

func (p *DemoProfiles) Profile(ctx context.Context, pr domain.Principal) (Profile, error) {
	info, ok := demoCompanies[pr.CompanyINN]
	if !ok {
		info = demoCompanies["7700000001"]
	}
	now := time.Now()
	regDate, _ := time.Parse("2006-01-02", info.RegistrationDate)
	ogrn := info.OGRN
	acc := info.AccountNumber
	title := info.OKVEDTitle
	debt := info.Debt
	equity := info.Equity
	ratio := info.DebtToEquity
	pos := info.UserPosition

	return Profile{
		User: domain.User{
			ID:         pr.UserID,
			Role:       pr.Role,
			FullName:   info.UserFullName,
			Phone:      info.UserPhone,
			Email:      info.UserEmail,
			Position:   &pos,
			CompanyINN: pr.CompanyINN,
		},
		Company: domain.Company{
			INN:              pr.CompanyINN,
			OrgType:          info.OrgType,
			Name:             info.Name,
			OGRN:             &ogrn,
			OKVED:            info.OKVED,
			OKVEDTitle:       &title,
			AccountNumber:    &acc,
			RegistrationDate: &regDate,
			TotalDebt:        &debt,
			Equity:           &equity,
			DebtToEquity:     &ratio,
			FetchedAt:        &now,
		},
		Director: info.Director,
		Address:  info.Address,
		Status:   "ACTIVE",
	}, nil
}

// ------------------- DemoLoans -------------------

type DemoLoans struct {
	apps *DemoApplications
}

func makeDemoLoan(id, productID, productName, contractNumber string, amount int64, rate float64, termMonths, paymentsMade int, overdue bool) Loan {
	now := time.Now()
	issued := now.Add(time.Duration(-paymentsMade*30*24) * time.Hour)
	maturity := issued.Add(time.Duration(termMonths*30*24) * time.Hour)
	nextDate := now.Add(10 * 24 * time.Hour)
	if overdue {
		nextDate = now.Add(-5 * 24 * time.Hour)
	}

	monthlyRate := rate / 100 / 12
	monthlyPayment := float64(amount) * monthlyRate / (1 - math.Pow(1+monthlyRate, -float64(termMonths)))
	if math.IsNaN(monthlyPayment) || math.IsInf(monthlyPayment, 0) || monthlyPayment <= 0 {
		monthlyPayment = float64(amount) / float64(termMonths)
	}

	paidTotal := int64(float64(paymentsMade) * monthlyPayment)
	principalLeft := amount - int64(float64(paymentsMade)*(float64(amount)/float64(termMonths)))
	if principalLeft < 0 {
		principalLeft = 0
	}

	status := domain.StatusActive
	var overdueAmount float64
	if overdue {
		status = domain.StatusOverdue
		overdueAmount = math.Round(monthlyPayment)
	}

	schedule := make([]Payment, 0, termMonths)
	for i := 1; i <= termMonths && i <= paymentsMade+3; i++ {
		pStatus := PaymentPlanned
		if i <= paymentsMade {
			pStatus = PaymentPaid
		} else if i == paymentsMade+1 {
			if overdue {
				pStatus = PaymentOverdue
			} else {
				pStatus = PaymentNext
			}
		}
		pDate := issued.Add(time.Duration(i*30*24) * time.Hour)
		schedule = append(schedule, Payment{
			Number:       i,
			Date:         pDate,
			Amount:       math.Round(monthlyPayment),
			Principal:    math.Round(float64(amount) / float64(termMonths)),
			Interest:     math.Round(monthlyPayment - float64(amount)/float64(termMonths)),
			BalanceAfter: math.Round(float64(amount) - float64(i)*(float64(amount)/float64(termMonths))),
			Status:       pStatus,
		})
	}

	var nextPayment *Payment
	if paymentsMade < termMonths {
		nextPayment = &Payment{
			Number:       paymentsMade + 1,
			Date:         nextDate,
			Amount:       math.Round(monthlyPayment),
			Principal:    math.Round(float64(amount) / float64(termMonths)),
			Interest:     math.Round(monthlyPayment - float64(amount)/float64(termMonths)),
			BalanceAfter: math.Round(float64(principalLeft)),
			Status:       PaymentNext,
		}
		if overdue {
			nextPayment.Status = PaymentOverdue
		}
	}

	return Loan{
		ID:             id,
		ProductID:      productID,
		ProductName:    productName,
		ContractNumber: contractNumber,
		Amount:         amount,
		Rate:           rate,
		TermMonths:     termMonths,
		IssuedAt:       issued,
		MaturityDate:   maturity,
		Status:         status,
		MonthlyPayment: math.Round(monthlyPayment),
		PrincipalLeft:  principalLeft,
		PaidTotal:      paidTotal,
		PaymentsMade:   paymentsMade,
		NextPayment:    nextPayment,
		OverdueAmount:  overdueAmount,
		Schedule:       schedule,
	}
}

func makeDemoClosedLoan(id, productID, productName, contractNumber string, amount int64, rate float64, termMonths, closedMonthsAgo int) Loan {
	now := time.Now()
	issued := now.Add(time.Duration(-(termMonths+closedMonthsAgo)*30*24) * time.Hour)
	closedAt := now.Add(time.Duration(-closedMonthsAgo*30*24) * time.Hour)
	return Loan{
		ID:             id,
		ProductID:      productID,
		ProductName:    productName,
		ContractNumber: contractNumber,
		Amount:         amount,
		Rate:           rate,
		TermMonths:     termMonths,
		IssuedAt:       issued,
		MaturityDate:   closedAt,
		ClosedAt:       &closedAt,
		Status:         domain.StatusClosed,
		PaymentsMade:   termMonths,
		PrincipalLeft:  0,
	}
}

func makeDemoLoanFromApp(app *domain.Application) Loan {
	term := app.TermMonths
	if term <= 0 {
		term = 12
	}
	amt := app.Amount
	if amt <= 0 {
		amt = 1000000
	}
	rate := 16.5
	if len(app.Decision) > 0 {
		var dec struct {
			Rate float64 `json:"rate"`
		}
		if err := json.Unmarshal(app.Decision, &dec); err == nil && dec.Rate > 0 {
			rate = dec.Rate
		}
	}
	hash := crc32.ChecksumIEEE([]byte(app.ID))
	contractNum := fmt.Sprintf("0КР-%04d/%04d", time.Now().Year(), (hash%9000)+1000)
	isOverdue := (app.Status == domain.StatusOverdue)
	return makeDemoLoan(app.ID, app.ProductID, demoProductName(app.ProductID), contractNum, amt, rate, term, 0, isOverdue)
}

func makeDemoClosedLoanFromApp(app *domain.Application) Loan {
	term := app.TermMonths
	if term <= 0 {
		term = 12
	}
	amt := app.Amount
	if amt <= 0 {
		amt = 1000000
	}
	rate := 16.5
	if len(app.Decision) > 0 {
		var dec struct {
			Rate float64 `json:"rate"`
		}
		if err := json.Unmarshal(app.Decision, &dec); err == nil && dec.Rate > 0 {
			rate = dec.Rate
		}
	}
	hash := crc32.ChecksumIEEE([]byte(app.ID))
	contractNum := fmt.Sprintf("0КР-%04d/%04d", time.Now().Year(), (hash%9000)+1000)
	return makeDemoClosedLoan(app.ID, app.ProductID, demoProductName(app.ProductID), contractNum, amt, rate, term, 1)
}

func baselineLoans(inn string) Loans {
	switch inn {
	case "7700000001":
		return Loans{
			Active: []Loan{
				makeDemoLoan("L-1001", "working_capital", "Кредит на пополнение оборотных средств", "0ОС-2026/1187", 2000000, 19.0, 18, 7, false),
				makeDemoLoan("L-1002", "auto_loan", "Автокредит для бизнеса", "0АК-2025/0342", 1500000, 15.5, 36, 20, false),
			},
			Closed: []Loan{
				makeDemoClosedLoan("C-1001", "working_capital", "Кредит на пополнение оборотных средств", "0ОС-2023/0412", 1500000, 18.0, 18, 20),
				makeDemoClosedLoan("C-1002", "express", "Экспресс-кредит для бизнеса", "0ЭК-2024/1120", 500000, 21.0, 12, 10),
			},
			OtherBanks: []OtherBankLoan{
				{Bank: "Сбербанк", Kind: "Кредит на оборотные средства", Debt: 958805},
			},
		}
	case "7700000002":
		return Loans{
			Active: []Loan{},
			Closed: []Loan{
				makeDemoClosedLoan("C-2001", "ooo_loan", "Кредит для ООО", "0КО-2023/0905", 800000, 20.0, 12, 24),
			},
			OtherBanks: []OtherBankLoan{
				{Bank: "ВТБ", Kind: "Инвестиционный кредит", Debt: 2400000},
			},
		}
	case "7700000003":
		return Loans{
			Active: []Loan{
				makeDemoLoan("L-3001", "equipment_loan", "Кредит на оборудование", "0ОБ-2024/2210", 2400000, 16.5, 48, 26, false),
			},
			Closed: []Loan{
				makeDemoClosedLoan("C-3001", "auto_loan", "Автокредит для бизнеса", "0АК-2020/1015", 2000000, 14.5, 36, 36),
				makeDemoClosedLoan("C-3002", "loan_up_to_1m", "Кредит до 1 млн ₽", "0КМ-2023/0301", 900000, 21.5, 12, 28),
			},
			OtherBanks: []OtherBankLoan{
				{Bank: "Т-Банк", Kind: "Овердрафт", Debt: 504676},
			},
		}
	case "7700000004":
		return Loans{
			Active: []Loan{
				makeDemoLoan("L-4001", "ooo_loan", "Кредит для ООО", "0КО-2026/0415", 3000000, 20.0, 18, 5, true),
				makeDemoLoan("L-4002", "credit_line", "Возобновляемая кредитная линия (ВКЛ)", "0ВКЛ-2026/0098", 1500000, 17.0, 24, 8, false),
			},
			Closed: []Loan{
				makeDemoClosedLoan("C-4001", "working_capital", "Кредит на пополнение оборотных средств", "0ОС-2022/0718", 2500000, 17.0, 24, 26),
			},
			OtherBanks: []OtherBankLoan{
				{Bank: "Сбербанк", Kind: "Кредит под залог недвижимости", Debt: 2214000},
				{Bank: "ВТБ", Kind: "Оборотный кредит", Debt: 1476802},
			},
		}
	case "770000000005":
		return Loans{
			Active: []Loan{
				makeDemoLoan("L-5001", "express", "Экспресс-кредит для бизнеса", "0ЭК-2026/0731", 600000, 20.5, 12, 4, false),
			},
			Closed: []Loan{
				makeDemoClosedLoan("C-5001", "ip_loan", "Кредит для ИП", "0КИ-2024/0210", 400000, 22.0, 12, 18),
			},
			OtherBanks: []OtherBankLoan{
				{Bank: "Совкомбанк", Kind: "Кредит для ИП", Debt: 386612},
			},
		}
	default:
		return Loans{
			Active:     []Loan{},
			Closed:     []Loan{},
			OtherBanks: []OtherBankLoan{},
		}
	}
}

func (l *DemoLoans) Loans(ctx context.Context, inn string) (Loans, error) {
	base := baselineLoans(inn)
	if l.apps != nil {
		l.apps.mu.RLock()
		var extraActive []Loan
		var extraClosed []Loan
		for _, app := range l.apps.applications {
			if app.CompanyINN != inn {
				continue
			}
			if app.Status == domain.StatusActive || app.Status == domain.StatusOverdue {
				extraActive = append(extraActive, makeDemoLoanFromApp(app))
			} else if app.Status == domain.StatusClosed {
				extraClosed = append(extraClosed, makeDemoClosedLoanFromApp(app))
			}
		}
		l.apps.mu.RUnlock()

		if len(extraActive) > 0 {
			base.Active = append(extraActive, base.Active...)
		}
		if len(extraClosed) > 0 {
			base.Closed = append(extraClosed, base.Closed...)
		}
	}
	return base, nil
}

// ------------------- DemoApplications -------------------

type DemoApplications struct {
	mu           sync.RWMutex
	applications map[string]*domain.Application
	idCounter    int
}

func (a *DemoApplications) getOrCreateLocked(id, inn string) *domain.Application {
	app, ok := a.applications[id]
	if ok {
		return app
	}
	now := time.Now()
	pID := "working-capital"
	app = &domain.Application{
		ID:         id,
		Version:    1,
		CompanyINN: inn,
		ProductID:  pID,
		Amount:     1000000,
		TermMonths: 12,
		Purpose:    "Пополнение оборотных средств",
		Status:     domain.StatusDraft,
		CreatedAt:  now,
		UpdatedAt:  now,
		Documents:  []domain.Document{},
		History:    []domain.HistoryEntry{{From: domain.StatusDraft, To: domain.StatusDraft, At: now, By: "system"}},
	}
	a.applications[id] = app
	return app
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

	app := a.getOrCreateLocked(id, p.CompanyINN)
	if in.ProductID != nil && *in.ProductID != "" {
		app.ProductID = *in.ProductID
	}
	if in.Amount != nil && *in.Amount > 0 {
		app.Amount = *in.Amount
	}
	if in.TermMonths != nil && *in.TermMonths > 0 {
		app.TermMonths = *in.TermMonths
	}
	if in.Purpose != nil && *in.Purpose != "" {
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
	a.mu.Lock()
	defer a.mu.Unlock()

	app := a.getOrCreateLocked(id, p.CompanyINN)
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
				ProductName: demoProductName(app.ProductID),
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

	app := a.getOrCreateLocked(id, p.CompanyINN)

	now := time.Now()
	app.Status = domain.StatusManualReview
	app.SubmittedAt = &now
	app.UpdatedAt = now

	rate := 16.5
	switch app.ProductID {
	case "working_capital", "working-capital":
		rate = 15.5
	case "equipment_loan", "equipment-leasing":
		rate = 14.5
	case "auto_loan":
		rate = 15.0
	case "express", "express-revolving":
		rate = 19.5
	case "mortgage_business":
		rate = 13.0
	}

	decMap := map[string]any{
		"score":      58,
		"rate":       rate,
		"status":     "MANUAL_REVIEW",
		"thresholds": map[string]any{"approve": 65, "counter": 45},
		"manual_review_reasons": []string{"BORDERLINE_SCORE", "FINANCIAL_STATEMENTS_NEEDED"},
		"reasons_positive": []map[string]any{
			{"factor": "revenue_trend", "name": "Положительная динамика выручки", "delta": 12},
			{"factor": "business_age", "name": "Срок существования бизнеса > 3 лет", "delta": 8},
		},
		"reasons_negative": []map[string]any{
			{"factor": "debt_load", "name": "Повышенная долговая нагрузка", "delta": -14},
		},
	}
	decBytes, _ := json.Marshal(decMap)
	app.Decision = decBytes

	app.History = append(app.History, domain.HistoryEntry{
		From: domain.StatusDraft,
		To:   domain.StatusManualReview,
		At:   now,
		By:   p.UserID,
	})
	return *app, nil
}

func (a *DemoApplications) Accept(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	app := a.getOrCreateLocked(id, p.CompanyINN)
	now := time.Now()
	prev := app.Status
	app.Status = domain.StatusActive
	app.UpdatedAt = now
	app.History = append(app.History, domain.HistoryEntry{
		From:    prev,
		To:      domain.StatusActive,
		At:      now,
		By:      p.UserID,
		Comment: "Договор подписан клиентом",
	})
	return *app, nil
}

func (a *DemoApplications) Decline(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	app := a.getOrCreateLocked(id, p.CompanyINN)
	now := time.Now()
	prev := app.Status
	app.Status = domain.StatusDraft
	app.UpdatedAt = now
	app.History = append(app.History, domain.HistoryEntry{
		From:    prev,
		To:      domain.StatusDraft,
		At:      now,
		By:      p.UserID,
		Comment: "Клиент отказался от предложения, заявка возвращена в черновик",
	})
	return *app, nil
}

func (a *DemoApplications) Resume(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	app := a.getOrCreateLocked(id, p.CompanyINN)
	now := time.Now()
	prev := app.Status
	app.Status = domain.StatusManualReview
	app.UpdatedAt = now
	app.History = append(app.History, domain.HistoryEntry{
		From:    prev,
		To:      domain.StatusManualReview,
		At:      now,
		By:      p.UserID,
		Comment: "Клиент предоставил запрошенные данные",
	})
	return *app, nil
}

func (a *DemoApplications) UploadDocument(ctx context.Context, p domain.Principal, id, kind, filename string, content io.Reader) (domain.Document, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	app := a.getOrCreateLocked(id, p.CompanyINN)

	var size int64 = 1024
	if content != nil {
		data, err := io.ReadAll(content)
		if err == nil && len(data) > 0 {
			size = int64(len(data))
		}
	}

	doc := domain.Document{
		ID:         fmt.Sprintf("doc_%d", time.Now().UnixNano()),
		Kind:       kind,
		Filename:   filename,
		Size:       size,
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
		return nil
	}
	for i, doc := range app.Documents {
		if doc.ID == documentID {
			app.Documents = append(app.Documents[:i], app.Documents[i+1:]...)
			return nil
		}
	}
	return nil
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
		rate := d.Rate
		if rate <= 0 {
			rate = 15.5
		}
		var decMap map[string]any
		if len(app.Decision) > 0 {
			_ = json.Unmarshal(app.Decision, &decMap)
		}
		if decMap == nil {
			decMap = make(map[string]any)
		}
		decMap["rate"] = rate
		decMap["approved_amount"] = app.Amount
		decMap["approved_term"] = app.TermMonths
		decMap["offer"] = map[string]any{
			"product_id":  app.ProductID,
			"amount":      app.Amount,
			"term_months": app.TermMonths,
			"rate":        rate,
		}
		app.Decision, _ = json.Marshal(decMap)
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
		rate := d.Rate
		if rate <= 0 {
			rate = 16.0
		}
		counterData, _ := json.Marshal(map[string]any{
			"product_id":  app.ProductID,
			"amount":      app.Amount,
			"term_months": app.TermMonths,
			"rate":        rate,
			"reason":      d.Reason,
			"offer": map[string]any{
				"product_id":  app.ProductID,
				"amount":      app.Amount,
				"term_months": app.TermMonths,
				"rate":        rate,
			},
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

	pdfData := []byte("%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj 2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj 3 0 obj<</Type/Page/MediaBox[0 0 300 144]/Parent 2 0 R/Resources<<>>>>endobj\nxref\n0 4\n0000000000 65535 f\n0000000010 00000 n\n0000000053 00000 n\n0000000102 00000 n\ntrailer<</Size 4/Root 1 0 R>>\nstartxref\n178\n%%EOF\n")

	app, ok := r.apps.applications[id]
	if ok {
		for _, doc := range app.Documents {
			if doc.ID == documentID {
				doc.Size = int64(len(pdfData))
				return io.NopCloser(bytes.NewReader(pdfData)), doc, nil
			}
		}
	}

	doc := domain.Document{
		ID:         documentID,
		Kind:       "document",
		Filename:   "Документ.pdf",
		Size:       int64(len(pdfData)),
		Status:     domain.DocumentUploaded,
		UploadedAt: time.Now(),
	}
	return io.NopCloser(bytes.NewReader(pdfData)), doc, nil
}

func (r *DemoReview) cardLocked(app *domain.Application) ReviewCard {
	info, ok := demoCompanies[app.CompanyINN]
	if !ok {
		info = demoCompanies["7700000001"]
	}
	debt := info.Debt
	equity := info.Equity
	ratio := info.DebtToEquity
	now := time.Now()
	regDate, _ := time.Parse("2006-01-02", info.RegistrationDate)
	title := info.OKVEDTitle
	ogrn := info.OGRN
	acc := info.AccountNumber

	co := domain.Company{
		INN:              app.CompanyINN,
		OrgType:          info.OrgType,
		Name:             info.Name,
		OKVED:            info.OKVED,
		OKVEDTitle:       &title,
		OGRN:             &ogrn,
		AccountNumber:    &acc,
		RegistrationDate: &regDate,
		TotalDebt:        &debt,
		Equity:           &equity,
		DebtToEquity:     &ratio,
		FetchedAt:        &now,
	}

	productName := demoProductName(app.ProductID)

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
