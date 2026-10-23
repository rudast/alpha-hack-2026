package service

import (
	"context"
	"log"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type StaleApplicationClaimer interface {
	ClaimStaleSubmitted(ctx context.Context, olderThan time.Duration, limit int) ([]domain.Application, error)
}

type ApplicationPublisher interface {
	Publish(ctx context.Context, t domain.ApplicationTask) error
}

const sweepBatch = 100

// ApplicationSweeper («сборщик») страхует отправку заявок. Заявка записывается в Postgres, а задача
// воркеру кладётся в Redis: это два хранилища, и между ними возможен сбой, после которого заявка
// остаётся в SUBMITTED, а воркер о ней не знает. Сборщик периодически находит такие заявки
// (статус не менялся дольше olderThan: воркер первым же сообщением сменил бы статус) и отправляет
// задачу заново. Повторная отправка безопасна: воркер идемпотентен по application_id.
type ApplicationSweeper struct {
	apps      StaleApplicationClaimer
	companies CompanyGetter
	publisher ApplicationPublisher
	interval  time.Duration
	olderThan time.Duration
}

func NewApplicationSweeper(apps StaleApplicationClaimer, companies CompanyGetter, publisher ApplicationPublisher, interval, olderThan time.Duration) *ApplicationSweeper {
	return &ApplicationSweeper{apps: apps, companies: companies, publisher: publisher, interval: interval, olderThan: olderThan}
}

// Run проверяет заявки раз в interval, пока не отменён ctx.
func (s *ApplicationSweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		sent, err := s.Sweep(ctx)
		switch {
		case err != nil:
			log.Printf("sweeper: %v", err)
		case sent > 0:
			log.Printf("sweeper: re-sent %d stuck applications", sent)
		}
	}
}

// Sweep выполняет одну проверку и возвращает, сколько задач отправлено. Если по какой-то заявке
// не удалось собрать или отправить задачу, ошибка пишется в лог, остальные заявки обрабатываются дальше,
// а эта будет взята снова после olderThan (её уже пометили как отправленную).
func (s *ApplicationSweeper) Sweep(ctx context.Context) (int, error) {
	stuck, err := s.apps.ClaimStaleSubmitted(ctx, s.olderThan, sweepBatch)
	if err != nil {
		return 0, err
	}

	sent := 0
	for _, a := range stuck {
		task := domain.ApplicationTask{
			ApplicationID: a.ID,
			INN:           a.CompanyINN,
			ProductID:     a.ProductID,
			Amount:        a.Amount,
			TermMonths:    a.TermMonths,
			Form:          a.Form,
			Documents:     a.Documents,
		}
		company, err := s.companies.GetByINN(ctx, a.CompanyINN)
		if err != nil {
			log.Printf("sweeper: application %s: company: %v", a.ID, err)
			continue
		}
		task.Debt, task.Equity, task.DebtToEquity = company.TotalDebt, company.Equity, company.DebtToEquity

		if err := s.publisher.Publish(ctx, task); err != nil {
			log.Printf("sweeper: application %s: publish: %v", a.ID, err)
			continue
		}
		sent++
	}
	return sent, nil
}
