package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

// AuthStore хранит в Redis сессии, ожидающие ввода кода 2FA и счётчики неудачных входов.
// Всё это временные данные: они сами истекают по TTL.
type AuthStore struct {
	rdb *redis.Client
}

func NewAuthStore(rdb *redis.Client) *AuthStore {
	return &AuthStore{rdb: rdb}
}

func (s *AuthStore) CreateSession(ctx context.Context, token string, p domain.Principal, ttl time.Duration) error {
	return s.setJSON(ctx, "session:"+token, p, ttl)
}

func (s *AuthStore) GetSession(ctx context.Context, token string) (domain.Principal, error) {
	var p domain.Principal
	if err := s.getJSON(ctx, "session:"+token, &p); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Principal{}, domain.ErrUnauthorized
		}
		return domain.Principal{}, err
	}
	return p, nil
}

func (s *AuthStore) PutChallenge(ctx context.Context, id string, ch domain.Challenge, ttl time.Duration) error {
	return s.setJSON(ctx, "challenge:"+id, ch, ttl)
}

func (s *AuthStore) GetChallenge(ctx context.Context, id string) (domain.Challenge, error) {
	var ch domain.Challenge
	if err := s.getJSON(ctx, "challenge:"+id, &ch); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Challenge{}, domain.ErrInvalidChallenge
		}
		return domain.Challenge{}, err
	}
	return ch, nil
}

func (s *AuthStore) RecordChallengeAttempt(ctx context.Context, id string, ttl time.Duration) (int, error) {
	return s.incr(ctx, "challenge_attempts:"+id, ttl)
}

func (s *AuthStore) DeleteChallenge(ctx context.Context, id string) error {
	return s.rdb.Del(ctx, "challenge:"+id, "challenge_attempts:"+id).Err()
}

func (s *AuthStore) LoginFailures(ctx context.Context, key string) (int, error) {
	v, err := s.rdb.Get(ctx, "login_fail:"+key).Result()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(v)
}

// RecordLoginFailure увеличивает счётчик; окно блокировки отсчитывается от первой неудачи.
func (s *AuthStore) RecordLoginFailure(ctx context.Context, key string, window time.Duration) (int, error) {
	return s.incr(ctx, "login_fail:"+key, window)
}

func (s *AuthStore) ResetLoginFailures(ctx context.Context, key string) error {
	return s.rdb.Del(ctx, "login_fail:"+key).Err()
}

func (s *AuthStore) incr(ctx context.Context, key string, ttl time.Duration) (int, error) {
	n, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		if err := s.rdb.Expire(ctx, key, ttl).Err(); err != nil {
			return 0, err
		}
	}
	return int(n), nil
}

func (s *AuthStore) setJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, key, b, ttl).Err()
}

func (s *AuthStore) getJSON(ctx context.Context, key string, dst any) error {
	b, err := s.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}
