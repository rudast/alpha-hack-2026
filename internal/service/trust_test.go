package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/queue"
)

var testNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

type fakeCompanies struct {
	mu      sync.Mutex
	c       domain.Company
	updates int
}

func (f *fakeCompanies) GetByINN(context.Context, string) (domain.Company, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.c, nil
}

func (f *fakeCompanies) UpdateFinancials(_ context.Context, _ string, debt, equity int64, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates++
	f.c.TotalDebt, f.c.Equity, f.c.DebtToEquity, f.c.FetchedAt = &debt, &equity, domain.DebtToEquity(debt, equity), &at
	return nil
}

type fakeHistory struct {
	calls atomic.Int32
	h     queue.History
	err   error
	gate  chan struct{}
}

func (f *fakeHistory) FetchHistory(context.Context, string) (queue.History, error) {
	f.calls.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	return f.h, f.err
}

func newTrust(c domain.Company, h *fakeHistory) (*TrustService, *fakeCompanies) {
	repo := &fakeCompanies{c: c}
	s := NewTrustService(repo, h, 12*time.Hour, time.Second)
	s.now = func() time.Time { return testNow }
	return s, repo
}

func ago(d time.Duration) *time.Time { v := testNow.Add(-d); return &v }

func TestEnsureTrustRatio_FreshDataIsNotRefetched(t *testing.T) {
	h := &fakeHistory{}
	s, _ := newTrust(domain.Company{INN: "1", FetchedAt: ago(time.Hour)}, h)

	if _, err := s.EnsureTrustRatio(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	if h.calls.Load() != 0 {
		t.Fatalf("worker called %d times, want 0", h.calls.Load())
	}
}

func TestEnsureTrustRatio_NeverFetchedAsksWorkerAndStores(t *testing.T) {
	h := &fakeHistory{h: queue.History{Debt: 3_000_000, Equity: 5_000_000}}
	s, repo := newTrust(domain.Company{INN: "1"}, h)

	c, err := s.EnsureTrustRatio(context.Background(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if c.DebtToEquity == nil || *c.DebtToEquity != 0.6 || repo.updates != 1 {
		t.Fatalf("ratio %v, updates %d", c.DebtToEquity, repo.updates)
	}
}

func TestEnsureTrustRatio_StaleDataIsRefreshed(t *testing.T) {
	h := &fakeHistory{h: queue.History{Debt: 1_800_000, Equity: 3_000_000}}
	old := int64(1_500_000)
	s, _ := newTrust(domain.Company{INN: "1", TotalDebt: &old, FetchedAt: ago(13 * time.Hour)}, h)

	c, err := s.EnsureTrustRatio(context.Background(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if *c.TotalDebt != 1_800_000 || h.calls.Load() != 1 {
		t.Fatalf("debt %d, calls %d", *c.TotalDebt, h.calls.Load())
	}
}

func TestEnsureTrustRatio_WorkerTimeout(t *testing.T) {
	timeout := &fakeHistory{err: domain.ErrUpstreamTimeout}

	t.Run("no previous data fails", func(t *testing.T) {
		s, _ := newTrust(domain.Company{INN: "1"}, timeout)
		if _, err := s.EnsureTrustRatio(context.Background(), "1"); !errors.Is(err, domain.ErrUpstreamTimeout) {
			t.Fatalf("got %v, want ErrUpstreamTimeout", err)
		}
	})

	t.Run("stale data is used as is", func(t *testing.T) {
		debt := int64(100)
		s, _ := newTrust(domain.Company{INN: "1", TotalDebt: &debt, FetchedAt: ago(48 * time.Hour)}, timeout)
		c, err := s.EnsureTrustRatio(context.Background(), "1")
		if err != nil || *c.TotalDebt != 100 {
			t.Fatalf("got %v, %v", c.TotalDebt, err)
		}
	})
}

func TestEnsureTrustRatio_ConcurrentCallsShareOneRequest(t *testing.T) {
	h := &fakeHistory{h: queue.History{Debt: 1, Equity: 2}, gate: make(chan struct{})}
	s, _ := newTrust(domain.Company{INN: "1"}, h)

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.EnsureTrustRatio(context.Background(), "1"); err != nil {
				t.Error(err)
			}
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(h.gate)
	wg.Wait()

	if h.calls.Load() != 1 {
		t.Fatalf("worker called %d times, want 1", h.calls.Load())
	}
}
