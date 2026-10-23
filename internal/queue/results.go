package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	resultStream = "applications:results"
	resultGroup  = "api-results"
)

// ApplicationEvent is emitted once per application job. event_id equals job_id,
// so a retried Redis message can be applied only once by the API.
type ApplicationEvent struct {
	EventID       string          `json:"event_id"`
	JobID         string          `json:"job_id"`
	ApplicationID string          `json:"application_id"`
	From          string          `json:"from"`
	To            string          `json:"to"`
	Kind          string          `json:"kind"`
	Result        json.RawMessage `json:"result"`
	Error         string          `json:"error,omitempty"`
	Decision      json.RawMessage `json:"decision,omitempty"`
	InfoRequest   json.RawMessage `json:"info_request,omitempty"`
	Comment       string          `json:"comment,omitempty"`
}

func (c *Client) InitResults(ctx context.Context) error {
	err := c.rdb.XGroupCreateMkStream(ctx, resultStream, resultGroup, "0").Err()
	if err != nil && !isBusyGroup(err) {
		return err
	}
	return nil
}

func isBusyGroup(err error) bool {
	return err != nil && len(err.Error()) >= 9 && err.Error()[:9] == "BUSYGROUP"
}

func (c *Client) ReadResult(ctx context.Context, consumer string) (string, ApplicationEvent, error) {
	streams, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: resultGroup, Consumer: consumer, Streams: []string{resultStream, ">"}, Count: 1, Block: time.Second,
	}).Result()
	if errors.Is(err, redis.Nil) {
		claimed, _, claimErr := c.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream: resultStream, Group: resultGroup, Consumer: consumer,
			MinIdle: 15 * time.Minute, Start: "0-0", Count: 1,
		}).Result()
		if claimErr != nil || len(claimed) == 0 {
			return "", ApplicationEvent{}, claimErr
		}
		return decodeResult(claimed[0])
	}
	if err != nil || len(streams) == 0 || len(streams[0].Messages) == 0 {
		return "", ApplicationEvent{}, err
	}
	return decodeResult(streams[0].Messages[0])
}

func decodeResult(message redis.XMessage) (string, ApplicationEvent, error) {
	raw, ok := message.Values["event"].(string)
	if !ok {
		return message.ID, ApplicationEvent{}, errors.New("result event is missing")
	}
	var event ApplicationEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return message.ID, ApplicationEvent{}, fmt.Errorf("decode result event: %w", err)
	}
	return message.ID, event, nil
}

func (c *Client) AckResult(ctx context.Context, id string) error {
	return c.rdb.XAck(ctx, resultStream, resultGroup, id).Err()
}
