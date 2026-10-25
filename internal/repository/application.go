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
	form, documents, history, decision, submitted_at, created_at, updated_at, offer_id::text`

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

// CreateFromOffer создаёт черновик по предварительному предложению (a.OfferID). По одному предложению
// бывает не больше одной заявки (уникальный индекс): если она уже есть, возвращается она и false.
func (r *ApplicationRepo) CreateFromOffer(ctx context.Context, a domain.Application, createdBy string) (domain.Application, bool, error) {
	form, err := formJSON(a.Form)
	if err != nil {
		return domain.Application{}, false, err
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO applications (company_inn, created_by, product_id, amount, term_months, purpose, form, offer_id)
		VALUES ($1, $2::text::uuid, $3, $4, $5, $6, $7::jsonb, $8::text::uuid)
		ON CONFLICT (offer_id) WHERE offer_id IS NOT NULL DO NOTHING
		RETURNING `+applicationColumns,
		a.CompanyINN, createdBy, a.ProductID, a.Amount, a.TermMonths, a.Purpose, form, a.OfferID)
	created, err := scanApplication(row)
	if !errors.Is(err, domain.ErrNotFound) {
		return created, err == nil, err
	}
	row = r.db.QueryRow(ctx,
		`SELECT `+applicationColumns+` FROM applications WHERE offer_id = $1::text::uuid AND company_inn = $2`, a.OfferID, a.CompanyINN)
	existing, err := scanApplication(row)
	return existing, false, err
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
// unlinkOffer отвязывает заявку от предварительного предложения (клиент сменил продукт).
// Не черновик: ErrConflict, устаревшая версия: ErrVersionConflict, чужая или несуществующая заявка: ErrNotFound.
func (r *ApplicationRepo) UpdateDraft(ctx context.Context, id, inn string, version int, p domain.DraftPatch, unlinkOffer bool) (domain.Application, error) {
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
			offer_id    = CASE WHEN $9 THEN NULL ELSE offer_id END,
			version     = version + 1,
			updated_at  = now()
		WHERE id = $1::text::uuid AND company_inn = $2 AND status = 'DRAFT' AND version = $8
		RETURNING `+applicationColumns,
		id, inn, p.ProductID, p.Amount, p.TermMonths, p.Purpose, form, version, unlinkOffer)
	return r.afterConditionalUpdate(ctx, row, id, inn)
}

// Submit переводит черновик в SUBMITTED и дописывает переход в history. Условие status = 'DRAFT'
// в самом запросе гарантирует, что при двойной отправке выиграет только один запрос.
// Если заявка создана по предварительному предложению, в той же транзакции предложение становится USED;
// если оно уже не действует (истекло, отозвано), отправка откатывается с domain.ErrOfferInvalid.
func (r *ApplicationRepo) Submit(ctx context.Context, id, inn, by string, at time.Time) (domain.Application, error) {
	var submitted domain.Application
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			UPDATE applications SET
				status = 'SUBMITTED', submitted_at = $3, version = version + 1, updated_at = now(),
				history = history || jsonb_build_array(jsonb_build_object(
					'from', 'DRAFT', 'to', 'SUBMITTED', 'at', $3::timestamptz, 'by', $4::text))
			WHERE id = $1::text::uuid AND company_inn = $2 AND status = 'DRAFT'
			RETURNING `+applicationColumns,
			id, inn, at, by)
		a, err := scanApplication(row)
		if err != nil {
			return err
		}
		if a.OfferID != nil {
			tag, err := tx.Exec(ctx, `
				UPDATE offers SET status = 'USED', used_at = $2
				WHERE id = $1::text::uuid AND status = 'ACTIVE' AND valid_until > $2`, *a.OfferID, at)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return domain.ErrOfferInvalid
			}
		}
		submitted = a
		return nil
	})
	if errors.Is(err, domain.ErrNotFound) {
		return r.conflictReason(ctx, id, inn)
	}
	return submitted, err
}

