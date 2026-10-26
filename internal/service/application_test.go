package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/catalog"
	"github.com/rudast/alpha-hack-2026/internal/domain"
)

// fakeApps повторяет контракт настоящего репозитория: владелец проверяется по ИНН,
// не черновик даёт ErrConflict, чужая заявка ErrNotFound.
type fakeApps struct {
	items     map[string]domain.Application
	createdBy []string
	submits   int
	enqueued  []string // заявки, отмеченные как отправленные воркеру
	markErr   error
}

func (f *fakeApps) MarkEnqueued(_ context.Context, id string) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.enqueued = append(f.enqueued, id)
	return nil
}

func newFakeApps() *fakeApps { return &fakeApps{items: map[string]domain.Application{}} }

func (f *fakeApps) Create(_ context.Context, a domain.Application, by string) (domain.Application, error) {
	a.ID = fmt.Sprintf("app-%d", len(f.items)+1)
	a.Version = 1
	a.Status = domain.StatusDraft
	f.items[a.ID] = a
	f.createdBy = append(f.createdBy, by)
	return a, nil
}

func (f *fakeApps) CreateFromOffer(ctx context.Context, a domain.Application, by string) (domain.Application, bool, error) {
	for _, existing := range f.items {
		if existing.OfferID != nil && *existing.OfferID == *a.OfferID {
			return existing, false, nil
		}
	}
	created, err := f.Create(ctx, a, by)
	return created, true, err
}

func (f *fakeApps) GetForCompany(_ context.Context, id, inn string) (domain.Application, error) {
	a, ok := f.items[id]
	if !ok || a.CompanyINN != inn {
		return domain.Application{}, domain.ErrNotFound
	}
	return a, nil
}

func (f *fakeApps) ListByCompany(_ context.Context, inn string) ([]domain.ApplicationSummary, error) {
	list := []domain.ApplicationSummary{}
	for _, a := range f.items {
		if a.CompanyINN == inn {
			list = append(list, domain.ApplicationSummary{ID: a.ID, ProductID: a.ProductID, Status: a.Status})
		}
	}
	return list, nil
}

func (f *fakeApps) UpdateDraft(ctx context.Context, id, inn string, version int, p domain.DraftPatch, unlinkOffer bool) (domain.Application, error) {
	a, err := f.GetForCompany(ctx, id, inn)
	if err != nil {
		return domain.Application{}, err
	}
	if a.Status != domain.StatusDraft {
		return domain.Application{}, domain.ErrConflict
	}
	if a.Version != version {
		return domain.Application{}, domain.ErrVersionConflict
	}
	a.Version++
	if p.ProductID != nil {
		a.ProductID = *p.ProductID
	}
	if p.Amount != nil {
		a.Amount = *p.Amount
	}
	if p.TermMonths != nil {
		a.TermMonths = *p.TermMonths
	}
	if p.Purpose != nil {
		a.Purpose = *p.Purpose
	}
	if p.Form != nil {
		a.Form = p.Form
	}
	if unlinkOffer {
		a.OfferID = nil
	}
	f.items[id] = a
	return a, nil
}

func (f *fakeApps) Submit(ctx context.Context, id, inn, by string, at time.Time) (domain.Application, error) {
	a, err := f.GetForCompany(ctx, id, inn)
	if err != nil {
		return domain.Application{}, err
	}
	if a.Status != domain.StatusDraft {
		return domain.Application{}, domain.ErrConflict
	}
	a.Status = domain.StatusSubmitted
	a.Version++
	a.SubmittedAt = &at
	a.History = append(a.History, domain.HistoryEntry{From: domain.StatusDraft, To: domain.StatusSubmitted, At: at, By: by})
	f.items[id] = a
	f.submits++
	return a, nil
}

var (
	clientA = domain.Principal{UserID: "user-a", Role: domain.RoleClient, CompanyINN: "7700000002"}
	clientB = domain.Principal{UserID: "user-b", Role: domain.RoleClient, CompanyINN: "7700000001"}
)

func newApplications(t *testing.T, orgType string) (*ApplicationService, *fakeApps) {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeApps()
	registered := testNow.AddDate(-4, 0, 0) // бизнесу 4 года: правило «от 9 месяцев» выполняется
	companies := &fakeCompanies{c: domain.Company{INN: clientA.CompanyINN, OrgType: orgType, RegistrationDate: &registered}}
	s := NewApplicationService(repo, companies, newFakeOffers(), cat, &fakePublisher{}, newFakeDocuments(), nil)
	s.now = func() time.Time { return testNow }
	return s, repo
}

