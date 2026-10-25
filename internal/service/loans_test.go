package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/catalog"
	"github.com/rudast/alpha-hack-2026/internal/domain"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func loanApp(status domain.Status, amount int64, rate float64, term, paid int, issued time.Time) domain.Application {
	return domain.Application{
		ID: "L", ProductID: "working_capital", Status: status, Amount: amount, TermMonths: term,
		Rate: &rate, IssuedAt: &issued, PaidPayments: paid,
	}
}

// Цифры из таблиц ТЗ фронта (BACKEND_TASKS.md, задача 7): остаток долга и платёж действующих кредитов.
func TestBuildLoan_ActiveMatchesTheFrontendFigures(t *testing.T) {
	today := day(2026, 10, 25)
	cases := []struct {
		name          string
		amount        int64
		rate          float64
		term, paid    int
		principalLeft int64
		payment       float64
	}{
		{"working_capital", 2_000_000, 19, 18, 7, 1_288_615, 128_566.97},
		{"auto_loan", 1_500_000, 15.5, 36, 20, 752_580, 52_366.02},
		{"equipment_loan", 2_400_000, 16.5, 48, 26, 1_295_324, 68_632.82},
		{"ooo_loan", 3_000_000, 20, 18, 5, 2_254_100, 194_289.60},
		{"credit_line", 1_500_000, 17, 24, 8, 1_055_098, 74_163.40},
		{"express", 600_000, 20.5, 12, 4, 413_388, 55_724.38},
	}
	for _, tc := range cases {
		l := buildLoan(loanApp(domain.StatusActive, tc.amount, tc.rate, tc.term, tc.paid, day(2026, 3, 15)), today)
		if l.PrincipalLeft != tc.principalLeft || l.MonthlyPayment != tc.payment {
			t.Errorf("%s: principal left %d, payment %.2f; want %d and %.2f", tc.name, l.PrincipalLeft, l.MonthlyPayment, tc.principalLeft, tc.payment)
		}
	}
}

func TestBuildLoan_ExampleFromTheContract(t *testing.T) {
	// Пример из ответа GET /loans: выдан 15.03.2026, 7 платежей внесено, сегодня 25.10.2026.
	l := buildLoan(loanApp(domain.StatusActive, 2_000_000, 19, 18, 7, day(2026, 3, 15)), day(2026, 10, 25))

	if l.PaidTotal != 899_969 || l.PaymentsMade != 7 || l.Status != domain.StatusActive {
		t.Fatalf("paid total %d, made %d, status %s", l.PaidTotal, l.PaymentsMade, l.Status)
	}
	if !l.MaturityDate.Equal(day(2027, 9, 15)) {
		t.Fatalf("maturity %v", l.MaturityDate)
	}
	if l.NextPayment == nil || !l.NextPayment.Date.Equal(day(2026, 11, 15)) || l.NextPayment.Status != PaymentNext {
		t.Fatalf("next payment %+v", l.NextPayment)
	}
	first := l.Schedule[0]
	if !first.Date.Equal(day(2026, 4, 15)) || first.Principal != 96_900.3 || first.Interest != 31_666.67 || first.BalanceAfter != 1_903_099.7 || first.Status != PaymentPaid {
		t.Fatalf("first payment %+v", first)
	}
	if len(l.Schedule) != 18 || l.Schedule[17].BalanceAfter != 0 {
		t.Fatalf("schedule must cover the whole term and end at zero, got %d payments, last balance %v", len(l.Schedule), l.Schedule[17].BalanceAfter)
	}
}

func TestBuildLoan_OverdueWhenTheNextPaymentDatePassed(t *testing.T) {
	// Выдан 10.04.2026, внесено 5: платёж № 6 был 10.10.2026, сегодня 25.10.2026.
	l := buildLoan(loanApp(domain.StatusActive, 3_000_000, 20, 18, 5, day(2026, 4, 10)), day(2026, 10, 25))
	if l.Status != domain.StatusOverdue || l.Schedule[5].Status != PaymentOverdue || l.OverdueAmount != 194_289.60 {
		t.Fatalf("status %s, payment 6 %s, overdue amount %v", l.Status, l.Schedule[5].Status, l.OverdueAmount)
	}
	if l.Schedule[6].Status != PaymentPlanned {
		t.Fatalf("payment 7 must be planned, got %s", l.Schedule[6].Status)
	}
}

