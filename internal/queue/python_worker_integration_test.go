package queue_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rudast/alpha-hack-2026/internal/queue"
)

func TestPythonWorkerContracts(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("set REDIS_TEST_ADDR with a running Python worker")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	client := queue.NewClient(rdb)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	history, err := client.FetchHistory(ctx, "7700000002")
	if err != nil || history.Debt != 2_400_000 || history.Equity != 4_000_000 {
		t.Fatalf("history = %+v, error = %v", history, err)
	}

	debt, equity := int64(7_000_000), int64(2_000_000)
	var verification queue.VerifyApplicationResult
	call(t, ctx, client, queue.KindVerifyApplication, queue.VerifyApplicationRequest{
		Application: queue.ApplicationSnapshot{
			INN: "7700000004", OrgType: "OOO", ProductID: "working_capital",
			Amount: 1_000_000, TermMonths: 12, Purpose: "оборотные средства", BKIConsent: true,
		},
		Company: queue.CompanySnapshot{TotalDebt: &debt, Equity: &equity},
	}, &verification)
	if verification.DebtToEquity == nil || *verification.DebtToEquity != 3.5 || !verification.ManualReviewRequired || len(verification.RiskFlags) != 1 {
		t.Fatalf("verification = %+v", verification)
	}

	var parsed queue.ParseDocumentsResult
	call(t, ctx, client, queue.KindParseDocuments, queue.ParseDocumentsRequest{
		ApplicationID: "application-1",
		Documents: []queue.DocumentRef{{Kind: "financial_statements", Filename: "report.json",
			MediaType: "application/json", ContentText: `{"period":"2025","revenue":1000000,"net_profit":200000,"equity":500000,"liabilities":300000}`}},
	}, &parsed)
	if parsed.Status != "DONE" || len(parsed.Documents) != 1 || parsed.Documents[0].Status != "PARSED" {
		t.Fatalf("parser = %+v", parsed)
	}

	var review queue.CriticalRiskResult
	ratio := 3.5
	call(t, ctx, client, queue.KindCriticalRiskReview, queue.CriticalRiskRequest{
		ApplicationID: "application-1", DebtToEquity: &ratio,
	}, &review)
	if review.RecommendedStatus != "MANUAL_REVIEW" || review.Decision != nil {
		t.Fatalf("review = %+v", review)
	}
}

func call(t *testing.T, ctx context.Context, client *queue.Client, kind string, payload, dst any) {
	t.Helper()
	id, err := client.Submit(ctx, kind, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Await(ctx, id, dst); err != nil {
		t.Fatal(err)
	}
}
