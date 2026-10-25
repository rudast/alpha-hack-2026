package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/catalog"
	"github.com/rudast/alpha-hack-2026/internal/domain"
)

func (f *fakeDocuments) Open(appID, id string) (io.ReadCloser, domain.Document, error) {
	data, ok := f.files[appID][id]
	if !ok {
		return nil, domain.Document{}, domain.ErrDocumentNotFound
	}
	kind, name, _ := strings.Cut(id, "/")
	return io.NopCloser(bytes.NewReader(data)), domain.Document{ID: id, Kind: kind, Filename: name, Size: int64(len(data))}, nil
}

func (f *fakeApps) Resume(ctx context.Context, id, inn string, to domain.Status, by string, at time.Time) (domain.Application, error) {
	a, err := f.GetForCompany(ctx, id, inn)
	if err != nil {
		return domain.Application{}, err
	}
	if a.Status != domain.StatusInfoRequested {
		return domain.Application{}, domain.ErrNotInfoRequested
	}
	a.History = append(a.History, domain.HistoryEntry{From: a.Status, To: to, At: at, By: by})
	a.Status, a.InfoRequest, a.AssignedTo = to, nil, nil
	f.items[id] = a
	return a, nil
}

// fakeReview повторяет условия настоящего репозитория: решение применяется, только если заявка
// на проверке и закреплена за этим сотрудником.
type fakeReview struct {
	app     domain.Application
	applied *domain.ReviewUpdate
}

func (f *fakeReview) ListReview(context.Context, string) ([]domain.ReviewSummary, error) {
	return nil, nil
}
func (f *fakeReview) GetByID(_ context.Context, id string) (domain.Application, error) {
	if id != f.app.ID {
		return domain.Application{}, domain.ErrNotFound
	}
	return f.app, nil
}
func (f *fakeReview) Take(_ context.Context, _, employee string, _ time.Time) (domain.Application, error) {
	if f.app.AssignedTo != nil && *f.app.AssignedTo != employee {
		return domain.Application{}, &domain.AssignedError{Name: "Сидорова Анна Сергеевна"}
	}
	f.app.AssignedTo = &employee
	return f.app, nil
}
func (f *fakeReview) Release(context.Context, string, string) (domain.Application, error) {
	f.app.AssignedTo = nil
	return f.app, nil
}
func (f *fakeReview) ReleaseStale(context.Context, time.Duration) (int64, error) { return 0, nil }
func (f *fakeReview) Review(_ context.Context, _, employee string, u domain.ReviewUpdate, _ time.Time) (domain.Application, error) {
	if f.app.Status != domain.StatusManualReview {
		return domain.Application{}, domain.ErrNotInReview
	}
	if f.app.AssignedTo == nil || *f.app.AssignedTo != employee {
		return domain.Application{}, domain.ErrNotAssignedToYou
	}
	f.applied = &u
	f.app.Status = u.To
	return f.app, nil
}

type fakeEmployees struct{}

func (fakeEmployees) GetByID(context.Context, string) (domain.User, error) {
	return domain.User{FullName: "Сидорова Анна Сергеевна"}, nil
}

var employee = domain.Principal{UserID: "emp-1", Role: domain.RoleEmployee}

// newReview — заявка на оборотный кредит ООО на ручной проверке, взятая сотрудником employee.
func newReview(t *testing.T) (*ReviewService, *fakeReview, *fakeDocuments) {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	registered := testNow.AddDate(-4, 0, 0)
	companies := &fakeCompanies{c: domain.Company{INN: clientA.CompanyINN, OrgType: "OOO", RegistrationDate: &registered}}
	assigned := employee.UserID
	repo := &fakeReview{app: domain.Application{
		ID: "app-1", CompanyINN: clientA.CompanyINN, ProductID: "working_capital", Amount: 2_000_000, TermMonths: 18,
		Status: domain.StatusManualReview, AssignedTo: &assigned,
		Form: map[string]any{"applicant": map[string]any{"position": "Директор", "has_collateral": false}},
	}}
	docs := newFakeDocuments()
	for _, kind := range []string{"passport", "bki_consent", "financial_statements"} {
		_, _ = docs.Save("app-1", kind, kind+".pdf", strings.NewReader("%PDF"))
	}
	s := NewReviewService(repo, fakeEmployees{}, companies, docs, cat)
	s.now = func() time.Time { return testNow }
	return s, repo, docs
}