// Переплата закрытых кредитов из ТЗ фронта (задача 7).
func TestBuildLoan_ClosedMatchesTheFrontendFigures(t *testing.T) {
	cases := []struct {
		name        string
		amount      int64
		rate        float64
		term, paid  int
		issued      time.Time
		overpayment int64
	}{
		{"working_capital", 1_500_000, 18, 18, 18, day(2023, 4, 12), 222_756},
		{"express early", 500_000, 21, 12, 7, day(2024, 11, 20), 46_944},
		{"ooo_loan", 800_000, 20, 12, 12, day(2023, 9, 5), 89_291},
		{"auto_loan", 2_000_000, 14.5, 36, 36, day(2020, 10, 15), 478_310},
		{"loan_up_to_1m early", 900_000, 21.5, 12, 9, day(2023, 3, 1), 99_453},
		{"working_capital late", 2_500_000, 17, 24, 24, day(2022, 7, 18), 466_536},
		{"ip_loan", 400_000, 22, 12, 12, day(2024, 2, 10), 49_253},
	}
	for _, tc := range cases {
		l := buildLoan(loanApp(domain.StatusClosed, tc.amount, tc.rate, tc.term, tc.paid, tc.issued), day(2026, 10, 25))
		if l.Overpayment != tc.overpayment {
			t.Errorf("%s: overpayment %d, want %d", tc.name, l.Overpayment, tc.overpayment)
		}
		if len(l.Schedule) != tc.paid || l.PrincipalLeft != 0 || l.NextPayment != nil || l.Status != domain.StatusClosed {
			t.Errorf("%s: %d payments, left %d, next %v, status %s", tc.name, len(l.Schedule), l.PrincipalLeft, l.NextPayment, l.Status)
		}
		if l.ClosedEarly != (tc.paid < tc.term) || l.MonthsEarly != tc.term-tc.paid {
			t.Errorf("%s: closed early %v, months early %d", tc.name, l.ClosedEarly, l.MonthsEarly)
		}
		if math.Abs(l.Schedule[tc.paid-1].BalanceAfter) > 0 {
			t.Errorf("%s: the last paid payment must close the loan", tc.name)
		}
	}
}

func TestPaymentDate_ShortMonth(t *testing.T) {
	issued := day(2026, 1, 31)
	if got := paymentDate(issued, 1); !got.Equal(day(2026, 2, 28)) {
		t.Fatalf("got %v, want the last day of February", got)
	}
	if got := paymentDate(issued, 2); !got.Equal(day(2026, 3, 31)) {
		t.Fatalf("got %v, want 31 March", got)
	}
}

type fakeLoanRepo struct{ apps []domain.Application }

func (f fakeLoanRepo) ListLoans(context.Context, string) ([]domain.Application, error) {
	return f.apps, nil
}

func TestLoans_SplitsSortsAndReadsOtherBanks(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	active := loanApp(domain.StatusActive, 2_000_000, 19, 18, 7, day(2026, 3, 15))
	overdue := loanApp(domain.StatusActive, 3_000_000, 20, 18, 5, day(2026, 4, 10))
	overdue.ID = "overdue"
	closedOld := loanApp(domain.StatusClosed, 800_000, 20, 12, 12, day(2023, 9, 5))
	closedOld.ID = "old"
	closedNew := loanApp(domain.StatusClosed, 400_000, 22, 12, 12, day(2024, 2, 10))
	closedNew.ID = "new"

	company := domain.Company{CreditHistory: json.RawMessage(`{"other_banks":[{"bank":"Сбербанк","kind":"Кредит на оборотные средства","debt":958805}]}`)}
	s := NewLoanService(fakeLoanRepo{apps: []domain.Application{active, closedOld, overdue, closedNew}}, &fakeCompanies{c: company}, cat)
	s.now = func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }

	got, err := s.Loans(context.Background(), "7700000001")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Active) != 2 || got.Active[0].ID != "overdue" || got.Active[0].ProductName == "" {
		t.Fatalf("active: %+v", got.Active)
	}
	if len(got.Closed) != 2 || got.Closed[0].ID != "new" {
		t.Fatalf("closed must be newest first: %+v", got.Closed)
	}
	if len(got.OtherBanks) != 1 || got.OtherBanks[0].Debt != 958_805 {
		t.Fatalf("other banks: %+v", got.OtherBanks)
	}
}
