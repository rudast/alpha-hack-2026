package domain

import (
	"testing"
	"time"
)

func TestDebtToEquity(t *testing.T) {
	if got := DebtToEquity(3_000_000, 5_000_000); got == nil || *got != 0.6 {
		t.Fatalf("got %v, want 0.6", got)
	}
	for _, equity := range []int64{0, -1} {
		if got := DebtToEquity(1, equity); got != nil {
			t.Fatalf("equity %d: want nil, got %v", equity, *got)
		}
	}
}

func TestNeedsRefresh(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	ttl := 12 * time.Hour
	at := func(ago time.Duration) *time.Time { v := now.Add(-ago); return &v }

	cases := []struct {
		name string
		c    Company
		want bool
	}{
		{"never fetched", Company{}, true},
		{"fresh", Company{FetchedAt: at(time.Hour)}, false},
		{"just under ttl", Company{FetchedAt: at(ttl - time.Second)}, false},
		{"exactly ttl", Company{FetchedAt: at(ttl)}, true},
		{"stale", Company{FetchedAt: at(48 * time.Hour)}, true},
	}
	for _, tc := range cases {
		if got := tc.c.NeedsRefresh(now, ttl); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	for _, in := range []string{"+79990000001", "89990000001", "8 (999) 000-00-01", "9990000001"} {
		if got, ok := NormalizePhone(in); !ok || got != "+79990000001" {
			t.Errorf("%q: got %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "123", "+1999000000199"} {
		if _, ok := NormalizePhone(in); ok {
			t.Errorf("%q: want invalid", in)
		}
	}
}

func TestCanTransition(t *testing.T) {
	allowed := [][2]Status{
		{StatusDraft, StatusSubmitted},
		{StatusSubmitted, StatusDataCheck},
		{StatusDataCheck, StatusScoring},
		{StatusDataCheck, StatusInfoRequested},
		{StatusInfoRequested, StatusDataCheck},
		{StatusScoring, StatusApproved},
		{StatusScoring, StatusCounterOffer},
		{StatusScoring, StatusManualReview},
		{StatusManualReview, StatusRejected},
	}
	for _, p := range allowed {
		if !CanTransition(p[0], p[1]) {
			t.Errorf("%s -> %s must be allowed", p[0], p[1])
		}
	}

	forbidden := [][2]Status{
		{StatusScoring, StatusRejected}, // отказ выносит только сотрудник
		{StatusSubmitted, StatusScoring},
		{StatusApproved, StatusScoring},
		{StatusClosed, StatusActive},
		{StatusDraft, StatusApproved},
	}
	for _, p := range forbidden {
		if CanTransition(p[0], p[1]) {
			t.Errorf("%s -> %s must be forbidden", p[0], p[1])
		}
	}
}

func TestWorkerMayTransition(t *testing.T) {
	allowed := [][2]Status{
		{StatusSubmitted, StatusDataCheck},
		{StatusDataCheck, StatusInfoRequested},
		{StatusDataCheck, StatusScoring},
		{StatusScoring, StatusApproved},
		{StatusScoring, StatusCounterOffer},
		{StatusScoring, StatusManualReview},
	}
	for _, p := range allowed {
		if !WorkerMayTransition(p[0], p[1]) {
			t.Errorf("the worker must be able to move %s -> %s", p[0], p[1])
		}
	}

	forbidden := [][2]Status{
		// решения после ручной проверки принимает сотрудник
		{StatusManualReview, StatusApproved},
		{StatusManualReview, StatusCounterOffer},
		{StatusManualReview, StatusRejected},
		// возврат после ответа клиента и действия клиента — не шаги воркера
		{StatusInfoRequested, StatusDataCheck},
		{StatusApproved, StatusSigning},
		{StatusCounterOffer, StatusDraft},
		{StatusDraft, StatusSubmitted},
	}
	for _, p := range forbidden {
		if WorkerMayTransition(p[0], p[1]) {
			t.Errorf("the worker must not move %s -> %s", p[0], p[1])
		}
	}
}

// Каждый переход воркера должен быть и в общей диаграмме состояний.
func TestWorkerTransitionsAreInTheDiagram(t *testing.T) {
	for from, targets := range workerTransitions {
		for _, to := range targets {
			if !CanTransition(from, to) {
				t.Errorf("worker transition %s -> %s is not in the state diagram", from, to)
			}
		}
	}
}

func TestKnownStatus(t *testing.T) {
	for _, s := range []Status{StatusDraft, StatusScoring, StatusClosed, StatusRejected} {
		if !KnownStatus(s) {
			t.Errorf("%s must be known", s)
		}
	}
	for _, s := range []Status{"", "NOPE", "draft"} {
		if KnownStatus(s) {
			t.Errorf("%q must be unknown", s)
		}
	}
}
