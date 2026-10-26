package service

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/catalog"
	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type ReviewRepository interface {
	ListReview(ctx context.Context, assignedTo string) ([]domain.ReviewSummary, error)
	GetByID(ctx context.Context, id string) (domain.Application, error)
	Take(ctx context.Context, id, employeeID string, at time.Time) (domain.Application, error)
	Release(ctx context.Context, id, employeeID string) (domain.Application, error)
	ReleaseStale(ctx context.Context, olderThan time.Duration) (int64, error)
	Review(ctx context.Context, id, employeeID string, u domain.ReviewUpdate, at time.Time) (domain.Application, error)
}

// DocumentReader — чтение документов заявки из тома (repository.DocumentStore).
type DocumentReader interface {
	List(applicationID string) ([]domain.Document, error)
	Open(applicationID, documentID string) (io.ReadCloser, domain.Document, error)
}

// ReviewCard — всё, что сотрудник видит в карточке заявки.
type ReviewCard struct {
	Application  domain.Application
	ProductName  string
	Company      domain.Company
	AssignedName string // ФИО сотрудника, взявшего заявку в работу; пусто, если она в пуле
}

// ReviewService — рабочее место сотрудника: пул заявок на ручной проверке, взятие в работу, возврат
// в пул и решение (согласовать, отказать, запросить документы, встречное предложение).
type ReviewService struct {
	apps      ReviewRepository
	users     UserGetter
	companies CompanyGetter
	documents DocumentReader
	catalog   *catalog.Catalog
	now       func() time.Time
}

func NewReviewService(apps ReviewRepository, users UserGetter, companies CompanyGetter, documents DocumentReader, cat *catalog.Catalog) *ReviewService {
	return &ReviewService{apps: apps, users: users, companies: companies, documents: documents, catalog: cat, now: time.Now}
}

// Pool возвращает общий пул (mine = false) или заявки, взятые сотрудником (mine = true).
func (s *ReviewService) Pool(ctx context.Context, p domain.Principal, mine bool) ([]domain.ReviewSummary, error) {
	assignedTo := ""
	if mine {
		assignedTo = p.UserID
	}
	list, err := s.apps.ListReview(ctx, assignedTo)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if product, ok := s.catalog.Product(list[i].ProductID); ok {
			list[i].ProductName = product.Name
		}
	}
	return list, nil
}

// Employee возвращает текущего сотрудника: ФИО и должность для шапки рабочего места.
func (s *ReviewService) Employee(ctx context.Context, p domain.Principal) (domain.User, error) {
	return s.users.GetByID(ctx, p.UserID)
}

// Card возвращает заявку с данными компании, документами и ФИО взявшего её сотрудника.
func (s *ReviewService) Card(ctx context.Context, id string) (ReviewCard, error) {
	a, err := s.apps.GetByID(ctx, id)
	if err != nil {
		return ReviewCard{}, err
	}
	return s.card(ctx, a)
}

func (s *ReviewService) card(ctx context.Context, a domain.Application) (ReviewCard, error) {
	docs, err := s.documents.List(a.ID)
	if err != nil {
		return ReviewCard{}, err
	}
	a.Documents = docs
	company, err := s.companies.GetByINN(ctx, a.CompanyINN)
	if err != nil {
		return ReviewCard{}, err
	}
	c := ReviewCard{Application: a, Company: company}
	if p, ok := s.catalog.Product(a.ProductID); ok {
		c.ProductName = p.Name
	}
	if a.AssignedTo != nil {
		if u, err := s.users.GetByID(ctx, *a.AssignedTo); err == nil {
			c.AssignedName = u.FullName
		}
	}
	return c, nil
}

// Take берёт заявку из общего пула в работу. Если её уже взял другой сотрудник, ошибка
// *domain.AssignedError содержит его ФИО.
func (s *ReviewService) Take(ctx context.Context, p domain.Principal, id string) (ReviewCard, error) {
	a, err := s.apps.Take(ctx, id, p.UserID, s.now())
	if err != nil {
		return ReviewCard{}, err
	}
	return s.card(ctx, a)
}

// Release возвращает взятую заявку в общий пул.
func (s *ReviewService) Release(ctx context.Context, p domain.Principal, id string) (ReviewCard, error) {
	a, err := s.apps.Release(ctx, id, p.UserID)
	if err != nil {
		return ReviewCard{}, err
	}
	return s.card(ctx, a)
}

// Decide записывает решение сотрудника по заявке, которую он взял в работу.
func (s *ReviewService) Decide(ctx context.Context, p domain.Principal, id string, d domain.ReviewDecision) (ReviewCard, error) {
	a, err := s.apps.GetByID(ctx, id)
	if err != nil {
		return ReviewCard{}, err
	}
	u, err := s.reviewUpdate(ctx, p, a, d)
	if err != nil {
		return ReviewCard{}, err
	}
	updated, err := s.apps.Review(ctx, id, p.UserID, u, s.now())
	if err != nil {
		return ReviewCard{}, err
	}
	return s.card(ctx, updated)
}

