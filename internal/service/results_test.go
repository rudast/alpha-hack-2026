package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type fakeApplier struct {
	applied []domain.ApplicationResult
	err     error
}

func (f *fakeApplier) ApplyResult(_ context.Context, res domain.ApplicationResult, _ time.Time) error {
	if f.err != nil {
		return f.err
	}
	f.applied = append(f.applied, res)
	return nil
}

func result(from, to domain.Status) domain.ApplicationResult {
	return domain.ApplicationResult{EventID: "e1", ApplicationID: "a1", From: from, To: to}
}

func TestProcess_AppliesAValidResult(t *testing.T) {
	applier := &fakeApplier{}
	p := NewResultProcessor(applier)
	res := result(domain.StatusScoring, domain.StatusApproved)
	res.Decision = json.RawMessage(`{"score":82,"outcome":"APPROVED"}`)
	res.Comment = "ok"

	if err := p.Process(context.Background(), res); err != nil {
		t.Fatal(err)
	}
	if len(applier.applied) != 1 || applier.applied[0].To != domain.StatusApproved || string(applier.applied[0].Decision) != `{"score":82,"outcome":"APPROVED"}` {
		t.Fatalf("applied %+v", applier.applied)
	}
}

func TestProcess_RejectsInvalidResultsWithoutTouchingTheDatabase(t *testing.T) {
	cases := map[string]domain.ApplicationResult{
		"worker sets REJECTED":      result(domain.StatusManualReview, domain.StatusRejected),
		"transition not in diagram": result(domain.StatusSubmitted, domain.StatusScoring),
		"unknown from":              result("NOPE", domain.StatusScoring),
		"unknown to":                result(domain.StatusDataCheck, "NOPE"),
		"worker sets SIGNING":       result(domain.StatusApproved, domain.StatusSigning),
		"empty application id":      {From: domain.StatusSubmitted, To: domain.StatusDataCheck},
	}
	bad := result(domain.StatusScoring, domain.StatusApproved)
	bad.Decision = json.RawMessage(`[1,2]`)
	cases["decision is not an object"] = bad
	notJSON := result(domain.StatusScoring, domain.StatusApproved)
	notJSON.Decision = json.RawMessage(`not json`)
	cases["decision is not JSON"] = notJSON

	for name, res := range cases {
		applier := &fakeApplier{}
		// nil означает «обработано» (сообщение подтверждается), в базу при этом ничего не пишется
		if err := NewResultProcessor(applier).Process(context.Background(), res); err != nil {
			t.Errorf("%s: got %v, want nil (acknowledged)", name, err)
		}
		if len(applier.applied) != 0 {
			t.Errorf("%s: an invalid result reached the database", name)
		}
	}
}

func TestProcess_ExpectedOutcomesAreAcknowledged(t *testing.T) {
	for name, applyErr := range map[string]error{
		"already applied (duplicate)": domain.ErrAlreadyApplied,
		"application not found":       domain.ErrNotFound,
		"stale status":                domain.ErrStatusMismatch,
	} {
		p := NewResultProcessor(&fakeApplier{err: applyErr})
		if err := p.Process(context.Background(), result(domain.StatusDataCheck, domain.StatusScoring)); err != nil {
			t.Errorf("%s: got %v, want nil so the message is acknowledged", name, err)
		}
	}
}

func TestProcess_TemporaryFailureIsReturnedSoTheMessageIsRetried(t *testing.T) {
	boom := errors.New("db is down")
	p := NewResultProcessor(&fakeApplier{err: boom})
	if err := p.Process(context.Background(), result(domain.StatusDataCheck, domain.StatusScoring)); !errors.Is(err, boom) {
		t.Fatalf("got %v, want the database error", err)
	}
}
