// Package service — бизнес-логика: вход и обновление показателей долговой нагрузки.
package service

import (
	"context"
	"errors"
	"log"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/queue"
)

type CompanyRepository interface {
	GetByINN(ctx context.Context, inn string) (domain.Company, error)
	UpdateFinancials(ctx context.Context, inn string, debt, equity int64, fetchedAt time.Time) error
}

type CreditHistoryProvider interface {
	FetchHistory(ctx context.Context, inn string) (queue.History, error)
}

// TrustService поддерживает актуальными показатели долговой нагрузки компании
// (общий долг, капитал, их отношение). Используется всеми сценариями, которым нужен коэффициент.
type TrustService struct {
	companies CompanyRepository
	history   CreditHistoryProvider
	ttl       time.Duration
	timeout   time.Duration
	now       func() time.Time
	inflight  singleflight.Group
}

func NewTrustService(c CompanyRepository, h CreditHistoryProvider, ttl, timeout time.Duration) *TrustService {
	return &TrustService{companies: c, history: h, ttl: ttl, timeout: timeout, now: time.Now}
}

// EnsureTrustRatio возвращает компанию с актуальным коэффициентом. Если данных нет
// или они старше ttl, запрашивает кредитную историю у воркера и ждёт ответа не дольше timeout.
//
// Если данных ещё не было и воркер не ответил, возвращает domain.ErrUpstreamTimeout.
// Если данные есть, но устарели, а воркер недоступен, возвращает прежние значения.
func (s *TrustService) EnsureTrustRatio(ctx context.Context, inn string) (domain.Company, error) {
	company, err := s.companies.GetByINN(ctx, inn)
	if err != nil {
		return domain.Company{}, err
	}
	if !company.NeedsRefresh(s.now(), s.ttl) {
		return company, nil
	}

	// Одновременные входы одной компании делят один запрос к воркеру.
	// Запись идемпотентна, так что при нескольких экземплярах API дубликаты безвредны.
	_, err, _ = s.inflight.Do(inn, func() (any, error) {
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout)
		defer cancel()

		h, err := s.history.FetchHistory(fetchCtx, inn)
		if err != nil {
			return nil, err
		}
		return nil, s.companies.UpdateFinancials(fetchCtx, inn, h.Debt, h.Equity, s.now())
	})
	if err != nil {
		if company.FetchedAt != nil && errors.Is(err, domain.ErrUpstreamTimeout) {
			log.Printf("trust ratio for %s is stale, worker did not answer: %v", inn, err)
			return company, nil
		}
		return domain.Company{}, err
	}
	return s.companies.GetByINN(ctx, inn)
}
