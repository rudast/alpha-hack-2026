// Package queue sends typed requests from the Go API to the Python worker.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rudast/alpha-hack-2026/internal/domain"
)

const (
	requestStream  = "worker:requests"
	replyKeyPrefix = "worker:reply:"
	defaultWait    = 10 * time.Second

	KindCreditHistory      = "credit_history"
	KindVerifyApplication  = "verify_application"
	KindParseDocuments     = "parse_documents"
	KindCriticalRiskReview = "critical_risk_review"
)

// Task is the shared JSON envelope in the Redis Stream field "task".
type Task struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
	Persist bool            `json:"persist,omitempty"`
}

type reply struct {
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

// History is returned by the Python financial verifier.
type History struct {
	Debt   int64 `json:"debt"`
	Equity int64 `json:"equity"`
}

type Client struct{ rdb *redis.Client }

func NewClient(rdb *redis.Client) *Client { return &Client{rdb: rdb} }

// Submit sends a task without waiting. Long tasks need a persisted result and
// a separate polling endpoint before they are exposed to clients.
func (c *Client) Submit(ctx context.Context, kind string, payload any) (string, error) {
	if kind == "" {
		return "", errors.New("worker task kind is required")
	}
	id, err := newID()
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	envelope, err := json.Marshal(Task{ID: id, Kind: kind, Payload: body})
	if err != nil {
		return "", err
	}
	if err := c.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: requestStream,
		Values: map[string]any{"task": string(envelope)},
	}).Err(); err != nil {
		return "", mapTimeout(err)
	}
	return id, nil
}

// PublishStored publishes a job already committed to PostgreSQL. Repeating this
// call is safe because the worker writes results by the same job ID.
func (c *Client) PublishStored(ctx context.Context, id, kind string, payload json.RawMessage) error {
	envelope, err := json.Marshal(Task{ID: id, Kind: kind, Payload: payload, Persist: true})
	if err != nil {
		return err
	}
	return c.rdb.XAdd(ctx, &redis.XAddArgs{Stream: requestStream, Values: map[string]any{"task": string(envelope)}}).Err()
}

// Await consumes one short-lived response. Use this only for fast tasks.
func (c *Client) Await(ctx context.Context, id string, dst any) error {
	wait := defaultWait
	if deadline, ok := ctx.Deadline(); ok {
		wait = time.Until(deadline)
		if wait <= 0 {
			return domain.ErrUpstreamTimeout
		}
	}
	values, err := c.rdb.BLPop(ctx, wait, replyKeyPrefix+id).Result()
	if err != nil {
		return mapTimeout(err)
	}
	var r reply
	if err := json.Unmarshal([]byte(values[1]), &r); err != nil {
		return fmt.Errorf("decode worker reply: %w", err)
	}
	if r.Error != "" {
		return fmt.Errorf("worker: %s", r.Error)
	}
	if len(r.Data) == 0 {
		return errors.New("worker returned an empty result")
	}
	return json.Unmarshal(r.Data, dst)
}

func (c *Client) FetchHistory(ctx context.Context, inn string) (History, error) {
	id, err := c.Submit(ctx, KindCreditHistory, struct {
		INN string `json:"inn"`
	}{INN: inn})
	if err != nil {
		return History{}, err
	}
	var h History
	err = c.Await(ctx, id, &h)
	return h, err
}

func mapTimeout(err error) error {
	if errors.Is(err, redis.Nil) || errors.Is(err, context.DeadlineExceeded) {
		return domain.ErrUpstreamTimeout
	}
	return err
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