// offersOf возвращает хранилище предложений сервиса, чтобы положить в него предложение для теста.
func offersOf(s *ApplicationService) *fakeOffers { return s.offers.(*fakeOffers) }

// publishedBy возвращает очередь-пустышку, в которую сервис отправляет задачи воркеру.
func publishedBy(s *ApplicationService) *fakePublisher { return s.publisher.(*fakePublisher) }

func str(s string) *string { return &s }
func i64(n int64) *int64   { return &n }
func num(n int) *int       { return &n }

// completeExpress — заполненный черновик продукта express (у него в форме только базовая группа).
func completeExpress() domain.DraftPatch {
	return domain.DraftPatch{
		ProductID: str("express"), Amount: i64(500_000), TermMonths: num(6), Purpose: str("Пополнение оборотных средств"),
		Form: map[string]any{
			"applicant": map[string]any{"position": "Директор", "has_collateral": false},
			"consents":  map[string]any{"bki_request": true, "personal_data": true},
		},
	}
}

func TestApplicationCreate(t *testing.T) {
	ctx := context.Background()

	t.Run("company and author come from the session", func(t *testing.T) {
		s, repo := newApplications(t, "OOO")
		a, err := s.Create(ctx, clientA, domain.DraftPatch{ProductID: str("express")})
		if err != nil {
			t.Fatal(err)
		}
		if a.CompanyINN != clientA.CompanyINN || a.Status != domain.StatusDraft || repo.createdBy[0] != clientA.UserID {
			t.Fatalf("got company %q status %q author %v", a.CompanyINN, a.Status, repo.createdBy)
		}
	})

	t.Run("an empty first save is allowed", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		if _, err := s.Create(ctx, clientA, domain.DraftPatch{}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unknown product and negative numbers are rejected", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		if _, err := s.Create(ctx, clientA, domain.DraftPatch{ProductID: str("nope")}); !errors.Is(err, domain.ErrUnknownProduct) {
			t.Fatalf("got %v, want ErrUnknownProduct", err)
		}
		if _, err := s.Create(ctx, clientA, domain.DraftPatch{Amount: i64(-1)}); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("got %v, want ErrInvalidInput", err)
		}
		if _, err := s.Create(ctx, clientA, domain.DraftPatch{TermMonths: num(-1)}); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("got %v, want ErrInvalidInput", err)
		}
	})
}

func TestApplicationIsVisibleOnlyToItsCompany(t *testing.T) {
	ctx := context.Background()
	s, _ := newApplications(t, "OOO")
	a, _ := s.Create(ctx, clientA, completeExpress())

	if _, err := s.Get(ctx, clientA, a.ID); err != nil {
		t.Fatalf("owner: %v", err)
	}
	if _, err := s.Get(ctx, clientB, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("other company Get: got %v, want ErrNotFound", err)
	}
	if _, err := s.Update(ctx, clientB, a.ID, a.Version, domain.DraftPatch{Purpose: str("x")}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("other company Update: got %v, want ErrNotFound", err)
	}
	if _, err := s.Submit(ctx, clientB, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("other company Submit: got %v, want ErrNotFound", err)
	}
	if list, _ := s.List(ctx, clientB); len(list) != 0 {
		t.Fatalf("other company sees %d applications", len(list))
	}
}

