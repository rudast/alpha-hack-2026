package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

const (
	// Запрос, который начали, но не закончили (например, упал процесс), перестаёт блокировать ключ через минуту.
	idempotencyInProgressTTL = time.Minute
	// Готовый ответ хранится сутки: за это время клиент может безопасно повторить запрос.
	idempotencyResultTTL = 24 * time.Hour
)

// IdempotencyStore хранит в Redis ключи идемпотентности: по ключу видно, выполнялся ли запрос,
// выполняется ли он сейчас и каким был ответ.
type IdempotencyStore struct {
	rdb *redis.Client
}

func NewIdempotencyStore(rdb *redis.Client) *IdempotencyStore {
	return &IdempotencyStore{rdb: rdb}
}

type idempotencyRecord struct {
	State       string                 `json:"state"` // "in_progress" или "done"
	Fingerprint string                 `json:"fingerprint"`
	Response    *domain.StoredResponse `json:"response,omitempty"`
}

func idempotencyKey(scope, key string) string { return "idempotency:" + scope + ":" + key }

// Begin пытается занять ключ (SET NX). Если он уже занят, сообщает, чем: запрос выполняется,
// выполнен (и тогда возвращает сохранённый ответ) или ключ использован для другого запроса.
func (s *IdempotencyStore) Begin(ctx context.Context, scope, key, fingerprint string) (domain.IdempotencyOutcome, domain.StoredResponse, error) {
	k := idempotencyKey(scope, key)
	started, err := json.Marshal(idempotencyRecord{State: "in_progress", Fingerprint: fingerprint})
	if err != nil {
		return 0, domain.StoredResponse{}, err
	}

	// Две попытки: если ключ истёк между SET NX и GET, занимаем его заново.
	for range 2 {
		acquired, err := s.rdb.SetNX(ctx, k, started, idempotencyInProgressTTL).Result()
		if err != nil {
			return 0, domain.StoredResponse{}, err
		}
		if acquired {
			return domain.IdempotencyAcquired, domain.StoredResponse{}, nil
		}

		raw, err := s.rdb.Get(ctx, k).Bytes()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return 0, domain.StoredResponse{}, err
		}
		var current idempotencyRecord
		if err := json.Unmarshal(raw, &current); err != nil {
			return 0, domain.StoredResponse{}, err
		}
		switch {
		case current.Fingerprint != fingerprint:
			return domain.IdempotencyMismatch, domain.StoredResponse{}, nil
		case current.State == "done" && current.Response != nil:
			return domain.IdempotencyReplay, *current.Response, nil
		default:
			return domain.IdempotencyInProgress, domain.StoredResponse{}, nil
		}
	}
	return 0, domain.StoredResponse{}, fmt.Errorf("idempotency key %q is unstable", key)
}

// Finish сохраняет ответ под ключом на сутки.
func (s *IdempotencyStore) Finish(ctx context.Context, scope, key, fingerprint string, resp domain.StoredResponse) error {
	b, err := json.Marshal(idempotencyRecord{State: "done", Fingerprint: fingerprint, Response: &resp})
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, idempotencyKey(scope, key), b, idempotencyResultTTL).Err()
}

// Abort освобождает ключ, если запрос закончился ошибкой сервера: повтор должен выполниться заново.
func (s *IdempotencyStore) Abort(ctx context.Context, scope, key string) error {
	return s.rdb.Del(ctx, idempotencyKey(scope, key)).Err()
}
