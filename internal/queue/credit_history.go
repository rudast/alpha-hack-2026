// Package queue — обмен с воркером заявок через Redis.
//
// Запрос кредитной истории: API кладёт задачу в stream и ждёт ответ в списке
// с уникальным именем (BLPOP). Список, а не pub/sub, чтобы ответ воркера нельзя
// было потерять, если он пришёл раньше, чем API начал ждать.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

const (
	requestStream   = "credit-history:requests"
	replyKeyPrefix  = "credit-history:reply:"
	consumerGroup   = "credit-history-workers"
	replyTTL        = time.Minute
	defaultWait     = 10 * time.Second
	requestMaxLen   = 10000
	workerBlockTime = 5 * time.Second

	// Сообщение, висящее без подтверждения дольше reclaimMinIdle, считается брошенным;
	// проверка выполняется не чаще раза в reclaimEvery.
	reclaimMinIdle = 30 * time.Second
	reclaimEvery   = 10 * time.Second
)

// History — кредитная история в виде двух показателей по всем банкам.
type History struct {
	Debt   int64 `json:"debt"`
	Equity int64 `json:"equity"`
}

type reply struct {
	History
	Error string `json:"error,omitempty"`
}

// Client — сторона API.
type Client struct {
	rdb *redis.Client
}

func NewClient(rdb *redis.Client) *Client {
	return &Client{rdb: rdb}
}

// FetchHistory отправляет ИНН воркеру и ждёт ответ до дедлайна ctx.
// Если воркер не ответил вовремя, возвращает domain.ErrUpstreamTimeout.
// Если воркер ответил ошибкой или некорректным JSON, возвращает domain.ErrUpstreamFailed
// (с подробностями в тексте ошибки).
func (c *Client) FetchHistory(ctx context.Context, inn string) (History, error) {
	wait := defaultWait
	if dl, ok := ctx.Deadline(); ok {
		wait = time.Until(dl)
	}

	id, err := newID()
	if err != nil {
		return History{}, err
	}
	err = c.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: requestStream,
		MaxLen: requestMaxLen,
		Approx: true,
		Values: map[string]any{"request_id": id, "inn": inn},
	}).Err()
	if err != nil {
		return History{}, mapTimeout(err)
	}

	res, err := c.rdb.BLPop(ctx, wait, replyKeyPrefix+id).Result()
	if err != nil {
		return History{}, mapTimeout(err)
	}

	var r reply
	if err := json.Unmarshal([]byte(res[1]), &r); err != nil {
		return History{}, fmt.Errorf("%w: decode reply: %v", domain.ErrUpstreamFailed, err)
	}
	if r.Error != "" {
		return History{}, fmt.Errorf("%w: %s", domain.ErrUpstreamFailed, r.Error)
	}
	return r.History, nil
}

func mapTimeout(err error) error {
	if errors.Is(err, redis.Nil) || errors.Is(err, context.DeadlineExceeded) {
		return domain.ErrUpstreamTimeout
	}
	return err
}

// Handler обрабатывает один запрос кредитной истории на стороне воркера.
type Handler func(ctx context.Context, inn string) (History, error)

// Serve читает запросы из stream через consumer group и отвечает на них.
// Сообщение подтверждается (XACK) только после отправки ответа. Сообщения, которые
// кто-то прочитал, но так и не подтвердил (например, воркер упал), периодически
// забираются обратно и обрабатываются заново, см. reclaim.
func Serve(ctx context.Context, rdb *redis.Client, consumer string, handle Handler) error {
	err := rdb.XGroupCreateMkStream(ctx, requestStream, consumerGroup, "0").Err()
	if err != nil && !isBusyGroup(err) {
		return err
	}

	var lastReclaim time.Time
	for {
		if time.Since(lastReclaim) >= reclaimEvery {
			lastReclaim = time.Now()
			if err := reclaim(ctx, rdb, consumer, handle); err != nil {
				return err
			}
		}
		if ctx.Err() != nil {
			return nil
		}

		streams, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    consumerGroup,
			Consumer: consumer,
			Streams:  []string{requestStream, ">"},
			Count:    10,
			Block:    workerBlockTime,
		}).Result()
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return err
		}

		for _, s := range streams {
			for _, msg := range s.Messages {
				if err := answer(ctx, rdb, msg, handle); err != nil {
					return err
				}
			}
		}
	}
}

// reclaim забирает у любого потребителя группы сообщения, которые висят без подтверждения
// дольше reclaimMinIdle (XAUTOCLAIM), и обрабатывает их. Клиент к этому моменту давно получил
// таймаут, поэтому ответ нужен лишь затем, чтобы сообщение не оставалось «зависшим» навсегда.
func reclaim(ctx context.Context, rdb *redis.Client, consumer string, handle Handler) error {
	start := "0-0"
	for {
		msgs, next, err := rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   requestStream,
			Group:    consumerGroup,
			Consumer: consumer,
			MinIdle:  reclaimMinIdle,
			Start:    start,
			Count:    10,
		}).Result()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		for _, msg := range msgs {
			if err := answer(ctx, rdb, msg, handle); err != nil {
				return err
			}
		}
		if next == "" || next == "0-0" {
			return nil
		}
		start = next
	}
}

func answer(ctx context.Context, rdb *redis.Client, msg redis.XMessage, handle Handler) error {
	id, _ := msg.Values["request_id"].(string)
	inn, _ := msg.Values["inn"].(string)

	var r reply
	if id == "" || inn == "" {
		r.Error = "malformed request"
	} else if h, err := handle(ctx, inn); err != nil {
		r.Error = err.Error()
	} else {
		r.History = h
	}

	if id != "" {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		key := replyKeyPrefix + id
		pipe := rdb.TxPipeline()
		pipe.LPush(ctx, key, b)
		pipe.Expire(ctx, key, replyTTL)
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	return rdb.XAck(ctx, requestStream, consumerGroup, msg.ID).Err()
}

func isBusyGroup(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "BUSYGROUP")
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
