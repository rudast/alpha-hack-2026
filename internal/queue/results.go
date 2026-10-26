package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

const (
	resultsStream = "applications:results"
	resultsGroup  = "api"

	// Пауза после сбоя Redis, чтобы не крутить цикл вхолостую.
	resultsErrorBackoff = 2 * time.Second
	reclaimEvery        = 10 * time.Second
	reclaimMinIdle      = 30 * time.Second
	workerBlockTime     = 5 * time.Second
)

func isBusyGroup(err error) bool { return err != nil && strings.HasPrefix(err.Error(), "BUSYGROUP") }

// ResultHandler применяет один результат. Ошибка означает «временный сбой, повторить позже»:
// сообщение остаётся неподтверждённым. nil означает «сообщение обработано» (в том числе отклонено как
// некорректное): оно подтверждается.
type ResultHandler func(ctx context.Context, r domain.ApplicationResult) error

// ResultConsumer читает результаты воркера скоринга из stream applications:results через группу api.
// Формат сообщения пока черновой и не согласован с воркером, см. docs/worker-protocol.md.
type ResultConsumer struct {
	rdb *redis.Client
}

func NewResultConsumer(rdb *redis.Client) *ResultConsumer {
	return &ResultConsumer{rdb: rdb}
}

// Serve работает, пока не отменён ctx. Сбои Redis не останавливают его: после паузы цикл продолжается.
// Несколько экземпляров API с разными именами consumer делят сообщения группы между собой.
func (c *ResultConsumer) Serve(ctx context.Context, consumer string, handle ResultHandler) {
	groupReady := false
	var lastReclaim time.Time

	for ctx.Err() == nil {
		if !groupReady {
			// Позиция 0: результаты, пришедшие, пока API был выключен, не теряются.
			err := c.rdb.XGroupCreateMkStream(ctx, resultsStream, resultsGroup, "0").Err()
			if err != nil && !isBusyGroup(err) {
				c.pause(ctx, err)
				continue
			}
			groupReady = true
		}

		if time.Since(lastReclaim) >= reclaimEvery {
			lastReclaim = time.Now()
			if err := c.reclaim(ctx, consumer, handle); err != nil {
				c.pause(ctx, err)
				continue
			}
		}

		streams, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    resultsGroup,
			Consumer: consumer,
			Streams:  []string{resultsStream, ">"},
			Count:    10,
			Block:    workerBlockTime,
		}).Result()
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, redis.Nil) {
			continue // за время ожидания новых сообщений не пришло
		}
		if err != nil {
			c.pause(ctx, err)
			continue
		}
		for _, s := range streams {
			for _, msg := range s.Messages {
				c.handleMessage(ctx, msg, handle)
			}
		}
	}
}

// reclaim забирает сообщения, которые кто-то прочитал, но не подтвердил (например, при сбое записи
// в базу или падении процесса), и обрабатывает их заново.
func (c *ResultConsumer) reclaim(ctx context.Context, consumer string, handle ResultHandler) error {
	start := "0-0"
	for {
		msgs, next, err := c.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   resultsStream,
			Group:    resultsGroup,
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
			c.handleMessage(ctx, msg, handle)
		}
		if next == "" || next == "0-0" {
			return nil
		}
		start = next
	}
}

func (c *ResultConsumer) handleMessage(ctx context.Context, msg redis.XMessage, handle ResultHandler) {
	res, err := parseResult(msg.Values)
	if err != nil {
		// Сообщение некорректно, повтор не поможет: фиксируем в логе и подтверждаем.
		log.Printf("results: message %s dropped: %v", msg.ID, err)
		c.ack(ctx, msg.ID)
		return
	}
	if err := handle(ctx, res); err != nil {
		log.Printf("results: event %q for application %s will be retried: %v", res.EventID, res.ApplicationID, err)
		return
	}
	c.ack(ctx, msg.ID)
}

func (c *ResultConsumer) ack(ctx context.Context, id string) {
	if err := c.rdb.XAck(ctx, resultsStream, resultsGroup, id).Err(); err != nil {
		log.Printf("results: cannot ack %s: %v", id, err)
	}
}

func (c *ResultConsumer) pause(ctx context.Context, err error) {
	log.Printf("results: redis error: %v", err)
	select {
	case <-ctx.Done():
	case <-time.After(resultsErrorBackoff):
	}
}

// parseResult собирает результат из полей сообщения. Обязательны application_id, from и to;
// значения в Redis приходят строками.
func parseResult(values map[string]any) (domain.ApplicationResult, error) {
	text := func(key string) string {
		s, _ := values[key].(string)
		return s
	}
	res := domain.ApplicationResult{
		EventID:       text("event_id"),
		ApplicationID: text("application_id"),
		From:          domain.Status(text("from")),
		To:            domain.Status(text("to")),
		Comment:       text("comment"),
	}
	for name, v := range map[string]string{"application_id": res.ApplicationID, "from": string(res.From), "to": string(res.To)} {
		if v == "" {
			return domain.ApplicationResult{}, fmt.Errorf("field %q is missing", name)
		}
	}
	if d := text("decision"); d != "" {
		res.Decision = json.RawMessage(d)
	}
	return res, nil
}
