package queue

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

const (
	applicationsStream = "applications:submitted"
	applicationsMaxLen = 10000
)

// ApplicationPublisher кладёт задачи по заявкам в очередь воркера скоринга.
// Формат сообщения пока черновой и не согласован с воркером, см. docs/worker-protocol.md.
type ApplicationPublisher struct {
	rdb *redis.Client
}

func NewApplicationPublisher(rdb *redis.Client) *ApplicationPublisher {
	return &ApplicationPublisher{rdb: rdb}
}

// Publish добавляет задачу в stream applications:submitted. Форма и документы передаются
// JSON-строками, числа обычными полями; показатели компании только если они известны.
func (p *ApplicationPublisher) Publish(ctx context.Context, t domain.ApplicationTask) error {
	form := []byte("{}")
	if t.Form != nil {
		var err error
		if form, err = json.Marshal(t.Form); err != nil {
			return err
		}
	}
	documents := string(t.Documents)
	if documents == "" {
		documents = "[]"
	}

	company, err := json.Marshal(t.Company)
	if err != nil {
		return err
	}

	values := map[string]any{
		"application_id": t.ApplicationID,
		"inn":            t.INN,
		"product_id":     t.ProductID,
		"amount":         t.Amount,
		"term_months":    t.TermMonths,
		"form":           string(form),
		"documents":      documents,
		"company":        string(company),
	}
	if t.Debt != nil {
		values["debt"] = *t.Debt
	}
	if t.Equity != nil {
		values["equity"] = *t.Equity
	}
	if t.DebtToEquity != nil {
		values["debt_to_equity"] = strconv.FormatFloat(*t.DebtToEquity, 'f', -1, 64)
	}

	return p.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: applicationsStream,
		MaxLen: applicationsMaxLen,
		Approx: true,
		Values: values,
	}).Err()
}
