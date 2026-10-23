package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/catalog"
	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/queue"
	"github.com/rudast/alpha-hack-2026/internal/repository"
)

type ApplicationInput struct {
	ProductID  string         `json:"product_id"`
	Amount     int64          `json:"amount"`
	TermMonths int            `json:"term_months"`
	Purpose    string         `json:"purpose"`
	Form       map[string]any `json:"form"`
}

type ApplicationService struct {
	repo    *repository.ApplicationRepo
	catalog *catalog.Catalog
	queue   *queue.Client
}

func NewApplicationService(repo *repository.ApplicationRepo, products *catalog.Catalog, q *queue.Client) *ApplicationService {
	return &ApplicationService{repo: repo, catalog: products, queue: q}
}

func (s *ApplicationService) Create(ctx context.Context, p domain.Principal, in ApplicationInput) (repository.Application, error) {
	product, ok := s.catalog.Product(in.ProductID)
	if !ok || in.Amount <= 0 || in.TermMonths <= 0 || strings.TrimSpace(in.Purpose) == "" {
		return repository.Application{}, fmt.Errorf("%w: product, amount, term and purpose are required", domain.ErrInvalidApplication)
	}
	company, err := s.repo.Company(ctx, p.CompanyINN)
	if err != nil {
		return repository.Application{}, err
	}
	if !s.catalog.Available(product, company.OrgType, in.Amount, in.TermMonths) {
		return repository.Application{}, fmt.Errorf("%w: product is unavailable for these parameters", domain.ErrInvalidApplication)
	}
	if in.Form == nil {
		in.Form = map[string]any{}
	}
	form, err := json.Marshal(in.Form)
	if err != nil {
		return repository.Application{}, err
	}
	return s.repo.Create(ctx, p.CompanyINN, p.UserID, in.ProductID, in.Amount, in.TermMonths, strings.TrimSpace(in.Purpose), form)
}

func (s *ApplicationService) Get(ctx context.Context, p domain.Principal, id string) (repository.Application, error) {
	return s.repo.Get(ctx, id, p.CompanyINN)
}

func (s *ApplicationService) AddDocument(ctx context.Context, p domain.Principal, id, kind, filename, mediaType string, content []byte) (repository.Application, error) {
	if s.catalog.DocumentTitle(kind) == "" || filename == "" || len(content) == 0 || len(content) > 10<<20 {
		return repository.Application{}, fmt.Errorf("%w: invalid document", domain.ErrInvalidApplication)
	}
	if mediaType != "application/json" && mediaType != "text/csv" && mediaType != "text/plain" &&
		mediaType != "application/pdf" && mediaType != "application/msword" &&
		mediaType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" &&
		mediaType != "image/jpeg" && mediaType != "image/png" && mediaType != "image/tiff" {
		return repository.Application{}, fmt.Errorf("%w: unsupported media type", domain.ErrInvalidApplication)
	}
	if _, err := s.repo.AddDocument(ctx, id, p.CompanyINN, kind, filename, mediaType, content); err != nil {
		return repository.Application{}, err
	}
	return s.Get(ctx, p, id)
}

