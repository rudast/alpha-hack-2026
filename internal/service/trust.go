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
	UpdateExternal(ctx context.Context, inn string, equity int64, otherBanks []domain.OtherBankLoan, fetchedAt time.Time) error
}

// DebtRecalculator пересчитывает общий долг компании (наши кредиты + кредиты в других банках).
type DebtRecalculator interface {
	Recalculate(ctx context.Context, inn string) (domain.Company, error)
}

type CreditHistoryProvider interface {
	FetchHistory(ctx context.Context, inn string) (queue.History, error)
}

// TrustService поддерживает актуальными показатели долговой нагрузки компании
// (общий долг, капитал, их отношение). Используется всеми сценариями, которым нужен коэффициент.
type TrustService struct {
	companies CompanyRepository
	history   CreditHistoryProvider
	debts     DebtRecalculator
	ttl       time.Duration
	timeout   time.Duration
	now       func() time.Time
	inflight  singleflight.Group
}

func NewTrustService(c CompanyRepository, h CreditHistoryProvider, debts DebtRecalculator, ttl, timeout time.Duration) *TrustService {
	return &TrustService{companies: c, history: h, debts: debts, ttl: ttl, timeout: timeout, now: time.Now}
}

// EnsureTrustRatio возвращает компанию с актуальным коэффициентом. Если данных нет
// или они старше ttl, запрашивает кредитную историю у воркера и ждёт ответа не дольше timeout.
//
// Воркер считается недоступным, если он не ответил вовремя (domain.ErrUpstreamTimeout)
// или ответил ошибкой (domain.ErrUpstreamFailed). Если данных ещё не было, эта ошибка
// возвращается вызывающему. Если данные есть, но устарели, возвращаются прежние значения.
func (s *TrustService) EnsureTrustRatio(ctx context.Context, inn string) (domain.Company, error) {
	company, err := s.companies.GetByINN(ctx, inn)
	if err != nil {
		return domain.Company{}, err
	}
	if !company.NeedsRefresh(s.now(), s.ttl) {
		// Внешние данные свежие, но наши кредиты могли измениться (выдача, погашение): долг
		// пересчитывается каждый раз, это дёшево и не даёт ему разойтись с кредитами.
		return s.debts.Recalculate(ctx, inn)
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
		return nil, s.companies.UpdateExternal(fetchCtx, inn, h.Equity, h.OtherBanks, s.now())
	})
	if err != nil {
		if company.FetchedAt != nil && isUpstreamUnavailable(err) {
			log.Printf("trust ratio for %s is stale, using previous values: %v", inn, err)
			// Внешние данные прежние, но наши кредиты учитываются актуальными.
			return s.debts.Recalculate(ctx, inn)
		}
		return domain.Company{}, err
	}
	// Общий долг = наши кредиты (их банк знает сам) + кредиты в других банках из ответа воркера.
	return s.debts.Recalculate(ctx, inn)
}

func isUpstreamUnavailable(err error) bool {
	return errors.Is(err, domain.ErrUpstreamTimeout) || errors.Is(err, domain.ErrUpstreamFailed)
}
