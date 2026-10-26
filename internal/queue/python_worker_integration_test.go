package queue_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rudast/alpha-hack-2026/internal/queue"
)

// Интеграционная проверка с запущенным Python-воркером (WORKER_PROTOCOL=backend):
// REDIS_TEST_ADDR=localhost:6379 go test ./internal/queue/
func TestPythonWorkerCreditHistory(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("set REDIS_TEST_ADDR with a running Python worker")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	history, err := queue.NewClient(rdb).FetchHistory(ctx, "7700000002")
	if err != nil || history.Debt != 2_400_000 || history.Equity != 4_000_000 {
		t.Fatalf("history = %+v, error = %v", history, err)
	}
}