func TestApplicationUpdate(t *testing.T) {
	ctx := context.Background()
	s, _ := newApplications(t, "OOO")
	a, _ := s.Create(ctx, clientA, domain.DraftPatch{ProductID: str("express")})

	a, err := s.Update(ctx, clientA, a.ID, a.Version, domain.DraftPatch{Amount: i64(300_000), Purpose: str("Закупка")})
	if err != nil {
		t.Fatal(err)
	}
	if a.Amount != 300_000 || a.Purpose != "Закупка" || a.ProductID != "express" {
		t.Fatalf("only passed fields must change, got %+v", a)
	}

	// отправленную заявку менять нельзя
	full, _ := s.Update(ctx, clientA, a.ID, a.Version, completeExpress())
	if _, err := s.Submit(ctx, clientA, full.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, clientA, a.ID, full.Version, domain.DraftPatch{Purpose: str("после отправки")}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
}

func TestApplicationUpdate_StaleVersionDoesNotOverwrite(t *testing.T) {
	ctx := context.Background()
	s, _ := newApplications(t, "OOO")
	a, _ := s.Create(ctx, clientA, domain.DraftPatch{ProductID: str("express")})
	if a.Version != 1 {
		t.Fatalf("new draft version = %d, want 1", a.Version)
	}

	// первая вкладка сохраняет сумму: версия растёт
	first, err := s.Update(ctx, clientA, a.ID, 1, domain.DraftPatch{Amount: i64(100_000)})
	if err != nil || first.Version != 2 {
		t.Fatalf("got version %d, %v", first.Version, err)
	}

	// вторая вкладка всё ещё видит версию 1 и пытается сохранить своё: отказ, чужое изменение цело
	if _, err := s.Update(ctx, clientA, a.ID, 1, domain.DraftPatch{Amount: i64(999)}); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("got %v, want ErrVersionConflict", err)
	}
	current, _ := s.Get(ctx, clientA, a.ID)
	if current.Amount != 100_000 {
		t.Fatalf("a stale update overwrote the data: amount = %d", current.Amount)
	}

	// после перечитывания заявки (версия 2) сохранение проходит
	if _, err := s.Update(ctx, clientA, a.ID, current.Version, domain.DraftPatch{Amount: i64(999)}); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationSubmit_Incomplete(t *testing.T) {
	ctx := context.Background()
	s, repo := newApplications(t, "OOO")
	a, _ := s.Create(ctx, clientA, domain.DraftPatch{ProductID: str("express")})

	_, err := s.Submit(ctx, clientA, a.ID)
	var v *domain.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("got %v, want ValidationError", err)
	}
	want := []string{"amount", "term_months", "purpose", "applicant.position", "applicant.has_collateral", "consents.bki_request"}
	if !slices.Equal(v.Fields, want) {
		t.Fatalf("missing fields: got %v, want %v", v.Fields, want)
	}
	if repo.submits != 0 {
		t.Fatal("an incomplete application must not be submitted")
	}

	// без продукта сообщается и о нём
	empty, _ := s.Create(ctx, clientA, domain.DraftPatch{})
	_, err = s.Submit(ctx, clientA, empty.ID)
	if !errors.As(err, &v) || v.Fields[0] != "product_id" {
		t.Fatalf("got %v, want product_id first", err)
	}
}

