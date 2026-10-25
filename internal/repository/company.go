package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type CompanyRepo struct {
	db *pgxpool.Pool
}

func NewCompanyRepo(db *pgxpool.Pool) *CompanyRepo {
	return &CompanyRepo{db: db}
}

func (r *CompanyRepo) GetByINN(ctx context.Context, inn string) (domain.Company, error) {
	var c domain.Company
	var registry, creditHistory, financials []byte
	err := r.db.QueryRow(ctx, `
		SELECT inn, org_type, name, okved, okved_title, ogrn, account_number, registration_date,
			total_debt, equity, debt_to_equity, financials_fetched_at, registry, credit_history, financials
		FROM companies WHERE inn = $1`, inn,
	).Scan(&c.INN, &c.OrgType, &c.Name, &c.OKVED, &c.OKVEDTitle, &c.OGRN, &c.AccountNumber, &c.RegistrationDate,
		&c.TotalDebt, &c.Equity, &c.DebtToEquity, &c.FetchedAt, &registry, &creditHistory, &financials)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Company{}, domain.ErrNotFound
	}
	c.Registry, c.CreditHistory, c.Financials = registry, creditHistory, financials
	return c, err
}

// UpdateFinancials идемпотентно записывает показатели из кредитной истории:
// параллельные записи одних и тех же данных безвредны.
func (r *CompanyRepo) UpdateFinancials(ctx context.Context, inn string, debt, equity int64, fetchedAt time.Time) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE companies
		SET total_debt = $2, equity = $3, debt_to_equity = $4, financials_fetched_at = $5, updated_at = now()
		WHERE inn = $1`,
		inn, debt, equity, domain.DebtToEquity(debt, equity), fetchedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
