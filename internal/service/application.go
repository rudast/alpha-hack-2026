package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
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
	Accept(ctx context.Context, id, inn string, from domain.Status, t domain.LoanTerms, by string, at time.Time) (domain.Application, error)
	Decline(ctx context.Context, id, inn, by string, at time.Time) (domain.Application, error)
	Resume(ctx context.Context, id, inn string, to domain.Status, by string, at time.Time) (domain.Application, error)
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
	documents DocumentStore
	now       func() time.Time
}

func NewApplicationService(apps ApplicationRepository, companies CompanyGetter, offers OfferGetter, cat *catalog.Catalog, publisher ApplicationPublisher, documents DocumentStore) *ApplicationService {
	return &ApplicationService{apps: apps, companies: companies, offers: offers, catalog: cat, publisher: publisher, documents: documents, now: time.Now}
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
	created, isNew, err := s.apps.CreateFromOffer(ctx, a, p.UserID)
	created, err = s.withDocuments(created, err)
	return created, isNew, err
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
	return s.withDocuments(s.apps.Create(ctx, a, p.UserID))
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
	return s.withDocuments(s.apps.UpdateDraft(ctx, id, p.CompanyINN, version, in, unlink))
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
	return s.withDocuments(s.apps.GetForCompany(ctx, id, p.CompanyINN))
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
	// Согласие на запрос в БКИ обязательно для любой заявки; фронт кладёт его в form.consents.
	if consents, _ := a.Form["consents"].(map[string]any); consents["bki_request"] != true {
		missing = append(missing, "consents.bki_request")
	}
	if len(missing) > 0 {
		return domain.Application{}, &domain.ValidationError{Fields: missing}
	}

	company, err := s.companies.GetByINN(ctx, p.CompanyINN)
	if err != nil {
		return domain.Application{}, err
	}
	if !s.catalog.Available(product, company.OrgType, a.Amount, a.TermMonths) ||
		!s.catalog.AvailableForAge(product, businessAgeMonths(company.RegistrationDate, s.now()), a.Amount) {
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
	submitted, err = s.withDocuments(submitted, nil)
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

// Accept — клиент принимает решение банка (одобрение или встречное предложение): кредит сразу выдаётся
// и становится ACTIVE. Подписание и выдача имитируются. Условия берутся из решения: у встречного
// предложения сумма, срок и ставка из decision.offer; ставка — decision.offer.rate, иначе ставка решения,
// иначе базовая ставка продукта.
func (s *ApplicationService) Accept(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	a, err := s.apps.GetForCompany(ctx, id, p.CompanyINN)
	if err != nil {
		return domain.Application{}, err
	}
	if a.Status != domain.StatusApproved && a.Status != domain.StatusCounterOffer {
		return domain.Application{}, domain.ErrNotAwaitingDecision
	}
	now := s.now()
	terms := s.loanTerms(a, now)
	return s.withDocuments(s.apps.Accept(ctx, id, p.CompanyINN, a.Status, terms, p.UserID, now))
}

// Decline — клиент отказывается от встречного предложения: заявка возвращается в черновик,
// условия можно изменить и подать заново.
func (s *ApplicationService) Decline(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	return s.withDocuments(s.apps.Decline(ctx, id, p.CompanyINN, p.UserID, s.now()))
}

// Resume — клиент дополнил заявку, по которой банк запросил данные (INFO_REQUESTED): загрузил документы
// и нажал «Отправить». Заявка возвращается туда, откуда её запросили: от сотрудника — в общий пул ручной
// проверки, от воркера — на проверку данных (задача воркеру отправляется заново).
func (s *ApplicationService) Resume(ctx context.Context, p domain.Principal, id string) (domain.Application, error) {
	a, err := s.apps.GetForCompany(ctx, id, p.CompanyINN)
	if err != nil {
		return domain.Application{}, err
	}
	if a.Status != domain.StatusInfoRequested {
		return domain.Application{}, domain.ErrNotInfoRequested
	}
	to := domain.StatusDataCheck
	for i := len(a.History) - 1; i >= 0; i-- {
		if a.History[i].To == domain.StatusInfoRequested {
			if a.History[i].From == domain.StatusManualReview {
				to = domain.StatusManualReview
			}
			break
		}
	}

	resumed, err := s.withDocuments(s.apps.Resume(ctx, id, p.CompanyINN, to, p.UserID, s.now()))
	if err != nil {
		return domain.Application{}, err
	}
	if to == domain.StatusDataCheck {
		company, err := s.companies.GetByINN(ctx, p.CompanyINN)
		if err != nil {
			return domain.Application{}, err
		}
		s.dispatch(ctx, resumed, company)
	}
	return resumed, nil
}

// decisionTerms — поля решения скоринга (docs/scoring.md, раздел 11), нужные для выдачи кредита.
type decisionTerms struct {
	Rate  *float64 `json:"rate"`
	Offer *struct {
		ProductID  string   `json:"product_id"`
		Amount     *int64   `json:"amount"`
		TermMonths *int     `json:"term_months"`
		Rate       *float64 `json:"rate"`
	} `json:"offer"`
}

func (s *ApplicationService) loanTerms(a domain.Application, now time.Time) domain.LoanTerms {
	t := domain.LoanTerms{
		ProductID:      a.ProductID,
		Amount:         a.Amount,
		TermMonths:     a.TermMonths,
		ContractNumber: contractNumber(a.ProductID, a.ID, now),
		IssuedAt:       dateOf(now),
	}
	var d decisionTerms
	_ = json.Unmarshal(a.Decision, &d)
	// Во встречном предложении сотрудник может предложить другой продукт.
	if a.Status == domain.StatusCounterOffer && d.Offer != nil && d.Offer.ProductID != "" {
		t.ProductID = d.Offer.ProductID
	}
	if p, ok := s.catalog.Product(t.ProductID); ok {
		t.Rate = p.BaseRate
	}
	if d.Rate != nil {
		t.Rate = *d.Rate
	}
	if d.Offer != nil {
		if d.Offer.Rate != nil {
			t.Rate = *d.Offer.Rate
		}
		if a.Status == domain.StatusCounterOffer && d.Offer.Amount != nil {
			t.Amount = *d.Offer.Amount
		}
		if a.Status == domain.StatusCounterOffer && d.Offer.TermMonths != nil {
			t.TermMonths = *d.Offer.TermMonths
		}
	}
	return t
}

// contractPrefix — буквенный код вида кредита в номере договора, как в демо-данных кабинета.
var contractPrefix = map[string]string{
	"working_capital": "ОС", "ooo_loan": "КО", "ip_loan": "КИ", "express": "ЭК", "loan_up_to_1m": "КМ",
	"overdraft": "ОВ", "credit_line": "ВКЛ", "marketplace_seller": "МП", "equipment_loan": "ОБ", "auto_loan": "АК",
	"secured_property_equipment": "ЗИ", "business_purchase": "ГБ", "mortgage_ip": "ИИ", "mortgage_business": "ИБ",
}

// contractNumber формирует номер договора вида «0ОС-2026/1187»: вид кредита, год выдачи и четыре цифры,
// выведенные из идентификатора заявки (для одной заявки номер всегда один и тот же).
func contractNumber(productID, applicationID string, at time.Time) string {
	prefix, ok := contractPrefix[productID]
	if !ok {
		prefix = "КР"
	}
	return fmt.Sprintf("0%s-%d/%04d", prefix, at.Year(), crc32.ChecksumIEEE([]byte(applicationID))%10000)
}
