package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

const offerColumns = `id::text, company_inn, product_id, max_amount, term_months, rate_from::float8, status,
	basis, valid_until, created_at, used_at`

type OfferRepo struct {
	db *pgxpool.Pool
}

func NewOfferRepo(db *pgxpool.Pool) *OfferRepo {
	return &OfferRepo{db: db}
}

// Active возвращает действующее предложение компании (статус ACTIVE; срок проверяет сервис).
func (r *OfferRepo) Active(ctx context.Context, inn string) (domain.Offer, error) {
	row := r.db.QueryRow(ctx, `SELECT `+offerColumns+` FROM offers WHERE company_inn = $1 AND status = 'ACTIVE'`, inn)
	return scanOffer(row)
}

// GetForCompany возвращает предложение, только если оно принадлежит компании: чужое неотличимо
// от несуществующего (ErrNotFound).
func (r *OfferRepo) GetForCompany(ctx context.Context, id, inn string) (domain.Offer, error) {
	row := r.db.QueryRow(ctx, `SELECT `+offerColumns+` FROM offers WHERE id = $1::text::uuid AND company_inn = $2`, id, inn)
	return scanOffer(row)
}

// Create сохраняет новое действующее предложение. Если у компании уже есть действующее
// (его только что создал параллельный запрос), ничего не пишет и возвращает false.
func (r *OfferRepo) Create(ctx context.Context, o domain.Offer) (domain.Offer, bool, error) {
	basis, err := json.Marshal(o.Basis)
	if err != nil {
		return domain.Offer{}, false, err
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO offers (company_inn, product_id, max_amount, term_months, rate_from, basis, valid_until)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
		ON CONFLICT (company_inn) WHERE status = 'ACTIVE' DO NOTHING
		RETURNING `+offerColumns,
		o.CompanyINN, o.ProductID, o.MaxAmount, o.TermMonths, o.RateFrom, string(basis), o.ValidUntil)
	created, err := scanOffer(row)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Offer{}, false, nil
	}
	if err != nil {
		return domain.Offer{}, false, err
	}
	return created, true, nil
}

// Close переводит действующее предложение в EXPIRED или REVOKED. Уже закрытое не трогает.
func (r *OfferRepo) Close(ctx context.Context, id, status string) error {
	_, err := r.db.Exec(ctx, `UPDATE offers SET status = $2 WHERE id = $1::text::uuid AND status = 'ACTIVE'`, id, status)
	return err
}

func scanOffer(row pgx.Row) (domain.Offer, error) {
	var o domain.Offer
	var basis []byte
	err := row.Scan(&o.ID, &o.CompanyINN, &o.ProductID, &o.MaxAmount, &o.TermMonths, &o.RateFrom, &o.Status,
		&basis, &o.ValidUntil, &o.CreatedAt, &o.UsedAt)
	if err != nil {
		return domain.Offer{}, mapNotFound(err)
	}
	if err := json.Unmarshal(basis, &o.Basis); err != nil {
		return domain.Offer{}, err
	}
	return o, nil
}
