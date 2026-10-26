package service

import (
	"cmp"
	"context"
	"encoding/json"
	"log"
	"math"
	"slices"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/catalog"
	"github.com/rudast/alpha-hack-2026/internal/domain"
)

// Статусы платежа в графике.
const (
	PaymentPaid    = "PAID"
	PaymentNext    = "NEXT"
	PaymentOverdue = "OVERDUE"
	PaymentPlanned = "PLANNED"
)

type LoanRepository interface {
	ListLoans(ctx context.Context, inn string) ([]domain.Application, error)
}

// Payment — один платёж графика.
type Payment struct {
	Number       int
	Date         time.Time
	Amount       float64
	Principal    float64
	Interest     float64
	BalanceAfter float64
	Status       string
}

// Loan — выданный кредит с посчитанным графиком. Кредит это заявка в ACTIVE, OVERDUE или CLOSED;
// график не хранится, а считается из суммы, ставки, срока, даты выдачи и числа внесённых платежей.
type Loan struct {
	ID             string
	ProductID      string
	ProductName    string
	ContractNumber string
	Amount         int64
	Rate           float64
	TermMonths     int
	IssuedAt       time.Time
	MaturityDate   time.Time
	Status         domain.Status // ACTIVE, OVERDUE (по графику) или CLOSED
	MonthlyPayment float64
	PrincipalLeft  int64
	PaidTotal      int64
	PaymentsMade   int
	NextPayment    *Payment
	OverdueAmount  float64
	Schedule       []Payment

	// Только у закрытых.
	ClosedAt     *time.Time
	ClosedEarly  bool
	MonthsEarly  int
	Overpayment  int64
	LatePayments int
}

// OtherBankLoan — кредит компании в другом банке (из ответа БКИ, companies.credit_history.other_banks).
type OtherBankLoan = domain.OtherBankLoan

// Loans — всё для разделов «Мои кредиты», «Кредитная история» и «Кредиты в других банках».
type Loans struct {
	Active     []Loan
	Closed     []Loan
	OtherBanks []OtherBankLoan
}

type LoanService struct {
	apps      LoanRepository
	companies CompanyGetter
	catalog   *catalog.Catalog
	now       func() time.Time
}

func NewLoanService(apps LoanRepository, companies CompanyGetter, cat *catalog.Catalog) *LoanService {
	return &LoanService{apps: apps, companies: companies, catalog: cat, now: time.Now}
}

// Loans возвращает кредиты компании: действующие (просроченные первыми), закрытые (новые первыми)
// и кредиты в других банках.
func (s *LoanService) Loans(ctx context.Context, inn string) (Loans, error) {
	company, err := s.companies.GetByINN(ctx, inn)
	if err != nil {
		return Loans{}, err
	}
	apps, err := s.apps.ListLoans(ctx, inn)
	if err != nil {
		return Loans{}, err
	}

	today := dateOf(s.now())
	out := Loans{Active: []Loan{}, Closed: []Loan{}, OtherBanks: otherBanks(company.CreditHistory)}
	for _, a := range apps {
		if a.Rate == nil || a.IssuedAt == nil || a.TermMonths <= 0 {
			log.Printf("loans: application %s has no rate, issue date or term, skipped", a.ID)
			continue
		}
		loan := buildLoan(a, today)
		if p, ok := s.catalog.Product(a.ProductID); ok {
			loan.ProductName = p.Name
		}
		if loan.Status == domain.StatusClosed {
			out.Closed = append(out.Closed, loan)
		} else {
			out.Active = append(out.Active, loan)
		}
	}
	slices.SortStableFunc(out.Active, func(x, y Loan) int {
		return cmp.Compare(overdueRank(y), overdueRank(x))
	})
	slices.SortStableFunc(out.Closed, func(x, y Loan) int {
		return y.ClosedAt.Compare(*x.ClosedAt)
	})
	return out, nil
}

func overdueRank(l Loan) int {
	if l.Status == domain.StatusOverdue {
		return 1
	}
	return 0
}

