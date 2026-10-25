package repository

import (
	"context"
	"errors"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

// Ручная проверка заявок сотрудником (см. domain/review.go).

// ListReview возвращает заявки на ручной проверке, старые первыми: общий пул (assignedTo пустой —
// заявки, которые никто не взял) или заявки, взятые сотрудником assignedTo.
func (r *ApplicationRepo) ListReview(ctx context.Context, assignedTo string) ([]domain.ReviewSummary, error) {
	rows, err := r.db.Query(ctx, `
		SELECT a.id::text, a.company_inn, c.name, a.product_id, a.amount, a.term_months, a.submitted_at,
			a.assigned_to::text, u.full_name, a.assigned_at
		FROM applications a
		JOIN companies c ON c.inn = a.company_inn
		LEFT JOIN users u ON u.id = a.assigned_to
		WHERE a.status = 'MANUAL_REVIEW'
		  AND CASE WHEN $1 = '' THEN a.assigned_to IS NULL ELSE a.assigned_to = $1::uuid END
		ORDER BY a.submitted_at`, assignedTo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.ReviewSummary, 0)
	for rows.Next() {
		var s domain.ReviewSummary
		if err := rows.Scan(&s.ID, &s.CompanyINN, &s.CompanyName, &s.ProductID, &s.Amount, &s.TermMonths, &s.SubmittedAt,
			&s.AssignedTo, &s.AssignedName, &s.AssignedAt); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// GetByID возвращает заявку без проверки компании: для сотрудника банка.
func (r *ApplicationRepo) GetByID(ctx context.Context, id string) (domain.Application, error) {
	row := r.db.QueryRow(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id = $1::text::uuid`, id)
	return scanApplication(row)
}

// Take закрепляет заявку из общего пула за сотрудником. Условие assigned_to IS NULL в самом запросе
// гарантирует, что при одновременном нажатии заявку получит только один.
func (r *ApplicationRepo) Take(ctx context.Context, id, employeeID string, at time.Time) (domain.Application, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE applications SET assigned_to = $2::text::uuid, assigned_at = $3, version = version + 1, updated_at = now()
		WHERE id = $1::text::uuid AND status = 'MANUAL_REVIEW' AND assigned_to IS NULL
		RETURNING `+applicationColumns, id, employeeID, at)
	a, err := scanApplication(row)
	if !errors.Is(err, domain.ErrNotFound) {
		return a, err
	}
	return r.reviewConflict(ctx, id, employeeID)
}

// Release возвращает взятую сотрудником заявку в общий пул.
func (r *ApplicationRepo) Release(ctx context.Context, id, employeeID string) (domain.Application, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE applications SET assigned_to = NULL, assigned_at = NULL, version = version + 1, updated_at = now()
		WHERE id = $1::text::uuid AND status = 'MANUAL_REVIEW' AND assigned_to = $2::text::uuid
		RETURNING `+applicationColumns, id, employeeID)
	a, err := scanApplication(row)
	if !errors.Is(err, domain.ErrNotFound) {
		return a, err
	}
	return r.reviewConflict(ctx, id, employeeID)
}

// ReleaseStale возвращает в общий пул заявки, которые взяты в работу дольше olderThan назад
// и по которым так и нет решения. Возвращает, сколько заявок вернулось.
func (r *ApplicationRepo) ReleaseStale(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE applications SET assigned_to = NULL, assigned_at = NULL, version = version + 1, updated_at = now()
		WHERE status = 'MANUAL_REVIEW' AND assigned_to IS NOT NULL
		  AND assigned_at < now() - make_interval(secs => $1::double precision)`, olderThan.Seconds())
	return tag.RowsAffected(), err
}

// Review записывает решение сотрудника: переход из MANUAL_REVIEW в u.To, дописывает его в history,
// сливает u.Decision с решением скоринга. Применяется, только если заявка всё ещё на проверке
// и закреплена за этим сотрудником. При запросе документов заявка открепляется: после ответа клиента
// она попадёт в общий пул.
func (r *ApplicationRepo) Review(ctx context.Context, id, employeeID string, u domain.ReviewUpdate, at time.Time) (domain.Application, error) {
	var decision any
	if len(u.Decision) > 0 {
		decision = string(u.Decision)
	}
	release := u.To == domain.StatusInfoRequested
	row := r.db.QueryRow(ctx, `
		UPDATE applications SET
			status       = $3,
			decision     = CASE WHEN $4::jsonb IS NULL THEN decision ELSE COALESCE(decision, '{}'::jsonb) || $4::jsonb END,
			info_request = $5,
			assigned_to  = CASE WHEN $8 THEN NULL ELSE assigned_to END,
			assigned_at  = CASE WHEN $8 THEN NULL ELSE assigned_at END,
			version      = version + 1,
			updated_at   = now(),
			history      = history || jsonb_build_array(
				jsonb_build_object('from', 'MANUAL_REVIEW', 'to', $3::text, 'at', $6::timestamptz, 'by', $2::text)
				|| CASE WHEN $7::text = '' THEN '{}'::jsonb ELSE jsonb_build_object('comment', $7::text) END)
		WHERE id = $1::text::uuid AND status = 'MANUAL_REVIEW' AND assigned_to = $2::text::uuid
		RETURNING `+applicationColumns,
		id, employeeID, string(u.To), decision, u.InfoRequest, at, u.Comment, release)
	a, err := scanApplication(row)
	if !errors.Is(err, domain.ErrNotFound) {
		return a, err
	}
	return r.reviewConflict(ctx, id, employeeID)
}

// Resume — клиент дополнил заявку в INFO_REQUESTED: она возвращается в статус to (MANUAL_REVIEW —
// в общий пул, DATA_CHECK — к воркеру), запрос очищается.
func (r *ApplicationRepo) Resume(ctx context.Context, id, inn string, to domain.Status, by string, at time.Time) (domain.Application, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE applications SET
			status = $3, info_request = NULL, assigned_to = NULL, assigned_at = NULL,
			enqueued_at = CASE WHEN $3 = 'DATA_CHECK' THEN NULL ELSE enqueued_at END,
			version = version + 1, updated_at = now(),
			history = history || jsonb_build_array(jsonb_build_object(
				'from', 'INFO_REQUESTED', 'to', $3::text, 'at', $4::timestamptz, 'by', $5::text,
				'comment', 'Клиент дополнил заявку'))
		WHERE id = $1::text::uuid AND company_inn = $2 AND status = 'INFO_REQUESTED'
		RETURNING `+applicationColumns,
		id, inn, string(to), at, by)
	a, err := scanApplication(row)
	if !errors.Is(err, domain.ErrNotFound) {
		return a, err
	}
	if _, err := r.GetForCompany(ctx, id, inn); err != nil {
		return domain.Application{}, err
	}
	return domain.Application{}, domain.ErrNotInfoRequested
}

// reviewConflict объясняет, почему действие сотрудника не применилось: заявки нет, она не на проверке,
// её взял другой сотрудник (с его ФИО) или она не закреплена за этим сотрудником.
func (r *ApplicationRepo) reviewConflict(ctx context.Context, id, employeeID string) (domain.Application, error) {
	var status string
	var assignedTo, assignedName *string
	err := r.db.QueryRow(ctx, `
		SELECT a.status, a.assigned_to::text, u.full_name
		FROM applications a LEFT JOIN users u ON u.id = a.assigned_to
		WHERE a.id = $1::text::uuid`, id).Scan(&status, &assignedTo, &assignedName)
	if err != nil {
		return domain.Application{}, mapNotFound(err)
	}
	switch {
	case domain.Status(status) != domain.StatusManualReview:
		return domain.Application{}, domain.ErrNotInReview
	case assignedTo != nil && *assignedTo == employeeID:
		// Заявка уже у этого сотрудника (например, повторное «взять в работу»).
		return r.GetByID(ctx, id)
	case assignedTo != nil:
		name := ""
		if assignedName != nil {
			name = *assignedName
		}
		return domain.Application{}, &domain.AssignedError{Name: name}
	default:
		return domain.Application{}, domain.ErrNotAssignedToYou
	}
}
