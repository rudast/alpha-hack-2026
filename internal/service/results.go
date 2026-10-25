package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type ResultApplier interface {
	ApplyResult(ctx context.Context, res domain.ApplicationResult, at time.Time) error
}

// ResultProcessor применяет результаты воркера скоринга к заявкам. Статусы и решение в Postgres
// пишет только API: воркер присылает результат через Redis, а допустимость перехода проверяется здесь.
type ResultProcessor struct {
	apps ResultApplier
	now  func() time.Time
}

func NewResultProcessor(apps ResultApplier) *ResultProcessor {
	return &ResultProcessor{apps: apps, now: time.Now}
}

// Process обрабатывает один результат. Возвращает ошибку только при временном сбое (например, недоступна база):
// тогда сообщение не подтверждается и будет обработано позже. Результат, который повтором не исправить
// (некорректный, для неизвестной заявки, с устаревшим статусом, уже применённый), записывается в лог
// и считается обработанным.
func (p *ResultProcessor) Process(ctx context.Context, res domain.ApplicationResult) error {
	if err := validateResult(res); err != nil {
		log.Printf("results: event %q for application %s rejected: %v", res.EventID, res.ApplicationID, err)
		return nil
	}

	err := p.apps.ApplyResult(ctx, res, p.now())
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrAlreadyApplied):
		log.Printf("results: event %q for application %s is a duplicate (already %s)", res.EventID, res.ApplicationID, res.To)
		return nil
	case errors.Is(err, domain.ErrNotFound):
		log.Printf("results: event %q: application %s not found", res.EventID, res.ApplicationID)
		return nil
	case errors.Is(err, domain.ErrStatusMismatch):
		log.Printf("results: event %q for application %s skipped: its status is not %s", res.EventID, res.ApplicationID, res.From)
		return nil
	}
	return err
}

// validateResult проверяет то, что можно проверить без обращения к базе: переход допустим по диаграмме
// состояний, воркеру разрешён целевой статус, решение (если есть) это JSON-объект.
func validateResult(res domain.ApplicationResult) error {
	switch {
	case res.ApplicationID == "":
		return errors.New("application_id is empty")
	case !domain.KnownStatus(res.From):
		return fmt.Errorf("unknown status %q", res.From)
	case !domain.KnownStatus(res.To):
		return fmt.Errorf("unknown status %q", res.To)
	case !domain.WorkerMaySet(res.To):
		return fmt.Errorf("the worker may not set status %s", res.To)
	case !domain.CanTransition(res.From, res.To):
		return fmt.Errorf("transition %s to %s is not allowed", res.From, res.To)
	}
	if len(res.Decision) > 0 {
		var decision map[string]any
		if err := json.Unmarshal(res.Decision, &decision); err != nil || decision == nil {
			return errors.New("decision is not a JSON object")
		}
	}
	return nil
}