// buildLoan считает график и показатели кредита на дату today.
func buildLoan(a domain.Application, today time.Time) Loan {
	issued := dateOf(*a.IssuedAt)
	closed := a.Status == domain.StatusClosed
	paid := min(a.PaidPayments, a.TermMonths)

	loan := Loan{
		ID:             a.ID,
		ProductID:      a.ProductID,
		Amount:         a.Amount,
		Rate:           *a.Rate,
		TermMonths:     a.TermMonths,
		IssuedAt:       issued,
		MaturityDate:   paymentDate(issued, a.TermMonths),
		MonthlyPayment: annuity(a.Amount, *a.Rate, a.TermMonths),
		PaymentsMade:   paid,
		LatePayments:   a.LatePayments,
	}
	if a.ContractNumber != nil {
		loan.ContractNumber = *a.ContractNumber
	}

	if closed {
		// У закрытого кредита в графике только внесённые платежи; при досрочном закрытии
		// последний из них гасит весь остаток.
		loan.Schedule = schedule(a.Amount, *a.Rate, a.TermMonths, issued, paid)
		for i := range loan.Schedule {
			loan.Schedule[i].Status = PaymentPaid
		}
		loan.Status = domain.StatusClosed
		loan.PaidTotal = paidTotal(loan.Schedule, paid)
		loan.Overpayment = loan.PaidTotal - a.Amount
		loan.ClosedAt = a.ClosedAt
		if loan.ClosedAt == nil && paid > 0 {
			d := loan.Schedule[paid-1].Date
			loan.ClosedAt = &d
		}
		loan.ClosedEarly = paid < a.TermMonths
		loan.MonthsEarly = a.TermMonths - paid
		return loan
	}

	loan.Schedule = schedule(a.Amount, *a.Rate, a.TermMonths, issued, a.TermMonths)
	loan.Status = domain.StatusActive
	for i := range loan.Schedule {
		p := &loan.Schedule[i]
		switch {
		case p.Number <= paid:
			p.Status = PaymentPaid
		case p.Number == paid+1 && p.Date.Before(today):
			p.Status = PaymentOverdue
			loan.Status = domain.StatusOverdue
			loan.OverdueAmount = p.Amount
		case p.Number == paid+1:
			p.Status = PaymentNext
		default:
			p.Status = PaymentPlanned
		}
	}
	loan.PaidTotal = paidTotal(loan.Schedule, paid)
	loan.PrincipalLeft = a.Amount
	if paid > 0 {
		loan.PrincipalLeft = int64(math.Round(loan.Schedule[paid-1].BalanceAfter))
	}
	if paid < a.TermMonths {
		next := loan.Schedule[paid]
		loan.NextPayment = &next
	}
	return loan
}

// annuity — ежемесячный аннуитетный платёж, округлённый до копеек.
func annuity(amount int64, rate float64, term int) float64 {
	r := rate / 100 / 12
	return round2(float64(amount) * r / (1 - math.Pow(1+r, -float64(term))))
}

// schedule строит первые last платежей аннуитетного графика на term месяцев. Платёж № last гасит
// весь остаток: на полном сроке это последний платёж, при досрочном закрытии — досрочный.
func schedule(amount int64, rate float64, term int, issued time.Time, last int) []Payment {
	r := rate / 100 / 12
	payment := annuity(amount, rate, term)
	balance := float64(amount)
	out := make([]Payment, 0, last)
	for n := 1; n <= last; n++ {
		interest := round2(balance * r)
		total := payment
		if n == last {
			total = round2(balance + interest)
		}
		principal := round2(total - interest)
		balance = math.Max(0, round2(balance-principal))
		out = append(out, Payment{
			Number: n, Date: paymentDate(issued, n),
			Amount: total, Principal: principal, Interest: interest, BalanceAfter: balance,
		})
	}
	return out
}

func paidTotal(s []Payment, paid int) int64 {
	var sum float64
	for _, p := range s[:paid] {
		sum += p.Amount
	}
	return int64(math.Round(sum))
}

// paymentDate — дата платежа № n: дата выдачи плюс n месяцев; если такого дня в месяце нет
// (31-е в коротком месяце), берётся последний день месяца.
func paymentDate(issued time.Time, n int) time.Time {
	first := time.Date(issued.Year(), issued.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	lastDay := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(issued.Day(), lastDay), 0, 0, 0, 0, time.UTC)
}

func dateOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

func otherBanks(creditHistory json.RawMessage) []OtherBankLoan {
	var h struct {
		OtherBanks []OtherBankLoan `json:"other_banks"`
	}
	_ = json.Unmarshal(creditHistory, &h)
	if h.OtherBanks == nil {
		return []OtherBankLoan{}
	}
	return h.OtherBanks
}
