package service

import (
	"context"
	"strings"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/catalog"
	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type ApplicationRepository interface {
	Create(ctx context.Context, a domain.Application, createdBy string) (domain.Application, error)
	GetForCompany(ctx context.Context, id, inn string) (domain.Application, error)
	ListByCompany(ctx context.Context, inn string) ([]domain.ApplicationSummary, error)
	UpdateDraft(ctx context.Context, id, inn string, version int, p domain.DraftPatch) (domain.Application, error)
	Submit(ctx context.Context, id, inn, by string, at time.Time) (domain.Application, error)
}

type CompanyGetter interface {
	GetByINN(ctx context.Context, inn string) (domain.Company, error)
}

// ApplicationService — жизненный цикл заявки со стороны клиента: черновик, просмотр, отправка.
// ИНН компании всегда берётся из сессии (domain.Principal), а не из запроса клиента.
type ApplicationService struct {
	apps      ApplicationRepository
	companies CompanyGetter
	catalog   *catalog.Catalog
	now       func() time.Time
}

func NewApplicationService(apps ApplicationRepository, companies CompanyGetter, cat *catalog.Catalog) *ApplicationService {
	return &ApplicationService{apps: apps, companies: companies, catalog: cat, now: time.Now}
}

// Create создаёт черновик из первого сохранения формы.
func (s *ApplicationService) Create(ctx context.Context, p domain.Principal, in domain.DraftPatch) (domain.Application, error) {
	if err := s.validatePatch(in); err != nil {
		return domain.Application{}, err
	}
	a := domain.Application{CompanyINN: p.CompanyINN, Form: in.Form}
	if in.ProductID != nil {
		a.ProductID = *in.ProductID
	}
	if in.Amount != nil {
		a.Amount = *in.Amount
	}
	if in.TermMonths != nil {
		a.TermMonths = *in.TermMonths
	}
	if in.Purpose != nil {
		a.Purpose = *in.Purpose
	}
	return s.apps.Create(ctx, a, p.UserID)
}

// Update сохраняет изменения черновика (автосохранение формы). version — версия заявки, которую
// клиент видел последней; если заявка с тех пор менялась, возвращается domain.ErrVersionConflict.
func (s *ApplicationService) Update(ctx context.Context, p domain.Principal, id string, version int, in domain.DraftPatch) (domain.Application, error) {
	if err := s.validatePatch(in); err != nil {
		return domain.Application{}, err
	}
	return s.apps.UpdateDraft(ctx, id, p.CompanyINN, version, in)
}

func (s *ApplicationService) Get(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	return s.apps.GetForCompany(ctx, id, p.CompanyINN)
}

// List возвращает заявки компании с названиями продуктов из каталога.
func (s *ApplicationService) List(ctx context.Context, p domain.Principal) ([]domain.ApplicationSummary, error) {
	list, err := s.apps.ListByCompany(ctx, p.CompanyINN)
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

// Submit проверяет заявку и переводит её из черновика в SUBMITTED.
// Незаполненные обязательные поля возвращаются как *domain.ValidationError.
func (s *ApplicationService) Submit(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	a, err := s.apps.GetForCompany(ctx, id, p.CompanyINN)
	if err != nil {
		return domain.Application{}, err
	}
	if a.Status != domain.StatusDraft {
		return domain.Application{}, domain.ErrConflict
	}

	product, known := s.catalog.Product(a.ProductID)
	var missing []string
	if !known {
		missing = append(missing, "product_id")
	}
	if a.Amount <= 0 {
		missing = append(missing, "amount")
	}
	if a.TermMonths <= 0 {
		missing = append(missing, "term_months")
	}
	if strings.TrimSpace(a.Purpose) == "" {
		missing = append(missing, "purpose")
	}
	if known {
		missing = append(missing, s.catalog.MissingFormFields(product, a.Form)...)
	}
	if len(missing) > 0 {
		return domain.Application{}, &domain.ValidationError{Fields: missing}
	}

	company, err := s.companies.GetByINN(ctx, p.CompanyINN)
	if err != nil {
		return domain.Application{}, err
	}
	if !s.catalog.Available(product, company.OrgType, a.Amount, a.TermMonths) {
		return domain.Application{}, domain.ErrProductUnavailable
	}

	return s.apps.Submit(ctx, id, p.CompanyINN, p.UserID, s.now())
}

// validatePatch проверяет только то, что можно проверить без полной заявки:
// продукт существует, числа не отрицательны.
func (s *ApplicationService) validatePatch(in domain.DraftPatch) error {
	if in.ProductID != nil && *in.ProductID != "" {
		if _, ok := s.catalog.Product(*in.ProductID); !ok {
			return domain.ErrUnknownProduct
		}
	}
	if in.Amount != nil && *in.Amount < 0 {
		return domain.ErrInvalidInput
	}
	if in.TermMonths != nil && *in.TermMonths < 0 {
		return domain.ErrInvalidInput
	}
	return nil
}
