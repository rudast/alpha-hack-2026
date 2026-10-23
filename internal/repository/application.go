package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

const applicationColumns = `id::text, version, company_inn, product_id, amount, term_months, purpose, status,
	form, documents, history, decision, submitted_at, created_at, updated_at`

type ApplicationRepo struct {
	db *pgxpool.Pool
}

func NewApplicationRepo(db *pgxpool.Pool) *ApplicationRepo {
	return &ApplicationRepo{db: db}
}

// Create создаёт черновик. Статус и время задаёт база (значения по умолчанию).
func (r *ApplicationRepo) Create(ctx context.Context, a domain.Application, createdBy string) (domain.Application, error) {
	form, err := formJSON(a.Form)
	if err != nil {
		return domain.Application{}, err
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO applications (company_inn, created_by, product_id, amount, term_months, purpose, form)
		VALUES ($1, $2::text::uuid, $3, $4, $5, $6, $7::jsonb)
		RETURNING `+applicationColumns,
		a.CompanyINN, createdBy, a.ProductID, a.Amount, a.TermMonths, a.Purpose, form)
	return scanApplication(row)
}

// GetForCompany возвращает заявку, только если она принадлежит компании. Владелец проверяется
// в самом запросе, поэтому чужая заявка неотличима от несуществующей (ErrNotFound).
func (r *ApplicationRepo) GetForCompany(ctx context.Context, id, inn string) (domain.Application, error) {
	row := r.db.QueryRow(ctx,
		`SELECT `+applicationColumns+` FROM applications WHERE id = $1::text::uuid AND company_inn = $2`, id, inn)
	return scanApplication(row)
}

func (r *ApplicationRepo) ListByCompany(ctx context.Context, inn string) ([]domain.ApplicationSummary, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id::text, product_id, amount, term_months, status, submitted_at, created_at
		FROM applications WHERE company_inn = $1 ORDER BY created_at DESC`, inn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.ApplicationSummary, 0)
	for rows.Next() {
		var s domain.ApplicationSummary
		var status string
		if err := rows.Scan(&s.ID, &s.ProductID, &s.Amount, &s.TermMonths, &status, &s.SubmittedAt, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.Status = domain.Status(status)
		list = append(list, s)
	}
	return list, rows.Err()
}

// UpdateDraft меняет только переданные поля черновика (COALESCE оставляет прежнее значение, если параметр NULL).
// Изменение применяется, только если version совпадает с текущей версией заявки, и увеличивает её на 1:
// так запрос, составленный по устаревшим данным, не затрёт чужие изменения.
// Не черновик: ErrConflict, устаревшая версия: ErrVersionConflict, чужая или несуществующая заявка: ErrNotFound.
func (r *ApplicationRepo) UpdateDraft(ctx context.Context, id, inn string, version int, p domain.DraftPatch) (domain.Application, error) {
	var form any
	if p.Form != nil {
		b, err := formJSON(p.Form)
		if err != nil {
			return domain.Application{}, err
		}
		form = b
	}
	row := r.db.QueryRow(ctx, `
		UPDATE applications SET
			product_id  = COALESCE($3, product_id),
			amount      = COALESCE($4, amount),
			term_months = COALESCE($5, term_months),
			purpose     = COALESCE($6, purpose),
			form        = COALESCE($7::jsonb, form),
			version     = version + 1,
			updated_at  = now()
		WHERE id = $1::text::uuid AND company_inn = $2 AND status = 'DRAFT' AND version = $8
		RETURNING `+applicationColumns,
		id, inn, p.ProductID, p.Amount, p.TermMonths, p.Purpose, form, version)
	return r.afterConditionalUpdate(ctx, row, id, inn)
}

// Submit переводит черновик в SUBMITTED и дописывает переход в history. Условие status = 'DRAFT'
// в самом запросе гарантирует, что при двойной отправке выиграет только один запрос.
func (r *ApplicationRepo) Submit(ctx context.Context, id, inn, by string, at time.Time) (domain.Application, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE applications SET
			status = 'SUBMITTED', submitted_at = $3, version = version + 1, updated_at = now(),
			history = history || jsonb_build_array(jsonb_build_object(
				'from', 'DRAFT', 'to', 'SUBMITTED', 'at', $3::timestamptz, 'by', $4::text))
		WHERE id = $1::text::uuid AND company_inn = $2 AND status = 'DRAFT'
		RETURNING `+applicationColumns,
		id, inn, at, by)
	return r.afterConditionalUpdate(ctx, row, id, inn)
}

// afterConditionalUpdate различает причины пустого результата условного UPDATE: заявки нет
// (или она чужая), заявка уже не черновик, либо версия устарела.
func (r *ApplicationRepo) afterConditionalUpdate(ctx context.Context, row pgx.Row, id, inn string) (domain.Application, error) {
	a, err := scanApplication(row)
	if !errors.Is(err, domain.ErrNotFound) {
		return a, err
	}
	current, err := r.GetForCompany(ctx, id, inn)
	if err != nil {
		return domain.Application{}, err
	}
	if current.Status != domain.StatusDraft {
		return domain.Application{}, domain.ErrConflict
	}
	return domain.Application{}, domain.ErrVersionConflict
}

func scanApplication(row pgx.Row) (domain.Application, error) {
	var a domain.Application
	var status string
	var form, history, documents, decision []byte
	err := row.Scan(&a.ID, &a.Version, &a.CompanyINN, &a.ProductID, &a.Amount, &a.TermMonths, &a.Purpose, &status,
		&form, &documents, &history, &decision, &a.SubmittedAt, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return domain.Application{}, mapNotFound(err)
	}
	a.Status = domain.Status(status)
	if err := json.Unmarshal(form, &a.Form); err != nil {
		return domain.Application{}, err
	}
	if err := json.Unmarshal(history, &a.History); err != nil {
		return domain.Application{}, err
	}
	a.Documents = documents
	a.Decision = decision
	return a, nil
}

func formJSON(form map[string]any) (string, error) {
	if form == nil {
		return "{}", nil
	}
	b, err := json.Marshal(form)
	return string(b), err
}

// mapNotFound превращает «нет строки» и «некорректный UUID в адресе» (SQLSTATE 22P02) в ErrNotFound.
func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
		return domain.ErrNotFound
	}
	return err
}
