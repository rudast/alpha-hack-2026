package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

func TestDebtTotal_OurLoansPlusOtherBanks(t *testing.T) {
	// Клиент 1 из демо-данных: оборотный 2 млн (внесено 7) и автокредит 1,5 млн (внесено 20) у нас,
	// 958 805 в Сбербанке. Вместе — 3 000 000, как total_debt в сиде.
	working := loanApp(domain.StatusActive, 2_000_000, 19, 18, 7, day(2026, 3, 15))
	auto := loanApp(domain.StatusActive, 1_500_000, 15.5, 36, 20, day(2025, 2, 5))
	closed := loanApp(domain.StatusClosed, 500_000, 21, 12, 7, day(2024, 11, 20)) // закрытый не считается
	fetched := testNow
	company := domain.Company{INN: "7700000001", FetchedAt: &fetched,
		CreditHistory: json.RawMessage(`{"other_banks":[{"bank":"Сбербанк","debt":958805}]}`)}

	s := NewDebtService(fakeLoanRepo{apps: []domain.Application{working, auto, closed}}, &fakeCompanies{c: company})
	s.now = func() time.Time { return testNow }
	total, err := s.Total(context.Background(), company)
	if err != nil || total != 3_000_000 {
		t.Fatalf("total %d, %v; want 3 000 000", total, err)
	}
}

func TestDebtRecalculate_NewLoanRaisesDebtAndRatio(t *testing.T) {
	equity := int64(4_000_000)
	fetched := testNow
	companies := &fakeCompanies{c: domain.Company{INN: "7700000002", Equity: &equity, FetchedAt: &fetched,
		CreditHistory: json.RawMessage(`{"other_banks":[{"bank":"ВТБ","debt":2400000}]}`)}}
	loans := &fakeLoanRepo{}
	s := NewDebtService(loans, companies)
	s.now = func() time.Time { return testNow }

	c, err := s.Recalculate(context.Background(), "7700000002")
	if err != nil || *c.TotalDebt != 2_400_000 || *c.DebtToEquity != 0.6 {
		t.Fatalf("before: debt %v, ratio %v, %v", c.TotalDebt, c.DebtToEquity, err)
	}

	// Выдан кредит 3 млн сегодня: долг вырос на всю сумму, нагрузка 5,4 млн / 4 млн = 1,35.
	loans.apps = []domain.Application{loanApp(domain.StatusActive, 3_000_000, 20, 12, 0, dateOf(testNow))}
	c, err = s.Recalculate(context.Background(), "7700000002")
	if err != nil || *c.TotalDebt != 5_400_000 || *c.DebtToEquity != 1.35 {
		t.Fatalf("after: debt %v, ratio %v, %v", c.TotalDebt, c.DebtToEquity, err)
	}
}

func TestDebtRecalculate_UnknownBeforeExternalData(t *testing.T) {
	companies := &fakeCompanies{c: domain.Company{INN: "1"}}
	c, err := NewDebtService(fakeLoanRepo{}, companies).Recalculate(context.Background(), "1")
	if err != nil || c.TotalDebt != nil {
		t.Fatalf("debt must stay unknown until the worker answered, got %v, %v", c.TotalDebt, err)
	}
}

func TestApplicationAccept_RecalculatesDebt(t *testing.T) {
	s, repo := newApplications(t, "OOO")
	recalc := &countingDebts{}
	s.debts = recalc
	a := decided(repo, domain.StatusApproved, `{"outcome":"APPROVED","rate":18.4}`)
	if _, err := s.Accept(context.Background(), clientA, a.ID); err != nil {
		t.Fatal(err)
	}
	if recalc.calls != 1 || recalc.inn != clientA.CompanyINN {
		t.Fatalf("debt recalculated %d times for %q, want once for the client's company", recalc.calls, recalc.inn)
	}
}

func TestOfferCurrent_RevokedWhenDebtChanged(t *testing.T) {
	ctx := context.Background()
	c := company(withHistory)
	s, repo := newOffers(t, c)
	first, _, _ := s.Current(ctx, "7700000001")

	// Тот же момент обновления внешних данных, но выдан новый кредит: долг вырос.
	c.TotalDebt = i64p(4_000_000)
	s.companies = &fakeCompanies{c: c}
	second, _, _ := s.Current(ctx, "7700000001")
	if repo.items[first.ID].Status != domain.OfferRevoked || second == nil || second.MaxAmount >= first.MaxAmount {
		t.Fatalf("old status %s, new %+v", repo.items[first.ID].Status, second)
	}
}

type countingDebts struct {
	calls int
	inn   string
}

func (c *countingDebts) Recalculate(_ context.Context, inn string) (domain.Company, error) {
	c.calls++
	c.inn = inn
	return domain.Company{}, nil
}
