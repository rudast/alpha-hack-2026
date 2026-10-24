package service

import (
	"context"
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

func (f *fakeApps) UpdateDraft(ctx context.Context, id, inn string, version int, p domain.DraftPatch) (domain.Application, error) {
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
	companies := &fakeCompanies{c: domain.Company{INN: clientA.CompanyINN, OrgType: orgType}}
	s := NewApplicationService(repo, companies, cat, &fakePublisher{})
	s.now = func() time.Time { return testNow }
	return s, repo
}

// publishedBy возвращает очередь-пустышку, в которую сервис отправляет задачи воркеру.
func publishedBy(s *ApplicationService) *fakePublisher { return s.publisher.(*fakePublisher) }

func str(s string) *string { return &s }
func i64(n int64) *int64   { return &n }
func num(n int) *int       { return &n }

// completeExpress — заполненный черновик продукта express (у него в форме только базовая группа).
func completeExpress() domain.DraftPatch {
	return domain.DraftPatch{
		ProductID: str("express"), Amount: i64(500_000), TermMonths: num(6), Purpose: str("Пополнение оборотных средств"),
		Form: map[string]any{"applicant": map[string]any{"position": "Директор", "has_collateral": false}},
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
	want := []string{"amount", "term_months", "purpose", "applicant.position", "applicant.has_collateral"}
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
