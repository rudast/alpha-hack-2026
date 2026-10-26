package service

import (
	"context"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type DebtCompanies interface {
	GetByINN(ctx context.Context, inn string) (domain.Company, error)
	UpdateDebt(ctx context.Context, inn string, debt int64) error
}

// DebtService считает общий долг компании: остаток по кредитам, выданным нашим банком, плюс долг
// в других банках из ответа воркера (companies.credit_history.other_banks). О своих кредитах банк
// знает сам, поэтому они не берутся из внешних источников и учитываются сразу после выдачи.
// Долг пересчитывается после обновления внешних данных и после выдачи кредита; от него зависят
// долговая нагрузка (D/E), скоринг и предварительное предложение.
type DebtService struct {
	loans     LoanRepository
	companies DebtCompanies
	now       func() time.Time
}

func NewDebtService(loans LoanRepository, companies DebtCompanies) *DebtService {
	return &DebtService{loans: loans, companies: companies, now: time.Now}
}

// Recalculate пересчитывает и сохраняет общий долг компании и возвращает её с новыми значениями.
// Пока внешние данные ни разу не получены (нет капитала и кредитов в других банках), долг не известен
// и не записывается.
func (s *DebtService) Recalculate(ctx context.Context, inn string) (domain.Company, error) {
	company, err := s.companies.GetByINN(ctx, inn)
	if err != nil {
		return domain.Company{}, err
	}
	if company.FetchedAt == nil {
		return company, nil
	}
	debt, err := s.Total(ctx, company)
	if err != nil {
		return domain.Company{}, err
	}
	if company.TotalDebt != nil && *company.TotalDebt == debt {
		return company, nil
	}
	if err := s.companies.UpdateDebt(ctx, inn, debt); err != nil {
		return domain.Company{}, err
	}
	return s.companies.GetByINN(ctx, inn)
}

// Total — наши действующие кредиты (остаток основного долга) плюс долг в других банках.
func (s *DebtService) Total(ctx context.Context, company domain.Company) (int64, error) {
	apps, err := s.loans.ListLoans(ctx, company.INN)
	if err != nil {
		return 0, err
	}
	today := dateOf(s.now())
	var total int64
	for _, a := range apps {
		if a.Status == domain.StatusClosed || a.Rate == nil || a.IssuedAt == nil || a.TermMonths <= 0 {
			continue
		}
		total += buildLoan(a, today).PrincipalLeft
	}
	for _, other := range otherBanks(company.CreditHistory) {
		total += other.Debt
	}
	return total, nil
}