// reviewUpdate проверяет решение и собирает, что записать в заявку. Решение сотрудника дописывается
// к решению скоринга: outcome, кто и когда решил, причина или условия встречного предложения.
func (s *ReviewService) reviewUpdate(ctx context.Context, p domain.Principal, a domain.Application, d domain.ReviewDecision) (domain.ReviewUpdate, error) {
	review := map[string]any{"by": p.UserID, "at": s.now().UTC()}
	patch := map[string]any{"review": review}
	var u domain.ReviewUpdate

	switch d.Action {
	case domain.ReviewApprove:
		u.To = domain.StatusApproved
		u.Comment = strings.TrimSpace(d.Reason)
	case domain.ReviewReject:
		reason := strings.TrimSpace(d.Reason)
		if reason == "" {
			return u, &domain.ValidationError{Fields: []string{"reason"}}
		}
		u.To, u.Comment = domain.StatusRejected, reason
		patch["reason"] = reason
	case domain.ReviewRequestInfo:
		text := strings.TrimSpace(d.InfoRequest)
		if text == "" {
			return u, &domain.ValidationError{Fields: []string{"info_request"}}
		}
		u.To, u.Comment, u.InfoRequest = domain.StatusInfoRequested, text, &text
	case domain.ReviewCounterOffer:
		offer, err := s.counterOffer(ctx, a, d)
		if err != nil {
			return u, err
		}
		u.To = domain.StatusCounterOffer
		u.Comment = strings.TrimSpace(d.Reason)
		patch["offer"] = offer
	default:
		return u, domain.ErrInvalidInput
	}

	review["outcome"] = string(u.To)
	patch["outcome"] = string(u.To)
	b, err := json.Marshal(patch)
	if err != nil {
		return u, err
	}
	u.Decision = b
	return u, nil
}

// counterOffer проверяет условия встречного предложения. Продукт по умолчанию тот же, что в заявке;
// другой можно выбрать, только если для него у заявки уже хватает данных: заполнены поля формы,
// загружены обязательные документы, продукт доступен компании по типу организации и возрасту бизнеса,
// сумма и срок в его пределах.
func (s *ReviewService) counterOffer(ctx context.Context, a domain.Application, d domain.ReviewDecision) (map[string]any, error) {
	productID := d.ProductID
	if productID == "" {
		productID = a.ProductID
	}
	product, ok := s.catalog.Product(productID)
	if !ok {
		return nil, domain.ErrUnknownProduct
	}
	if d.Amount <= 0 || d.TermMonths <= 0 || d.Rate <= 0 || d.Rate >= 100 {
		return nil, domain.ErrInvalidInput
	}

	company, err := s.companies.GetByINN(ctx, a.CompanyINN)
	if err != nil {
		return nil, err
	}
	age := businessAgeMonths(company.RegistrationDate, s.now())
	young := age < s.catalog.OfferRules().YoungBusinessMonths
	switch {
	case !s.catalog.Available(product, company.OrgType, d.Amount, d.TermMonths),
		d.Amount < product.MinAmount,
		age < product.MinBusinessAgeMonths,
		young && product.YoungMaxAmount > 0 && d.Amount > product.YoungMaxAmount:
		return nil, domain.ErrProductUnavailable
	}

	missing := s.catalog.MissingFormFields(product, a.Form)
	docs, err := s.documents.List(a.ID)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, doc := range docs {
		have[doc.Kind] = true
	}
	for _, kind := range s.catalog.RequiredDocuments(product) {
		if !have[kind] {
			missing = append(missing, "documents."+kind)
		}
	}
	if len(missing) > 0 {
		return nil, &domain.ValidationError{Fields: missing}
	}

	return map[string]any{
		"product_id":  product.ID,
		"amount":      d.Amount,
		"term_months": d.TermMonths,
		"rate":        d.Rate,
	}, nil
}

// OpenDocument открывает файл заявки для скачивания сотрудником.
func (s *ReviewService) OpenDocument(ctx context.Context, id, documentID string) (io.ReadCloser, domain.Document, error) {
	if _, err := s.apps.GetByID(ctx, id); err != nil {
		return nil, domain.Document{}, err
	}
	return s.documents.Open(id, documentID)
}

// RunReleaser раз в interval возвращает в общий пул заявки, взятые в работу дольше
// domain.ReviewAssignTimeout назад без решения. Работает, пока не отменён ctx.
func (s *ReviewService) RunReleaser(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		n, err := s.apps.ReleaseStale(ctx, domain.ReviewAssignTimeout)
		switch {
		case err != nil:
			log.Printf("review: release stale: %v", err)
		case n > 0:
			log.Printf("review: returned %d stale applications to the pool", n)
		}
	}
}
