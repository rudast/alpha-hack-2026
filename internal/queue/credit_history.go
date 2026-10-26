// Package queue — обмен API с воркерами через Redis.
//
// Запрос кредитной истории: API кладёт задачу в stream и ждёт ответ в списке
// с уникальным именем (BLPOP). Список, а не pub/sub, чтобы ответ воркера нельзя
// было потерять, если он пришёл раньше, чем API начал ждать. Отвечает Python-воркер
// (worker/, режим WORKER_PROTOCOL=backend), см. docs/worker-protocol.md.
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
	historyStream      = "credit-history:requests"
	historyReplyPrefix = "credit-history:reply:"
	historyMaxLen      = 10000
	defaultWait        = 10 * time.Second
)

// History — то, чего банк сам не знает: собственный капитал компании и её кредиты в других банках.
// Кредиты, выданные нашим банком, API считает сам (service.DebtService). OtherBanks равен nil, если
// воркер прислал ответ старого формата ({"debt", "equity"}): тогда общий долг воркера не используется,
// а кредиты в других банках остаются прежними (из демо-данных).
type History struct {
	Equity     int64                  `json:"equity"`
	OtherBanks []domain.OtherBankLoan `json:"other_banks"`
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
	if deadline, ok := ctx.Deadline(); ok {
		wait = time.Until(deadline)
		if wait <= 0 {
			return History{}, domain.ErrUpstreamTimeout
		}
	}

	id, err := newID()
	if err != nil {
		return History{}, err
	}
	if err := c.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: historyStream,
		MaxLen: historyMaxLen,
		Approx: true,
		Values: map[string]any{"request_id": id, "inn": inn},
	}).Err(); err != nil {
		return History{}, mapTimeout(err)
	}

	values, err := c.rdb.BLPop(ctx, wait, historyReplyPrefix+id).Result()
	if err != nil {
		return History{}, mapTimeout(err)
	}
	var response struct {
		History
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(values[1]), &response); err != nil {
		return History{}, fmt.Errorf("%w: decode reply: %v", domain.ErrUpstreamFailed, err)
	}
	if response.Error != "" {
		return History{}, fmt.Errorf("%w: %s", domain.ErrUpstreamFailed, response.Error)
	}
	return response.History, nil
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