func TestApplicationSubmit_Success(t *testing.T) {
	ctx := context.Background()
	s, repo := newApplications(t, "OOO")
	a, _ := s.Create(ctx, clientA, completeExpress())

	got, err := s.Submit(ctx, clientA, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusSubmitted || got.SubmittedAt == nil || len(got.History) != 1 || got.History[0].By != clientA.UserID {
		t.Fatalf("got %+v", got)
	}
	if repo.submits != 1 {
		t.Fatalf("submits = %d, want 1", repo.submits)
	}
}

func TestApplicationSubmit_SendsTheTaskToTheWorkerImmediately(t *testing.T) {
	ctx := context.Background()
	s, repo := newApplications(t, "OOO")
	a, _ := s.Create(ctx, clientA, completeExpress())

	if _, err := s.Submit(ctx, clientA, a.ID); err != nil {
		t.Fatal(err)
	}
	tasks := publishedBy(s).tasks
	if len(tasks) != 1 {
		t.Fatalf("tasks sent = %d, want 1", len(tasks))
	}
	task := tasks[0]
	if task.ApplicationID != a.ID || task.INN != clientA.CompanyINN || task.ProductID != "express" || task.Amount != 500_000 || task.TermMonths != 6 {
		t.Fatalf("task fields: %+v", task)
	}
	if task.Form["applicant"] == nil {
		t.Fatal("the form must be passed to the worker")
	}
	if !slices.Equal(repo.enqueued, []string{a.ID}) {
		t.Fatalf("enqueued marks = %v, want the application marked once", repo.enqueued)
	}
}

func TestApplicationSubmit_NothingIsSentWhenTheSubmitFails(t *testing.T) {
	ctx := context.Background()
	s, repo := newApplications(t, "OOO")

	incomplete, _ := s.Create(ctx, clientA, domain.DraftPatch{ProductID: str("express")})
	if _, err := s.Submit(ctx, clientA, incomplete.ID); err == nil {
		t.Fatal("want a validation error")
	}
	full, _ := s.Create(ctx, clientA, completeExpress())
	_, _ = s.Submit(ctx, clientA, full.ID)
	_, _ = s.Submit(ctx, clientA, full.ID) // повторная отправка: 409

	if n := len(publishedBy(s).tasks); n != 1 {
		t.Fatalf("tasks sent = %d, want 1 (only the successful submit)", n)
	}
	if len(repo.enqueued) != 1 {
		t.Fatalf("enqueued marks = %v, want 1", repo.enqueued)
	}
}

func TestApplicationSubmit_QueueFailureDoesNotFailTheSubmit(t *testing.T) {
	ctx := context.Background()

	t.Run("the task could not be published", func(t *testing.T) {
		s, repo := newApplications(t, "OOO")
		a, _ := s.Create(ctx, clientA, completeExpress())
		publishedBy(s).failFor = a.ID

		got, err := s.Submit(ctx, clientA, a.ID)
		if err != nil || got.Status != domain.StatusSubmitted {
			t.Fatalf("got %v, %v; the application must still be submitted", got.Status, err)
		}
		// метка не ставится: сборщик отправит задачу, когда пройдёт порог
		if len(repo.enqueued) != 0 {
			t.Fatalf("enqueued marks = %v, want none", repo.enqueued)
		}
	})

	t.Run("the enqueued mark could not be saved", func(t *testing.T) {
		s, repo := newApplications(t, "OOO")
		repo.markErr = errors.New("db is down")
		a, _ := s.Create(ctx, clientA, completeExpress())

		got, err := s.Submit(ctx, clientA, a.ID)
		if err != nil || got.Status != domain.StatusSubmitted {
			t.Fatalf("got %v, %v; the application must still be submitted", got.Status, err)
		}
	})
}

func TestApplicationSubmit_TwiceIsRejected(t *testing.T) {
	ctx := context.Background()
	s, repo := newApplications(t, "OOO")
	a, _ := s.Create(ctx, clientA, completeExpress())

	if _, err := s.Submit(ctx, clientA, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(ctx, clientA, a.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if repo.submits != 1 {
		t.Fatalf("submits = %d, want 1", repo.submits)
	}
}

func TestApplicationSubmit_ProductUnavailable(t *testing.T) {
	ctx := context.Background()

	t.Run("product only for OOO, company is IP", func(t *testing.T) {
		s, repo := newApplications(t, "IP")
		p := completeExpress()
		p.ProductID = str("ooo_loan")
		a, _ := s.Create(ctx, clientA, p)
		if _, err := s.Submit(ctx, clientA, a.ID); !errors.Is(err, domain.ErrProductUnavailable) {
			t.Fatalf("got %v, want ErrProductUnavailable", err)
		}
		if repo.submits != 0 {
			t.Fatal("must not be submitted")
		}
	})

	t.Run("term outside the product range", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		p := completeExpress()
		p.TermMonths = num(60) // express: 1-18 месяцев
		a, _ := s.Create(ctx, clientA, p)
		if _, err := s.Submit(ctx, clientA, a.ID); !errors.Is(err, domain.ErrProductUnavailable) {
			t.Fatalf("got %v, want ErrProductUnavailable", err)
		}
	})
}

func TestApplicationList_FillsProductNames(t *testing.T) {
	ctx := context.Background()
	s, _ := newApplications(t, "OOO")
	_, _ = s.Create(ctx, clientA, domain.DraftPatch{ProductID: str("express")})

	list, err := s.List(ctx, clientA)
	if err != nil || len(list) != 1 {
		t.Fatalf("got %v, %v", list, err)
	}
	if list[0].ProductName != "Экспресс-кредит для бизнеса" {
		t.Fatalf("product name %q", list[0].ProductName)
	}
}

// activeOffer — действующее предложение компании clientA: оборотный кредит до 1 млн на 12 месяцев.
func activeOffer(s *ApplicationService) domain.Offer {
	return offersOf(s).put(domain.Offer{
		CompanyINN: clientA.CompanyINN, ProductID: "working_capital", MaxAmount: 1_000_000, TermMonths: 12,
		RateFrom: 17.5, Status: domain.OfferActive, ValidUntil: testNow.AddDate(0, 0, 30),
	})
}

func TestApplicationCreateFromOffer(t *testing.T) {
	ctx := context.Background()

	t.Run("product, amount and term come from the offer, not from the request", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		o := activeOffer(s)
		a, created, err := s.CreateFromOffer(ctx, clientA, o.ID, domain.DraftPatch{
			ProductID: str("express"), Amount: i64(9_000_000), TermMonths: num(3), Purpose: str("Закупка"),
		})
		if err != nil || !created {
			t.Fatalf("got %v, created %v", err, created)
		}
		if a.ProductID != "working_capital" || a.Amount != 1_000_000 || a.TermMonths != 12 || a.Purpose != "Закупка" || a.OfferID == nil || *a.OfferID != o.ID {
			t.Fatalf("got %+v", a)
		}
	})

	t.Run("pressing the button twice returns the same draft", func(t *testing.T) {
		s, repo := newApplications(t, "OOO")
		o := activeOffer(s)
		first, _, _ := s.CreateFromOffer(ctx, clientA, o.ID, domain.DraftPatch{})
		second, created, err := s.CreateFromOffer(ctx, clientA, o.ID, domain.DraftPatch{})
		if err != nil || created || second.ID != first.ID || len(repo.items) != 1 {
			t.Fatalf("got %s (created %v, %v), want the same draft %s", second.ID, created, err, first.ID)
		}
	})

	t.Run("unusable offers", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		used := offersOf(s).put(domain.Offer{CompanyINN: clientA.CompanyINN, Status: domain.OfferUsed, ValidUntil: testNow.AddDate(0, 0, 1)})
		revoked := offersOf(s).put(domain.Offer{CompanyINN: clientA.CompanyINN, Status: domain.OfferRevoked, ValidUntil: testNow.AddDate(0, 0, 1)})
		expired := offersOf(s).put(domain.Offer{CompanyINN: clientA.CompanyINN, Status: domain.OfferActive, ValidUntil: testNow.Add(-time.Minute)})
		foreign := offersOf(s).put(domain.Offer{CompanyINN: clientB.CompanyINN, Status: domain.OfferActive, ValidUntil: testNow.AddDate(0, 0, 1)})

		for id, want := range map[string]error{
			used.ID:    domain.ErrOfferUsed,
			revoked.ID: domain.ErrOfferUsed,
			expired.ID: domain.ErrOfferExpired,
			foreign.ID: domain.ErrOfferNotFound,
			"nope":     domain.ErrOfferNotFound,
		} {
			if _, _, err := s.CreateFromOffer(ctx, clientA, id, domain.DraftPatch{}); !errors.Is(err, want) {
				t.Errorf("offer %s: got %v, want %v", id, err, want)
			}
		}
	})
}

