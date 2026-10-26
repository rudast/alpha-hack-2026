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
	form, history, decision, submitted_at, created_at, updated_at, offer_id::text,
	rate::float8, contract_number, issued_at, paid_payments, late_payments, closed_at,
	info_request, assigned_to::text, assigned_at`

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

// ListLoans возвращает выданные кредиты компании: заявки в статусах ACTIVE, OVERDUE и CLOSED.
func (r *ApplicationRepo) ListLoans(ctx context.Context, inn string) ([]domain.Application, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+applicationColumns+` FROM applications
		WHERE company_inn = $1 AND status IN ('ACTIVE', 'OVERDUE', 'CLOSED')
		ORDER BY issued_at DESC NULLS LAST`, inn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	loans := make([]domain.Application, 0)
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		loans = append(loans, a)
	}
	return loans, rows.Err()
}

// GetForCompany возвращает заявку, только если она принадлежит компании. Владелец проверяется
// в самом запросе, поэтому чужая заявка неотличима от несуществующей (ErrNotFound).
func (r *ApplicationRepo) GetForCompany(ctx context.Context, id, inn string) (domain.Application, error) {
	row := r.db.QueryRow(ctx,
		`SELECT `+applicationColumns+` FROM applications WHERE id = $1::text::uuid AND company_inn = $2`, id, inn)
	return scanApplication(row)
}

// ListByCompany возвращает заявки компании до выдачи кредита. Выданные кредиты (ACTIVE, OVERDUE, CLOSED)
// показываются отдельно, в GET /loans (ListLoans).
func (r *ApplicationRepo) ListByCompany(ctx context.Context, inn string) ([]domain.ApplicationSummary, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id::text, product_id, amount, term_months, status, submitted_at, created_at, decision
		FROM applications WHERE company_inn = $1 AND status NOT IN ('ACTIVE', 'OVERDUE', 'CLOSED')
		ORDER BY created_at DESC`, inn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.ApplicationSummary, 0)
	for rows.Next() {
		var s domain.ApplicationSummary
		var status string
		var decision []byte
		if err := rows.Scan(&s.ID, &s.ProductID, &s.Amount, &s.TermMonths, &status, &s.SubmittedAt, &s.CreatedAt, &decision); err != nil {
			return nil, err
		}
		s.Status = domain.Status(status)
		s.Decision = decision
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

// Accept выдаёт кредит по решению банка, которое принял клиент: из APPROVED или COUNTER_OFFER (from) заявка
// проходит SIGNING, SIGNED, DISBURSED и становится ACTIVE с условиями terms. Подписание и выдача имитируются,
// все переходы дописываются в history. Условие status = from защищает от двойного принятия.
// Заявка в другом статусе: ErrNotAwaitingDecision; чужая или несуществующая: ErrNotFound.
func (r *ApplicationRepo) Accept(ctx context.Context, id, inn string, from domain.Status, t domain.LoanTerms, by string, at time.Time) (domain.Application, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE applications SET
			status = 'ACTIVE', product_id = $11, amount = $4, term_months = $5, rate = $6, contract_number = $7, issued_at = $8,
			paid_payments = 0, late_payments = 0, version = version + 1, updated_at = now(),
			history = history || jsonb_build_array(
				jsonb_build_object('from', $3::text, 'to', 'SIGNING', 'at', $9::timestamptz, 'by', $10::text),
				jsonb_build_object('from', 'SIGNING', 'to', 'SIGNED', 'at', $9::timestamptz, 'by', $10::text),
				jsonb_build_object('from', 'SIGNED', 'to', 'DISBURSED', 'at', $9::timestamptz, 'by', 'system'),
				jsonb_build_object('from', 'DISBURSED', 'to', 'ACTIVE', 'at', $9::timestamptz, 'by', 'system'))
		WHERE id = $1::text::uuid AND company_inn = $2 AND status = $3
		RETURNING `+applicationColumns,
		id, inn, string(from), t.Amount, t.TermMonths, t.Rate, t.ContractNumber, t.IssuedAt, at, by, t.ProductID)
	return r.afterDecisionUpdate(ctx, row, id, inn)
}

// Decline возвращает заявку со встречным предложением в черновик: клиент может изменить условия
// и подать заново. Связь с предварительным предложением снимается (оно уже использовано при отправке).
func (r *ApplicationRepo) Decline(ctx context.Context, id, inn, by string, at time.Time) (domain.Application, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE applications SET
			status = 'DRAFT', offer_id = NULL, enqueued_at = NULL, version = version + 1, updated_at = now(),
			history = history || jsonb_build_array(jsonb_build_object(
				'from', 'COUNTER_OFFER', 'to', 'DRAFT', 'at', $3::timestamptz, 'by', $4::text,
				'comment', 'Клиент отказался от встречного предложения'))
		WHERE id = $1::text::uuid AND company_inn = $2 AND status = 'COUNTER_OFFER'
		RETURNING `+applicationColumns,
		id, inn, at, by)
	return r.afterDecisionUpdate(ctx, row, id, inn)
}

// afterDecisionUpdate различает причины пустого результата Accept и Decline: заявки нет (или она чужая)
// либо она не ждёт ответа клиента.
func (r *ApplicationRepo) afterDecisionUpdate(ctx context.Context, row pgx.Row, id, inn string) (domain.Application, error) {
	a, err := scanApplication(row)
	if !errors.Is(err, domain.ErrNotFound) {
		return a, err
	}
	if _, err := r.GetForCompany(ctx, id, inn); err != nil {
		return domain.Application{}, err
	}
	return domain.Application{}, domain.ErrNotAwaitingDecision
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
			-- Запрос клиенту: текст воркера, а если его нет, комментарий к переходу.
			info_request = CASE WHEN $3::text = 'INFO_REQUESTED' THEN COALESCE(NULLIF($8::text, ''), NULLIF($7::text, '')) ELSE info_request END,
			history    = history || jsonb_build_array(
				jsonb_build_object('from', $2::text, 'to', $3::text, 'at', $5::timestamptz, 'by', $6::text)
				|| CASE WHEN $7::text = '' THEN '{}'::jsonb ELSE jsonb_build_object('comment', $7::text) END)
		WHERE id = $1::text::uuid AND status = $2`,
		res.ApplicationID, string(res.From), string(res.To), decision, at, "worker", res.Comment, res.InfoRequest)
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
	var form, history, decision []byte
	err := row.Scan(&a.ID, &a.Version, &a.CompanyINN, &a.ProductID, &a.Amount, &a.TermMonths, &a.Purpose, &status,
		&form, &history, &decision, &a.SubmittedAt, &a.CreatedAt, &a.UpdatedAt, &a.OfferID,
		&a.Rate, &a.ContractNumber, &a.IssuedAt, &a.PaidPayments, &a.LatePayments, &a.ClosedAt,
		&a.InfoRequest, &a.AssignedTo, &a.AssignedAt)
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
