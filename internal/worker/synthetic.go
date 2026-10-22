// Package worker — мок обработчика заявок: отдаёт синтетическую кредитную историю по ИНН.
package worker

import (
	"context"
	"hash/fnv"

	"github.com/rudast/alpha-hack-2026/internal/queue"
)

// Для демонстрационных компаний из миграций значения заданы явно.
// У 7700000003 долг вырос по сравнению с базой: так виден эффект обновления.
var known = map[string]queue.History{
	"7700000001":   {Debt: 3_000_000, Equity: 5_000_000},
	"7700000002":   {Debt: 2_400_000, Equity: 4_000_000},
	"7700000003":   {Debt: 1_800_000, Equity: 3_000_000},
	"7700000004":   {Debt: 7_000_000, Equity: 2_000_000},
	"770000000005": {Debt: 800_000, Equity: 2_000_000},
}

// Handle возвращает историю компании; для неизвестных ИНН значения выводятся из хеша ИНН,
// поэтому для одного и того же ИНН ответ всегда одинаков.
func Handle(_ context.Context, inn string) (queue.History, error) {
	if h, ok := known[inn]; ok {
		return h, nil
	}
	f := fnv.New32a()
	_, _ = f.Write([]byte(inn))
	v := int64(f.Sum32())
	return queue.History{
		Debt:   1_000_000 + v%5_000_000,
		Equity: 2_000_000 + (v/7)%6_000_000,
	}, nil
}