func TestApplicationUpdate_ByOffer(t *testing.T) {
	ctx := context.Background()
	s, _ := newApplications(t, "OOO")
	o := activeOffer(s)
	a, _, _ := s.CreateFromOffer(ctx, clientA, o.ID, domain.DraftPatch{})

	if _, err := s.Update(ctx, clientA, a.ID, a.Version, domain.DraftPatch{Amount: i64(1_500_000)}); !errors.Is(err, domain.ErrOfferAmountExceeded) {
		t.Fatalf("amount above the limit: got %v, want ErrOfferAmountExceeded", err)
	}
	a, err := s.Update(ctx, clientA, a.ID, a.Version, domain.DraftPatch{Amount: i64(700_000)})
	if err != nil || a.Amount != 700_000 || a.OfferID == nil {
		t.Fatalf("a smaller amount keeps the offer: got %+v, %v", a, err)
	}
	a, err = s.Update(ctx, clientA, a.ID, a.Version, domain.DraftPatch{ProductID: str("express"), Amount: i64(3_000_000)})
	if err != nil || a.OfferID != nil || a.ProductID != "express" {
		t.Fatalf("changing the product unlinks the offer: got %+v, %v", a, err)
	}
}

func TestApplicationSubmit_ByOffer(t *testing.T) {
	ctx := context.Background()
	fill := domain.DraftPatch{
		Purpose: str("Пополнение оборотных средств"),
		Form: map[string]any{
			"applicant": map[string]any{"position": "Директор", "has_collateral": false},
			"consents":  map[string]any{"bki_request": true, "personal_data": true},
		},
	}

	t.Run("a valid offer is submitted", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		o := activeOffer(s)
		a, _, _ := s.CreateFromOffer(ctx, clientA, o.ID, fill)
		if got, err := s.Submit(ctx, clientA, a.ID); err != nil || got.Status != domain.StatusSubmitted {
			t.Fatalf("got %v, %v", got.Status, err)
		}
	})

	t.Run("the offer expired while the form was being filled", func(t *testing.T) {
		s, repo := newApplications(t, "OOO")
		o := activeOffer(s)
		a, _, _ := s.CreateFromOffer(ctx, clientA, o.ID, fill)
		s.now = func() time.Time { return testNow.AddDate(0, 0, 31) }

		if _, err := s.Submit(ctx, clientA, a.ID); !errors.Is(err, domain.ErrOfferInvalid) {
			t.Fatalf("got %v, want ErrOfferInvalid", err)
		}
		if repo.submits != 0 {
			t.Fatal("must not be submitted")
		}
	})
}

