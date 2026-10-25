package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/catalog"
	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type ApplicationRepository interface {
	Create(ctx context.Context, a domain.Application, createdBy string) (domain.Application, error)
	CreateFromOffer(ctx context.Context, a domain.Application, createdBy string) (domain.Application, bool, error)
	GetForCompany(ctx context.Context, id, inn string) (domain.Application, error)
	ListByCompany(ctx context.Context, inn string) ([]domain.ApplicationSummary, error)
	UpdateDraft(ctx context.Context, id, inn string, version int, p domain.DraftPatch, unlinkOffer bool) (domain.Application, error)
	Submit(ctx context.Context, id, inn, by string, at time.Time) (domain.Application, error)
	// MarkEnqueued запоминает, что задача по заявке отправлена воркеру, чтобы сборщик не отправил её повторно сразу же.
	MarkEnqueued(ctx context.Context, id string) error
}

type CompanyGetter interface {
	GetByINN(ctx context.Context, inn string) (domain.Company, error)
}

type OfferGetter interface {
	GetForCompany(ctx context.Context, id, inn string) (domain.Offer, error)
}

// dispatchTimeout ограничивает отправку задачи воркеру при отправке заявки.
const dispatchTimeout = 5 * time.Second

// ApplicationService — жизненный цикл заявки со стороны клиента: черновик, просмотр, отправка.
// ИНН компании всегда берётся из сессии (domain.Principal), а не из запроса клиента.
type ApplicationService struct {
	apps      ApplicationRepository
	companies CompanyGetter
	offers    OfferGetter
	catalog   *catalog.Catalog
	publisher ApplicationPublisher
	now       func() time.Time
}

func NewApplicationService(apps ApplicationRepository, companies CompanyGetter, offers OfferGetter, cat *catalog.Catalog, publisher ApplicationPublisher) *ApplicationService {
	return &ApplicationService{apps: apps, companies: companies, offers: offers, catalog: cat, publisher: publisher, now: time.Now}
}

// CreateFromOffer создаёт черновик по предварительному предложению: продукт, сумму и срок берёт
// из сохранённого предложения, а не из запроса (in используется только для цели и формы).
// Если черновик по этому предложению уже есть, возвращает его и false.
func (s *ApplicationService) CreateFromOffer(ctx context.Context, p domain.Principal, offerID string, in domain.DraftPatch) (domain.Application, bool, error) {
	o, err := s.offers.GetForCompany(ctx, offerID, p.CompanyINN)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Application{}, false, domain.ErrOfferNotFound
	}
	if err != nil {
		return domain.Application{}, false, err
	}
	switch {
	case o.Status == domain.OfferUsed || o.Status == domain.OfferRevoked:
		return domain.Application{}, false, domain.ErrOfferUsed
	case !o.Usable(s.now()):
		return domain.Application{}, false, domain.ErrOfferExpired
	}

	a := domain.Application{
		CompanyINN: p.CompanyINN,
		ProductID:  o.ProductID,
		Amount:     o.MaxAmount,
		TermMonths: o.TermMonths,
		Form:       in.Form,
		OfferID:    &o.ID,
	}
	if in.Purpose != nil {
		a.Purpose = *in.Purpose
	}
	return s.apps.CreateFromOffer(ctx, a, p.UserID)
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
// У заявки по предложению сумма не может превышать его лимит, а смена продукта отвязывает предложение.
func (s *ApplicationService) Update(ctx context.Context, p domain.Principal, id string, version int, in domain.DraftPatch) (domain.Application, error) {
	if err := s.validatePatch(in); err != nil {
		return domain.Application{}, err
	}
	unlink, err := s.checkOfferPatch(ctx, p, id, in)
	if err != nil {
		return domain.Application{}, err
	}
	return s.apps.UpdateDraft(ctx, id, p.CompanyINN, version, in, unlink)
}

// checkOfferPatch проверяет изменение заявки, созданной по предложению. Возвращает true, если
// предложение нужно отвязать (клиент сменил продукт: заявка становится обычной).
func (s *ApplicationService) checkOfferPatch(ctx context.Context, p domain.Principal, id string, in domain.DraftPatch) (bool, error) {
	if in.ProductID == nil && in.Amount == nil {
		return false, nil
	}
	current, err := s.apps.GetForCompany(ctx, id, p.CompanyINN)
	if err != nil {
		return false, err
	}
	if current.OfferID == nil {
		return false, nil
	}
	if in.ProductID != nil && *in.ProductID != current.ProductID {
		return true, nil
	}
	if in.Amount == nil {
		return false, nil
	}
	o, err := s.offers.GetForCompany(ctx, *current.OfferID, p.CompanyINN)
	if err != nil {
		return false, err
	}
	if *in.Amount > o.MaxAmount {
		return false, domain.ErrOfferAmountExceeded
	}
	return false, nil
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

// Submit проверяет заявку, переводит её из черновика в SUBMITTED и сразу отправляет задачу воркеру скоринга.
// Незаполненные обязательные поля возвращаются как *domain.ValidationError.
// Сбой отправки задачи не отменяет отправку заявки: она уже сохранена, и её подберёт сборщик (ApplicationSweeper).
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
	if a.OfferID != nil {
		o, err := s.offers.GetForCompany(ctx, *a.OfferID, p.CompanyINN)
		if err != nil {
			return domain.Application{}, err
		}
		if !o.Usable(s.now()) {
			return domain.Application{}, domain.ErrOfferInvalid
		}
		if a.Amount > o.MaxAmount {
			return domain.Application{}, domain.ErrOfferAmountExceeded
		}
	}

	// Предложение становится USED в той же транзакции, что и отправка (см. ApplicationRepo.Submit).
	submitted, err := s.apps.Submit(ctx, id, p.CompanyINN, p.UserID, s.now())
	if err != nil {
		return domain.Application{}, err
	}
	s.dispatch(ctx, submitted, company)
	return submitted, nil
}

// dispatch отправляет задачу воркеру и отмечает заявку как отправленную. Ошибки только пишутся в лог:
// клиенту заявка уже принята, а если задача не ушла или метка не поставилась, сборщик отправит её
// (повторная отправка безопасна, воркер идемпотентен по application_id). Контекст отвязан от запроса,
// чтобы обрыв связи с клиентом не прервал отправку.
func (s *ApplicationService) dispatch(ctx context.Context, a domain.Application, company domain.Company) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dispatchTimeout)
	defer cancel()

	if err := s.publisher.Publish(ctx, newApplicationTask(a, company)); err != nil {
		log.Printf("applications: %s: publish on submit: %v (the sweeper will retry)", a.ID, err)
		return
	}
	if err := s.apps.MarkEnqueued(ctx, a.ID); err != nil {
		log.Printf("applications: %s: mark enqueued: %v (the sweeper may send it again)", a.ID, err)
	}
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