func TestReviewDecide_SimpleActions(t *testing.T) {
	ctx := context.Background()

	t.Run("approve", func(t *testing.T) {
		s, repo, _ := newReview(t)
		if _, err := s.Decide(ctx, employee, "app-1", domain.ReviewDecision{Action: domain.ReviewApprove}); err != nil {
			t.Fatal(err)
		}
		if repo.applied.To != domain.StatusApproved || !strings.Contains(string(repo.applied.Decision), `"outcome":"APPROVED"`) {
			t.Fatalf("applied %+v %s", repo.applied, repo.applied.Decision)
		}
	})

	t.Run("reject needs a reason", func(t *testing.T) {
		s, repo, _ := newReview(t)
		var v *domain.ValidationError
		if _, err := s.Decide(ctx, employee, "app-1", domain.ReviewDecision{Action: domain.ReviewReject, Reason: "  "}); !errors.As(err, &v) || v.Fields[0] != "reason" {
			t.Fatalf("got %v, want missing reason", err)
		}
		if _, err := s.Decide(ctx, employee, "app-1", domain.ReviewDecision{Action: domain.ReviewReject, Reason: "Высокая долговая нагрузка"}); err != nil {
			t.Fatal(err)
		}
		if repo.applied.To != domain.StatusRejected || repo.applied.Comment != "Высокая долговая нагрузка" ||
			!strings.Contains(string(repo.applied.Decision), `"reason":"Высокая долговая нагрузка"`) {
			t.Fatalf("applied %+v %s", repo.applied, repo.applied.Decision)
		}
	})

	t.Run("request info needs the text", func(t *testing.T) {
		s, repo, _ := newReview(t)
		var v *domain.ValidationError
		if _, err := s.Decide(ctx, employee, "app-1", domain.ReviewDecision{Action: domain.ReviewRequestInfo}); !errors.As(err, &v) {
			t.Fatalf("got %v, want missing info_request", err)
		}
		if _, err := s.Decide(ctx, employee, "app-1", domain.ReviewDecision{Action: domain.ReviewRequestInfo, InfoRequest: "Выписка за 12 месяцев"}); err != nil {
			t.Fatal(err)
		}
		if repo.applied.To != domain.StatusInfoRequested || *repo.applied.InfoRequest != "Выписка за 12 месяцев" {
			t.Fatalf("applied %+v", repo.applied)
		}
	})

	t.Run("unknown action", func(t *testing.T) {
		s, _, _ := newReview(t)
		if _, err := s.Decide(ctx, employee, "app-1", domain.ReviewDecision{Action: "nope"}); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("only the employee who took the application decides", func(t *testing.T) {
		s, _, _ := newReview(t)
		other := domain.Principal{UserID: "emp-2", Role: domain.RoleEmployee}
		if _, err := s.Decide(ctx, other, "app-1", domain.ReviewDecision{Action: domain.ReviewApprove}); !errors.Is(err, domain.ErrNotAssignedToYou) {
			t.Fatalf("got %v, want ErrNotAssignedToYou", err)
		}
	})
}

func TestReviewDecide_CounterOffer(t *testing.T) {
	ctx := context.Background()
	offer := func(product string, amount int64, term int) domain.ReviewDecision {
		return domain.ReviewDecision{Action: domain.ReviewCounterOffer, ProductID: product, Amount: amount, TermMonths: term, Rate: 21.5}
	}

	t.Run("same product by default", func(t *testing.T) {
		s, repo, _ := newReview(t)
		if _, err := s.Decide(ctx, employee, "app-1", offer("", 1_500_000, 12)); err != nil {
			t.Fatal(err)
		}
		var d struct {
			Outcome string `json:"outcome"`
			Offer   struct {
				ProductID  string  `json:"product_id"`
				Amount     int64   `json:"amount"`
				TermMonths int     `json:"term_months"`
				Rate       float64 `json:"rate"`
			} `json:"offer"`
		}
		_ = json.Unmarshal(repo.applied.Decision, &d)
		if repo.applied.To != domain.StatusCounterOffer || d.Outcome != "COUNTER_OFFER" ||
			d.Offer.ProductID != "working_capital" || d.Offer.Amount != 1_500_000 || d.Offer.TermMonths != 12 || d.Offer.Rate != 21.5 {
			t.Fatalf("decision %s", repo.applied.Decision)
		}
	})

	t.Run("another product without enough data: missing fields and documents", func(t *testing.T) {
		s, _, _ := newReview(t)
		var v *domain.ValidationError
		_, err := s.Decide(ctx, employee, "app-1", offer("auto_loan", 1_000_000, 24))
		if !errors.As(err, &v) || !slices.Contains(v.Fields, "vehicle.vin") || !slices.Contains(v.Fields, "documents.vehicle_registration") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("another product the company can take", func(t *testing.T) {
		s, repo, _ := newReview(t)
		if _, err := s.Decide(ctx, employee, "app-1", offer("ooo_loan", 1_000_000, 24)); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(repo.applied.Decision), `"product_id":"ooo_loan"`) {
			t.Fatalf("decision %s", repo.applied.Decision)
		}
	})

	t.Run("product, amount or term not allowed", func(t *testing.T) {
		s, _, _ := newReview(t)
		for name, d := range map[string]domain.ReviewDecision{
			"IP-only product for OOO": offer("ip_loan", 1_000_000, 12),
			"amount below minimum":    offer("ooo_loan", 100_000, 12),
			"term above maximum":      offer("working_capital", 1_000_000, 60),
		} {
			if _, err := s.Decide(ctx, employee, "app-1", d); !errors.Is(err, domain.ErrProductUnavailable) {
				t.Errorf("%s: got %v, want ErrProductUnavailable", name, err)
			}
		}
		if _, err := s.Decide(ctx, employee, "app-1", offer("nope", 1_000_000, 12)); !errors.Is(err, domain.ErrUnknownProduct) {
			t.Errorf("unknown product: got %v", err)
		}
		if _, err := s.Decide(ctx, employee, "app-1", domain.ReviewDecision{Action: domain.ReviewCounterOffer, Amount: 1_000_000, TermMonths: 12}); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("no rate: got %v", err)
		}
	})
}

func TestReviewTake_AnotherEmployeeGetsTheName(t *testing.T) {
	s, _, _ := newReview(t)
	var assigned *domain.AssignedError
	if _, err := s.Take(context.Background(), domain.Principal{UserID: "emp-2"}, "app-1"); !errors.As(err, &assigned) || assigned.Name == "" {
		t.Fatalf("got %v, want the name of who took it", err)
	}
}

func TestReviewCard_ShowsWhoTookIt(t *testing.T) {
	s, _, _ := newReview(t)
	c, err := s.Card(context.Background(), "app-1")
	if err != nil || c.AssignedName != "Сидорова Анна Сергеевна" || len(c.Application.Documents) != 3 || c.ProductName == "" {
		t.Fatalf("card %+v, %v", c, err)
	}
}

func TestApplicationResume(t *testing.T) {
	ctx := context.Background()
	requested := func(repo *fakeApps, from domain.Status) domain.Application {
		a, _ := repo.Create(ctx, domain.Application{CompanyINN: clientA.CompanyINN, ProductID: "express"}, clientA.UserID)
		text := "Выписка"
		a.Status, a.InfoRequest = domain.StatusInfoRequested, &text
		a.History = []domain.HistoryEntry{{From: from, To: domain.StatusInfoRequested}}
		repo.items[a.ID] = a
		return a
	}

	t.Run("requested by the employee: back to the review pool, no task", func(t *testing.T) {
		s, repo := newApplications(t, "OOO")
		a := requested(repo, domain.StatusManualReview)
		got, err := s.Resume(ctx, clientA, a.ID)
		if err != nil || got.Status != domain.StatusManualReview || got.AssignedTo != nil || len(publishedBy(s).tasks) != 0 {
			t.Fatalf("got %s, %v, tasks %d", got.Status, err, len(publishedBy(s).tasks))
		}
	})

	t.Run("requested by the worker: back to data check, the task is sent again", func(t *testing.T) {
		s, repo := newApplications(t, "OOO")
		a := requested(repo, domain.StatusDataCheck)
		got, err := s.Resume(ctx, clientA, a.ID)
		if err != nil || got.Status != domain.StatusDataCheck || len(publishedBy(s).tasks) != 1 {
			t.Fatalf("got %s, %v, tasks %d", got.Status, err, len(publishedBy(s).tasks))
		}
	})

	t.Run("not waiting for documents", func(t *testing.T) {
		s, repo := newApplications(t, "OOO")
		a, _ := repo.Create(ctx, domain.Application{CompanyINN: clientA.CompanyINN}, clientA.UserID)
		if _, err := s.Resume(ctx, clientA, a.ID); !errors.Is(err, domain.ErrNotInfoRequested) {
			t.Fatalf("got %v", err)
		}
	})
}