func TestApplicationSubmit_CatalogRulesFromTheCalculator(t *testing.T) {
	ctx := context.Background()

	t.Run("amount below the product minimum", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		p := completeExpress()
		p.Amount = i64(10_000) // express: от 50 000
		a, _ := s.Create(ctx, clientA, p)
		if _, err := s.Submit(ctx, clientA, a.ID); !errors.Is(err, domain.ErrProductUnavailable) {
			t.Fatalf("got %v, want ErrProductUnavailable", err)
		}
	})

	t.Run("young business: product needs 9 months", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		young := testNow.AddDate(0, -5, 0)
		s.companies = &fakeCompanies{c: domain.Company{INN: clientA.CompanyINN, OrgType: "OOO", RegistrationDate: &young}}
		p := completeExpress()
		p.ProductID, p.TermMonths = str("working_capital"), num(12)
		a, _ := s.Create(ctx, clientA, p)
		if _, err := s.Submit(ctx, clientA, a.ID); !errors.Is(err, domain.ErrProductUnavailable) {
			t.Fatalf("got %v, want ErrProductUnavailable", err)
		}
	})

	t.Run("young business: express above the young limit", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		young := testNow.AddDate(0, -5, 0)
		s.companies = &fakeCompanies{c: domain.Company{INN: clientA.CompanyINN, OrgType: "OOO", RegistrationDate: &young}}
		p := completeExpress()
		p.Amount = i64(3_000_000) // express: молодому бизнесу до 2 000 000
		a, _ := s.Create(ctx, clientA, p)
		if _, err := s.Submit(ctx, clientA, a.ID); !errors.Is(err, domain.ErrProductUnavailable) {
			t.Fatalf("got %v, want ErrProductUnavailable", err)
		}
	})

	t.Run("no consent to the credit bureau request", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		p := completeExpress()
		p.Form = map[string]any{"applicant": map[string]any{"position": "Директор", "has_collateral": false}}
		a, _ := s.Create(ctx, clientA, p)
		var v *domain.ValidationError
		if _, err := s.Submit(ctx, clientA, a.ID); !errors.As(err, &v) || !slices.Equal(v.Fields, []string{"consents.bki_request"}) {
			t.Fatalf("got %v, want the missing consent", err)
		}
	})
}

func (f *fakeApps) Accept(ctx context.Context, id, inn string, from domain.Status, t domain.LoanTerms, by string, at time.Time) (domain.Application, error) {
	a, err := f.GetForCompany(ctx, id, inn)
	if err != nil {
		return domain.Application{}, err
	}
	if a.Status != from {
		return domain.Application{}, domain.ErrNotAwaitingDecision
	}
	a.Status, a.Amount, a.TermMonths = domain.StatusActive, t.Amount, t.TermMonths
	a.Rate, a.ContractNumber, a.IssuedAt = &t.Rate, &t.ContractNumber, &t.IssuedAt
	for _, step := range [][2]domain.Status{{from, domain.StatusSigning}, {domain.StatusSigning, domain.StatusSigned},
		{domain.StatusSigned, domain.StatusDisbursed}, {domain.StatusDisbursed, domain.StatusActive}} {
		a.History = append(a.History, domain.HistoryEntry{From: step[0], To: step[1], At: at, By: by})
	}
	f.items[id] = a
	return a, nil
}

func (f *fakeApps) Decline(ctx context.Context, id, inn, by string, at time.Time) (domain.Application, error) {
	a, err := f.GetForCompany(ctx, id, inn)
	if err != nil {
		return domain.Application{}, err
	}
	if a.Status != domain.StatusCounterOffer {
		return domain.Application{}, domain.ErrNotAwaitingDecision
	}
	a.Status, a.OfferID = domain.StatusDraft, nil
	f.items[id] = a
	return a, nil
}

