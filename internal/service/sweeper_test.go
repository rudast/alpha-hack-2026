package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type fakeClaimer struct {
	mu        sync.Mutex
	stuck     []domain.Application
	err       error
	calls     int
	olderThan time.Duration
	limit     int
}

func (f *fakeClaimer) ClaimStaleSubmitted(_ context.Context, olderThan time.Duration, limit int) ([]domain.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.olderThan, f.limit = olderThan, limit
	return f.stuck, f.err
}

func (f *fakeClaimer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakePublisher struct {
	tasks   []domain.ApplicationTask
	failFor string // идентификатор заявки, для которой отправка падает
}

func (f *fakePublisher) Publish(_ context.Context, t domain.ApplicationTask) error {
	if t.ApplicationID == f.failFor {
		return errors.New("redis is down")
	}
	f.tasks = append(f.tasks, t)
	return nil
}

type failingCompanies struct{}

func (failingCompanies) GetByINN(context.Context, string) (domain.Company, error) {
	return domain.Company{}, errors.New("db is down")
}

func stuckApp(id string) domain.Application {
	return domain.Application{
		ID: id, CompanyINN: "7700000002", ProductID: "express", Amount: 500_000, TermMonths: 6,
		Status: domain.StatusSubmitted, Form: map[string]any{"applicant": map[string]any{"position": "Директор"}},
	}
}

func TestSweep_SendsATaskForEveryStuckApplication(t *testing.T) {
	debt, equity := int64(2_400_000), int64(4_000_000)
	ratio := 0.6
	registered := time.Date(2022, 9, 1, 0, 0, 0, 0, time.UTC)
	companies := &fakeCompanies{c: domain.Company{
		INN: "7700000002", OrgType: "OOO", OKVED: "41.20", RegistrationDate: &registered,
		TotalDebt: &debt, Equity: &equity, DebtToEquity: &ratio,
		Registry: json.RawMessage(`{"status":"ACTIVE"}`),
	}}
	claimer := &fakeClaimer{stuck: []domain.Application{stuckApp("a1"), stuckApp("a2")}}
	publisher := &fakePublisher{}
	s := NewApplicationSweeper(claimer, companies, publisher, time.Minute, 2*time.Minute)

	sent, err := s.Sweep(context.Background())
	if err != nil || sent != 2 || len(publisher.tasks) != 2 {
		t.Fatalf("sent %d, tasks %d, err %v; want 2 and 2", sent, len(publisher.tasks), err)
	}
	task := publisher.tasks[0]
	if task.ApplicationID != "a1" || task.INN != "7700000002" || task.ProductID != "express" || task.Amount != 500_000 || task.TermMonths != 6 {
		t.Fatalf("task fields: %+v", task)
	}
	if task.Form["applicant"] == nil {
		t.Fatal("the form must be passed to the worker")
	}
	if task.Debt == nil || *task.Debt != debt || *task.Equity != equity || *task.DebtToEquity != ratio {
		t.Fatalf("company figures are missing: %+v", task)
	}
	profile := task.Company
	if profile.OrgType != "OOO" || profile.OKVED != "41.20" || profile.RegistrationDate != "2022-09-01" {
		t.Fatalf("company profile: %+v", profile)
	}
	if string(profile.Registry) != `{"status":"ACTIVE"}` {
		t.Fatalf("registry must be passed as stored, got %s", profile.Registry)
	}
	// данных БКИ и документов у компании нет: воркер получает пустые объекты, а не null
	if string(profile.CreditHistory) != "{}" || string(profile.Financials) != "{}" {
		t.Fatalf("missing data must be an empty object, got %s and %s", profile.CreditHistory, profile.Financials)
	}
}

func TestSweep_AsksForApplicationsOlderThanTheThreshold(t *testing.T) {
	claimer := &fakeClaimer{}
	s := NewApplicationSweeper(claimer, &fakeCompanies{}, &fakePublisher{}, time.Minute, 2*time.Minute)

	sent, err := s.Sweep(context.Background())
	if err != nil || sent != 0 {
		t.Fatalf("got %d, %v; want nothing to send", sent, err)
	}
	if claimer.olderThan != 2*time.Minute || claimer.limit != sweepBatch {
		t.Fatalf("claimed with olderThan %v, limit %d", claimer.olderThan, claimer.limit)
	}
}

func TestSweep_OneFailureDoesNotStopTheOthers(t *testing.T) {
	claimer := &fakeClaimer{stuck: []domain.Application{stuckApp("a1"), stuckApp("a2"), stuckApp("a3")}}
	publisher := &fakePublisher{failFor: "a2"}
	s := NewApplicationSweeper(claimer, &fakeCompanies{c: domain.Company{INN: "7700000002"}}, publisher, time.Minute, 2*time.Minute)

	sent, err := s.Sweep(context.Background())
	if err != nil || sent != 2 {
		t.Fatalf("got %d, %v; want 2 sent and no error", sent, err)
	}
	if publisher.tasks[0].ApplicationID != "a1" || publisher.tasks[1].ApplicationID != "a3" {
		t.Fatalf("sent %v, want a1 and a3", publisher.tasks)
	}
}

func TestSweep_CompanyErrorSkipsTheApplication(t *testing.T) {
	claimer := &fakeClaimer{stuck: []domain.Application{stuckApp("a1")}}
	publisher := &fakePublisher{}
	s := NewApplicationSweeper(claimer, failingCompanies{}, publisher, time.Minute, 2*time.Minute)

	sent, err := s.Sweep(context.Background())
	if err != nil || sent != 0 || len(publisher.tasks) != 0 {
		t.Fatalf("got %d, %v, tasks %d; want nothing sent", sent, err, len(publisher.tasks))
	}
}

func TestSweep_ClaimErrorIsReturned(t *testing.T) {
	claimer := &fakeClaimer{err: errors.New("db is down")}
	s := NewApplicationSweeper(claimer, &fakeCompanies{}, &fakePublisher{}, time.Minute, 2*time.Minute)

	if _, err := s.Sweep(context.Background()); err == nil {
		t.Fatal("want the claim error")
	}
}

func TestRun_SweepsRepeatedlyAndStopsWithTheContext(t *testing.T) {
	claimer := &fakeClaimer{}
	s := NewApplicationSweeper(claimer, &fakeCompanies{}, &fakePublisher{}, 5*time.Millisecond, time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after the context was cancelled")
	}
	if claimer.callCount() < 2 {
		t.Fatalf("swept %d times, want repeated sweeps", claimer.callCount())
	}
}