func (s *ApplicationService) Submit(ctx context.Context, p domain.Principal, id string) (repository.Application, []string, error) {
	a, err := s.repo.Get(ctx, id, p.CompanyINN)
	if err != nil {
		return repository.Application{}, nil, err
	}
	if a.Status != "DRAFT" {
		return repository.Application{}, nil, domain.ErrConflict
	}
	product, ok := s.catalog.Product(a.ProductID)
	if !ok {
		return repository.Application{}, nil, fmt.Errorf("%w: unknown product", domain.ErrInvalidApplication)
	}
	var form map[string]any
	if err := json.Unmarshal(a.Form, &form); err != nil {
		return repository.Application{}, nil, err
	}
	missing := s.catalog.MissingFormFields(product, form)
	present := map[string]bool{}
	for _, d := range a.Documents {
		present[d.Kind] = true
	}
	for _, kind := range s.catalog.RequiredDocuments(product) {
		if !present[kind] {
			missing = append(missing, "documents."+kind)
		}
	}
	if len(missing) > 0 {
		return repository.Application{}, missing, domain.ErrInvalidApplication
	}
	company, err := s.repo.Company(ctx, p.CompanyINN)
	if err != nil {
		return repository.Application{}, nil, err
	}
	verification := queue.VerifyApplicationRequest{
		ApplicationID: id,
		Application: queue.ApplicationSnapshot{
			INN: p.CompanyINN, OrgType: company.OrgType, ProductID: a.ProductID,
			Amount: a.Amount, TermMonths: a.TermMonths, Purpose: a.Purpose, BKIConsent: present["bki_consent"],
		},
		Company: queue.CompanySnapshot{TotalDebt: company.TotalDebt, Equity: company.Equity},
	}
	refs := make([]queue.DocumentRef, 0, len(a.Documents))
	for _, d := range a.Documents {
		refs = append(refs, queue.DocumentRef{ID: d.ID, Kind: d.Kind, Filename: d.Filename})
	}
	requests := []struct {
		kind    string
		payload any
	}{
		{queue.KindVerifyApplication, verification},
		{queue.KindParseDocuments, queue.ParseDocumentsRequest{ApplicationID: id, Documents: refs}},
		{queue.KindCriticalRiskReview, queue.CriticalRiskRequest{ApplicationID: id, DebtToEquity: company.DebtToEquity}},
	}
	jobs := make([]repository.QueuedJob, 0, len(requests))
	for _, request := range requests {
		body, err := json.Marshal(request.payload)
		if err != nil {
			return repository.Application{}, nil, err
		}
		jobs = append(jobs, repository.QueuedJob{Kind: request.kind, Payload: body})
	}
	if err := s.repo.Submit(ctx, id, p.CompanyINN, jobs); err != nil {
		return repository.Application{}, nil, err
	}
	// Jobs are durable before publication; the background dispatcher retries Redis failures.
	if err := s.DispatchQueued(ctx); err != nil {
		log.Printf("dispatch application jobs: %v", err)
	}
	a, err = s.Get(ctx, p, id)
	return a, nil, err
}

func (s *ApplicationService) DispatchQueued(ctx context.Context) error {
	jobs, err := s.repo.QueuedJobs(ctx)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		payload := j.Payload
		if j.Kind == queue.KindParseDocuments {
			var request queue.ParseDocumentsRequest
			if err := json.Unmarshal(payload, &request); err != nil {
				return err
			}
			documents, err := s.repo.DocumentsForJob(ctx, request.ApplicationID)
			if err != nil {
				return err
			}
			if len(documents) != len(request.Documents) {
				return fmt.Errorf("document set changed for job %s", j.ID)
			}
			request.Documents = make([]queue.DocumentRef, 0, len(documents))
			for _, d := range documents {
				request.Documents = append(request.Documents, queue.DocumentRef{ID: d.ID, Kind: d.Kind,
					Filename: d.Filename, MediaType: d.MediaType, ContentBase64: base64.StdEncoding.EncodeToString(d.Content)})
			}
			payload, err = json.Marshal(request)
			if err != nil {
				return err
			}
		}
		if err := s.queue.PublishStored(ctx, j.ID, j.Kind, payload); err != nil {
			return err
		}
		if err := s.repo.MarkEnqueued(ctx, j.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *ApplicationService) RunResults(ctx context.Context) {
	if err := s.queue.InitResults(ctx); err != nil {
		log.Printf("init application results: %v", err)
		return
	}
	consumer := fmt.Sprintf("api-%d", os.Getpid())
	for ctx.Err() == nil {
		messageID, event, err := s.queue.ReadResult(ctx, consumer)
		if err != nil {
			log.Printf("read application result: %v", err)
			time.Sleep(time.Second)
			continue
		}
		if messageID == "" {
			continue
		}
		if err := s.repo.ApplyResult(ctx, event); err != nil {
			log.Printf("apply application result %s: %v", messageID, err)
			time.Sleep(time.Second)
			continue
		}
		if err := s.queue.AckResult(ctx, messageID); err != nil {
			log.Printf("ack application result %s: %v", messageID, err)
		}
	}
}

func (s *ApplicationService) RunDispatcher(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.DispatchQueued(ctx); err != nil {
				log.Printf("dispatch application jobs: %v", err)
			}
		}
	}
}