// ApplyResult применяет результат воркера: переводит заявку из res.From в res.To, дописывает переход
// в history (автор «worker») и, если пришло решение, записывает его в decision.
//
// Условие status = res.From делает применение идемпотентным: повторное сообщение ничего не меняет.
// Если строка не обновилась, различаются три случая: заявки нет (ErrNotFound), результат уже применён,
// то есть статус уже равен res.To (ErrAlreadyApplied), либо статус другой (ErrStatusMismatch).
// Метод не проверяет владельца: результаты приходят от воркера, а не от клиента.
func (r *ApplicationRepo) ApplyResult(ctx context.Context, res domain.ApplicationResult, at time.Time) error {
	var decision any
	if len(res.Decision) > 0 {
		decision = string(res.Decision)
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE applications SET
			status     = $3,
			version    = version + 1,
			updated_at = now(),
			decision   = COALESCE($4::jsonb, decision),
			history    = history || jsonb_build_array(
				jsonb_build_object('from', $2::text, 'to', $3::text, 'at', $5::timestamptz, 'by', $6::text)
				|| CASE WHEN $7::text = '' THEN '{}'::jsonb ELSE jsonb_build_object('comment', $7::text) END)
		WHERE id = $1::text::uuid AND status = $2`,
		res.ApplicationID, string(res.From), string(res.To), decision, at, "worker", res.Comment)
	if err != nil {
		return mapNotFound(err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}

	var current string
	err = r.db.QueryRow(ctx, `SELECT status FROM applications WHERE id = $1::text::uuid`, res.ApplicationID).Scan(&current)
	if err != nil {
		return mapNotFound(err)
	}
	if domain.Status(current) == res.To {
		return domain.ErrAlreadyApplied
	}
	return domain.ErrStatusMismatch
}

// MarkEnqueued отмечает, что задача по заявке только что отправлена воркеру (enqueued_at = now()).
// Версию и updated_at не трогает: для клиента заявка не меняется. Если статус уже сменился, метка не нужна.
func (r *ApplicationRepo) MarkEnqueued(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE applications SET enqueued_at = now() WHERE id = $1::text::uuid AND status = 'SUBMITTED'`, id)
	return err
}

// ClaimStaleSubmitted находит заявки, которые слишком долго остаются в SUBMITTED, и помечает их
// (enqueued_at = now()), чтобы следующая проверка не взяла их снова раньше срока.
//
// «Слишком долго» считается от последней отправки в очередь, а если её не было, то от submitted_at.
// FOR UPDATE SKIP LOCKED нужен для нескольких экземпляров API: строку, которую уже взял другой
// экземпляр, этот пропускает, и одну заявку не отправляют дважды одновременно.
func (r *ApplicationRepo) ClaimStaleSubmitted(ctx context.Context, olderThan time.Duration, limit int) ([]domain.Application, error) {
	rows, err := r.db.Query(ctx, `
		UPDATE applications SET enqueued_at = now()
		WHERE id IN (
			SELECT id FROM applications
			WHERE status = 'SUBMITTED'
			  AND COALESCE(enqueued_at, submitted_at) < now() - make_interval(secs => $1::double precision)
			ORDER BY COALESCE(enqueued_at, submitted_at)
			LIMIT $2
			FOR UPDATE SKIP LOCKED)
		RETURNING `+applicationColumns,
		olderThan.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	claimed := make([]domain.Application, 0)
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, a)
	}
	return claimed, rows.Err()
}

// afterConditionalUpdate различает причины пустого результата условного UPDATE: заявки нет
// (или она чужая), заявка уже не черновик, либо версия устарела.
func (r *ApplicationRepo) afterConditionalUpdate(ctx context.Context, row pgx.Row, id, inn string) (domain.Application, error) {
	a, err := scanApplication(row)
	if !errors.Is(err, domain.ErrNotFound) {
		return a, err
	}
	return r.conflictReason(ctx, id, inn)
}

// conflictReason объясняет, почему условный UPDATE не изменил строку (см. afterConditionalUpdate).
func (r *ApplicationRepo) conflictReason(ctx context.Context, id, inn string) (domain.Application, error) {
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
		&form, &documents, &history, &decision, &a.SubmittedAt, &a.CreatedAt, &a.UpdatedAt, &a.OfferID)
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
