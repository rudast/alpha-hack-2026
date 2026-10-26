package repository

import (
	"context"
	"encoding/json"
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

// UpdateExternal записывает данные внешних источников: капитал и кредиты в других банках
// (в credit_history.other_banks, остальные поля credit_history сохраняются). Общий долг после этого
// пересчитывает DebtService. Запись идемпотентна: параллельные записи одних и тех же данных безвредны.
func (r *CompanyRepo) UpdateExternal(ctx context.Context, inn string, equity int64, otherBanks []domain.OtherBankLoan, fetchedAt time.Time) error {
	// nil — воркер не прислал other_banks (старый формат ответа): прежний список не трогаем.
	var others any
	if otherBanks != nil {
		b, err := json.Marshal(otherBanks)
		if err != nil {
			return err
		}
		others = string(b)
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE companies
		SET equity = $2,
			credit_history = CASE WHEN $3::jsonb IS NULL THEN credit_history
				ELSE credit_history || jsonb_build_object('other_banks', $3::jsonb) END,
			financials_fetched_at = $4, updated_at = now()
		WHERE inn = $1`,
		inn, equity, others, fetchedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// UpdateDebt записывает общий долг и пересчитывает долговую нагрузку (NULL, если капитал не больше нуля).
func (r *CompanyRepo) UpdateDebt(ctx context.Context, inn string, debt int64) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE companies
		SET total_debt = $2::bigint,
			debt_to_equity = CASE WHEN equity > 0 THEN round($2::bigint::numeric / equity, 4) END,
			updated_at = now()
		WHERE inn = $1`,
		inn, debt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