// decided кладёт заявку в статус решения банка с решением decision.
func decided(repo *fakeApps, status domain.Status, decision string) domain.Application {
	a, _ := repo.Create(context.Background(), domain.Application{CompanyINN: clientA.CompanyINN, ProductID: "working_capital", Amount: 2_000_000, TermMonths: 18}, clientA.UserID)
	a.Status, a.Decision = status, json.RawMessage(decision)
	repo.items[a.ID] = a
	return a
}

func TestApplicationAccept(t *testing.T) {
	ctx := context.Background()

	t.Run("approval: requested terms, rate from the decision", func(t *testing.T) {
		s, repo := newApplications(t, "OOO")
		a := decided(repo, domain.StatusApproved, `{"outcome":"APPROVED","rate":18.4}`)
		got, err := s.Accept(ctx, clientA, a.ID)
		if err != nil || got.Status != domain.StatusActive {
			t.Fatalf("got %v, %v", got.Status, err)
		}
		if got.Amount != 2_000_000 || got.TermMonths != 18 || *got.Rate != 18.4 || !got.IssuedAt.Equal(dateOf(testNow)) {
			t.Fatalf("terms: %d %d %v %v", got.Amount, got.TermMonths, *got.Rate, got.IssuedAt)
		}
		if *got.ContractNumber == "" || len(got.History) != 4 || got.History[3].To != domain.StatusActive {
			t.Fatalf("contract %q, history %+v", *got.ContractNumber, got.History)
		}
	})

	t.Run("counter offer: amount, term and rate from decision.offer", func(t *testing.T) {
		s, repo := newApplications(t, "OOO")
		a := decided(repo, domain.StatusCounterOffer, `{"outcome":"COUNTER_OFFER","rate":25,"offer":{"amount":1500000,"term_months":24,"rate":21.5}}`)
		got, err := s.Accept(ctx, clientA, a.ID)
		if err != nil || got.Amount != 1_500_000 || got.TermMonths != 24 || *got.Rate != 21.5 {
			t.Fatalf("got %d %d %v, %v", got.Amount, got.TermMonths, got.Rate, err)
		}
	})

	t.Run("no rate in the decision: the product base rate", func(t *testing.T) {
		s, repo := newApplications(t, "OOO")
		a := decided(repo, domain.StatusApproved, `{"outcome":"APPROVED"}`)
		got, _ := s.Accept(ctx, clientA, a.ID)
		if *got.Rate != 19 { // working_capital
			t.Fatalf("rate %v, want 19", *got.Rate)
		}
	})

	t.Run("not awaiting a decision", func(t *testing.T) {
		s, repo := newApplications(t, "OOO")
		a := decided(repo, domain.StatusScoring, `{}`)
		if _, err := s.Accept(ctx, clientA, a.ID); !errors.Is(err, domain.ErrNotAwaitingDecision) {
			t.Fatalf("got %v, want ErrNotAwaitingDecision", err)
		}
		if _, err := s.Accept(ctx, clientB, a.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("other company: got %v, want ErrNotFound", err)
		}
	})
}

func TestApplicationDecline(t *testing.T) {
	ctx := context.Background()
	s, repo := newApplications(t, "OOO")

	counter := decided(repo, domain.StatusCounterOffer, `{"outcome":"COUNTER_OFFER"}`)
	got, err := s.Decline(ctx, clientA, counter.ID)
	if err != nil || got.Status != domain.StatusDraft {
		t.Fatalf("got %v, %v", got.Status, err)
	}
	approved := decided(repo, domain.StatusApproved, `{"outcome":"APPROVED"}`)
	if _, err := s.Decline(ctx, clientA, approved.ID); !errors.Is(err, domain.ErrNotAwaitingDecision) {
		t.Fatalf("approval cannot be declined here: got %v", err)
	}
}

func TestContractNumber(t *testing.T) {
	got := contractNumber("working_capital", "app-1", testNow)
	if got != contractNumber("working_capital", "app-1", testNow) || got[:len("0ОС-2026/")] != "0ОС-2026/" || len(got) != len("0ОС-2026/0000") {
		t.Fatalf("got %q", got)
	}
}
