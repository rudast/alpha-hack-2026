package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/queue"
)

type Application struct {
	ID         string          `json:"id"`
	CompanyINN string          `json:"company_inn"`
	ProductID  string          `json:"product_id"`
	Amount     int64           `json:"amount"`
	TermMonths int             `json:"term_months"`
	Purpose    string          `json:"purpose"`
	Status     string          `json:"status"`
	Form       json.RawMessage `json:"form"`
	Documents  []Document      `json:"documents"`
	Jobs       []Job           `json:"jobs"`
}

type Document struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Filename  string          `json:"filename"`
	MediaType string          `json:"media_type"`
	Status    string          `json:"status"`
	Extracted json.RawMessage `json:"extracted"`
	Error     *string         `json:"error,omitempty"`
}

type Job struct {
	ID     string          `json:"id"`
	Kind   string          `json:"kind"`
	Status string          `json:"status"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *string         `json:"error,omitempty"`
}

type QueuedJob struct {
	ID      string
	Kind    string
	Payload json.RawMessage
}

type DocumentContent struct {
	ID        string
	Kind      string
	Filename  string
	MediaType string
	Content   []byte
}

type ApplicationRepo struct{ db *pgxpool.Pool }

func NewApplicationRepo(db *pgxpool.Pool) *ApplicationRepo { return &ApplicationRepo{db: db} }

func (r *ApplicationRepo) Create(ctx context.Context, inn, userID, productID string, amount int64, months int, purpose string, form json.RawMessage) (Application, error) {
	var id string
	err := r.db.QueryRow(ctx, `INSERT INTO applications(company_inn, created_by, product_id, amount, term_months, purpose, form)
		VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text`, inn, userID, productID, amount, months, purpose, form).Scan(&id)
	if err != nil {
		return Application{}, err
	}
	return r.Get(ctx, id, inn)
}

func (r *ApplicationRepo) Get(ctx context.Context, id, inn string) (Application, error) {
	var a Application
	err := r.db.QueryRow(ctx, `SELECT id::text, company_inn, product_id, amount, term_months, purpose, status, form
		FROM applications WHERE id::text=$1 AND company_inn=$2`, id, inn).
		Scan(&a.ID, &a.CompanyINN, &a.ProductID, &a.Amount, &a.TermMonths, &a.Purpose, &a.Status, &a.Form)
	if errors.Is(err, pgx.ErrNoRows) {
		return Application{}, domain.ErrNotFound
	}
	if err != nil {
		return Application{}, err
	}
	a.Documents, err = r.Documents(ctx, id)
	if err != nil {
		return Application{}, err
	}
	a.Jobs, err = r.Jobs(ctx, id)
	return a, err
}

func (r *ApplicationRepo) Documents(ctx context.Context, id string) ([]Document, error) {
	rows, err := r.db.Query(ctx, `SELECT id::text, kind, filename, media_type, status, extracted, error
		FROM application_documents WHERE application_id::text=$1 ORDER BY created_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.Kind, &d.Filename, &d.MediaType, &d.Status, &d.Extracted, &d.Error); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *ApplicationRepo) Jobs(ctx context.Context, id string) ([]Job, error) {
	rows, err := r.db.Query(ctx, `SELECT id::text, kind, status, result, error FROM worker_jobs
		WHERE application_id::text=$1 ORDER BY created_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.Kind, &j.Status, &j.Result, &j.Error); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (r *ApplicationRepo) AddDocument(ctx context.Context, id, inn, kind, filename, mediaType string, content []byte) (string, error) {
	var docID string
	err := r.db.QueryRow(ctx, `INSERT INTO application_documents(application_id,kind,filename,media_type,content)
		SELECT id,$3,$4,$5,$6 FROM applications WHERE id::text=$1 AND company_inn=$2 AND status='DRAFT'
		RETURNING id::text`, id, inn, kind, filename, mediaType, content).Scan(&docID)
	if errors.Is(err, pgx.ErrNoRows) {
		var status string
		lookupErr := r.db.QueryRow(ctx, `SELECT status FROM applications WHERE id::text=$1 AND company_inn=$2`, id, inn).Scan(&status)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return "", domain.ErrNotFound
		}
		if lookupErr != nil {
			return "", lookupErr
		}
		return "", domain.ErrConflict
	}
	return docID, err
}

func (r *ApplicationRepo) Company(ctx context.Context, inn string) (domain.Company, error) {
	return NewCompanyRepo(r.db).GetByINN(ctx, inn)
}

func (r *ApplicationRepo) Submit(ctx context.Context, id, inn string, jobs []QueuedJob) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM applications WHERE id::text=$1 AND company_inn=$2 FOR UPDATE`, id, inn).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "DRAFT" {
		return domain.ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE applications SET status='SUBMITTED', submitted_at=now(), updated_at=now() WHERE id::text=$1`, id)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		_, err = tx.Exec(ctx, `INSERT INTO worker_jobs(application_id,kind,payload) VALUES($1,$2,$3)`, id, j.Kind, j.Payload)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *ApplicationRepo) QueuedJobs(ctx context.Context) ([]QueuedJob, error) {
	rows, err := r.db.Query(ctx, `SELECT id::text, kind, payload FROM worker_jobs
		WHERE status='QUEUED' AND enqueued_at IS NULL ORDER BY created_at LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueuedJob{}
	for rows.Next() {
		var j QueuedJob
		if err := rows.Scan(&j.ID, &j.Kind, &j.Payload); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (r *ApplicationRepo) MarkEnqueued(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `UPDATE worker_jobs SET enqueued_at=$2, updated_at=$2 WHERE id::text=$1 AND enqueued_at IS NULL`, id, time.Now())
	return err
}

func (r *ApplicationRepo) DocumentsForJob(ctx context.Context, applicationID string) ([]DocumentContent, error) {
	rows, err := r.db.Query(ctx, `SELECT id::text, kind, filename, media_type, content FROM application_documents
		WHERE application_id::text=$1 ORDER BY created_at`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var documents []DocumentContent
	for rows.Next() {
		var d DocumentContent
		if err := rows.Scan(&d.ID, &d.Kind, &d.Filename, &d.MediaType, &d.Content); err != nil {
			return nil, err
		}
		documents = append(documents, d)
	}
	return documents, rows.Err()
}

// ApplyResult commits a job, its document fields and its status event together.
// The locked job row makes repeated delivery safe.
func (r *ApplicationRepo) ApplyResult(ctx context.Context, event queue.ApplicationEvent) error {
	if event.EventID == "" || event.EventID != event.JobID || event.ApplicationID == "" ||
		event.From != "SUBMITTED" || event.To != "DATA_CHECK" || event.Kind == "" {
		return errors.New("invalid application result event")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var applicationStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM applications WHERE id::text=$1 FOR UPDATE`, event.ApplicationID).Scan(&applicationStatus); err != nil {
		return err
	}
	var status, kind, applicationID string
	err = tx.QueryRow(ctx, `SELECT status, kind, application_id::text FROM worker_jobs WHERE id::text=$1 FOR UPDATE`, event.JobID).
		Scan(&status, &kind, &applicationID)
	if err != nil {
		return err
	}
	if applicationID != event.ApplicationID || kind != event.Kind {
		return errors.New("result event does not match job")
	}
	if status == "DONE" || status == "FAILED" {
		return tx.Commit(ctx)
	}
	if status != "QUEUED" && status != "RUNNING" {
		return errors.New("unexpected job status")
	}
	if event.Error == "" && !json.Valid(event.Result) {
		return errors.New("invalid job result JSON")
	}
	jobStatus := "DONE"
	if event.Error != "" {
		jobStatus = "FAILED"
	}
	_, err = tx.Exec(ctx, `UPDATE worker_jobs SET status=$2, result=$3, error=$4, updated_at=now() WHERE id::text=$1`,
		event.JobID, jobStatus, nullableJSON(event.Result), nullableString(event.Error))
	if err != nil {
		return err
	}
	if event.Kind == queue.KindParseDocuments && event.Error == "" {
		var parsed struct {
			Documents []struct {
				ID              string          `json:"id"`
				Status          string          `json:"status"`
				ExtractedFields json.RawMessage `json:"extracted_fields"`
				Error           string          `json:"error"`
			} `json:"documents"`
		}
		if err := json.Unmarshal(event.Result, &parsed); err != nil {
			return err
		}
		for _, d := range parsed.Documents {
			if d.ID == "" || !validDocumentStatus(d.Status) || !json.Valid(d.ExtractedFields) {
				return errors.New("invalid parsed document")
			}
			command, err := tx.Exec(ctx, `UPDATE application_documents SET status=$3, extracted=$4, error=$5
				WHERE id::text=$1 AND application_id::text=$2`, d.ID, event.ApplicationID, d.Status, d.ExtractedFields, nullableString(d.Error))
			if err != nil {
				return err
			}
			if command.RowsAffected() != 1 {
				return errors.New("parsed document does not belong to application")
			}
		}
	}
	// Every worker sees the submission snapshot. Only the first result moves the
	// application to DATA_CHECK; later results still update their own jobs.
	_, err = tx.Exec(ctx, `UPDATE applications SET status='DATA_CHECK', updated_at=now()
		WHERE id::text=$1 AND status=$2 AND $3='DATA_CHECK'`, event.ApplicationID, event.From, event.To)
	if err != nil {
		return err
	}
	historyEvent, err := json.Marshal(map[string]any{
		"event_id": event.EventID, "job_id": event.JobID, "kind": event.Kind,
		"from": event.From, "to": event.To, "comment": event.Comment,
		"decision": event.Decision, "info_request": event.InfoRequest,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE applications SET history=history || jsonb_build_array($2::jsonb), updated_at=now()
		WHERE id::text=$1`, event.ApplicationID, historyEvent)
	if err != nil {
		return err
	}
	var pending int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM worker_jobs WHERE application_id::text=$1 AND status NOT IN ('DONE','FAILED')`, event.ApplicationID).Scan(&pending)
	if err != nil {
		return err
	}
	if pending == 0 {
		var failed int
		err = tx.QueryRow(ctx, `SELECT count(*) FROM worker_jobs WHERE application_id::text=$1 AND status='FAILED'`, event.ApplicationID).Scan(&failed)
		if err != nil {
			return err
		}
		next := "SCORING"
		if failed > 0 {
			next = "MANUAL_REVIEW"
		} else {
			var verify, parsed, review json.RawMessage
			_ = tx.QueryRow(ctx, `SELECT result FROM worker_jobs WHERE application_id::text=$1 AND kind='verify_application'`, event.ApplicationID).Scan(&verify)
			_ = tx.QueryRow(ctx, `SELECT result FROM worker_jobs WHERE application_id::text=$1 AND kind='parse_documents'`, event.ApplicationID).Scan(&parsed)
			_ = tx.QueryRow(ctx, `SELECT result FROM worker_jobs WHERE application_id::text=$1 AND kind='critical_risk_review'`, event.ApplicationID).Scan(&review)
			var v struct {
				NextStatus   string `json:"next_status"`
				ManualReview bool   `json:"manual_review_required"`
			}
			var p struct {
				Status string `json:"status"`
			}
			var risk struct {
				RecommendedStatus string `json:"recommended_status"`
			}
			if err := json.Unmarshal(verify, &v); err != nil {
				return err
			}
			if err := json.Unmarshal(parsed, &p); err != nil {
				return err
			}
			if err := json.Unmarshal(review, &risk); err != nil {
				return err
			}
			if v.NextStatus == "INFO_REQUESTED" {
				next = "INFO_REQUESTED"
			} else if v.ManualReview || p.Status != "DONE" || risk.RecommendedStatus == "MANUAL_REVIEW" {
				next = "MANUAL_REVIEW"
			}
		}
		_, err = tx.Exec(ctx, `UPDATE applications SET status=$2, updated_at=now() WHERE id::text=$1 AND status='DATA_CHECK'`, event.ApplicationID, next)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func validDocumentStatus(value string) bool {
	switch value {
	case "PARSED", "TEXT_EXTRACTED", "UNSUPPORTED", "FAILED":
		return true
	}
	return false
}
