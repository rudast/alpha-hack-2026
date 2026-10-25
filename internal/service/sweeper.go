package service

import (
	"context"
	"encoding/json"
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

// newApplicationTask собирает задачу воркеру скоринга: данные заявки и показатели её компании.
// Одну и ту же задачу отправляют и сразу при отправке заявки, и сборщик.
func newApplicationTask(a domain.Application, c domain.Company) domain.ApplicationTask {
	profile := domain.CompanyProfile{
		OrgType:       c.OrgType,
		OKVED:         c.OKVED,
		Registry:      jsonObject(c.Registry),
		CreditHistory: jsonObject(c.CreditHistory),
		Financials:    jsonObject(c.Financials),
	}
	if c.RegistrationDate != nil {
		profile.RegistrationDate = c.RegistrationDate.Format("2006-01-02")
	}
	return domain.ApplicationTask{
		ApplicationID: a.ID,
		INN:           a.CompanyINN,
		ProductID:     a.ProductID,
		Amount:        a.Amount,
		TermMonths:    a.TermMonths,
		Form:          a.Form,
		Documents:     a.Documents,
		Debt:          c.TotalDebt,
		Equity:        c.Equity,
		DebtToEquity:  c.DebtToEquity,
		Company:       profile,
	}
}

// jsonObject возвращает сохранённый JSON-объект или пустой объект, если данных нет.
func jsonObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

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
		company, err := s.companies.GetByINN(ctx, a.CompanyINN)
		if err != nil {
			log.Printf("sweeper: application %s: company: %v", a.ID, err)
			continue
		}

		if err := s.publisher.Publish(ctx, newApplicationTask(a, company)); err != nil {
			log.Printf("sweeper: application %s: publish: %v", a.ID, err)
			continue
		}
		sent++
	}
	return sent, nil
}
