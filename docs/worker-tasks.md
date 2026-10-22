# Контракт Go API ↔ Python worker

Go API — единственная внешняя точка входа. Он отправляет задачи в Redis Stream `worker:requests`. Python worker читает их, выбирает сервис по `kind` и переносит сообщение в `worker:fast` или `worker:slow`. Сервисы находятся отдельно в `worker/services/`; маршруты перечислены в `worker/router.py`.

Сообщение Redis содержит поле `task` с JSON:

```json
{"id":"уникальный-id","kind":"credit_history","payload":{"inn":"7700000002"}}
```

Ответ записывается в список `worker:reply:<id>` на 60 секунд. Успех: `{"data":{...}}`; ошибка валидации: `{"error":"..."}`. Go-клиент в `internal/queue` умеет отправить задачу (`Submit`) и дождаться ответа (`Await`). Сейчас HTTP использует только быстрый `credit_history`; остальные задачи можно вызвать через Redis, но для них ещё нет публичных REST-маршрутов.

## Быстрые сервисы: `worker/services/financial_verifier.py`

### `credit_history`

Запрос: `{"inn":"7700000002"}`. Ответ: `{"debt":2400000,"equity":4000000}`.

Этот маршрут нужен для `POST /api/v1/me/bootstrap`: Go API запрашивает данные, если они отсутствуют или старше 12 часов. Python возвращает синтетические значения, совместимые с демонстрационными компаниями из миграции. Отношение долга к капиталу считает и сохраняет Go API. Пустой или неверный ИНН даёт ошибку.

### `verify_application` — предварительная заглушка

```json
{
  "application": {
    "inn": "7700000004", "org_type": "OOO", "product_id": "working-capital",
    "amount": 1000000, "term_months": 12, "purpose": "Пополнение оборотных средств",
    "bki_consent": true
  },
  "company": {"total_debt": 7000000, "equity": 2000000}
}
```

Ответ для этого примера:

```json
{
  "complete": true, "missing_fields": [], "debt_to_equity": 3.5,
  "risk_flags": ["HIGH_DEBT_TO_EQUITY"], "manual_review_required": true,
  "next_status": "SCORING"
}
```

Проверяются поля заявки из [OpenAPI](openapi.yaml), положительные сумма и срок, согласие на БКИ из [сценариев](test-scenarios.md), а также долг и капитал из [модели данных](data-model.md). Если полей нет, `next_status` становится `INFO_REQUESTED`; это рекомендация для будущего API, текущий worker не меняет статус в PostgreSQL. При `equity <= 0` коэффициент `null` и флаг `EQUITY_NON_POSITIVE`. Значение больше 2 даёт флаг риска. Это предварительная проверка, не кредитное решение.

## Длительные сервисы: заглушки интерфейсов

### `parse_documents` — `worker/services/document_parser.py`

Запрос:

```json
{"application_id":"application-1","documents":[{"kind":"financial_report","filename":"report.pdf"}]}
```

Ответ содержит `application_id`, `status: "STUB"`, список документов с `status: "NOT_PARSED"` и пустыми `extracted_fields`. Содержимое файлов пока не читается, текст не извлекается. Нынешняя модель заявки хранит вид и имя файла; способ передачи содержимого ещё нужно определить.

### `critical_risk_review` — `worker/services/risk_reviewer.py`

Запрос: `{"application_id":"application-1","debt_to_equity":3.5}`.

Ответ:

```json
{
  "application_id": "application-1", "status": "STUB",
  "risk_flags": ["HIGH_DEBT_TO_EQUITY"],
  "recommended_status": "MANUAL_REVIEW", "decision": null,
  "reason": "full scoring and external checks are not connected yet"
}
```

При коэффициенте не больше 2 рекомендация остаётся `SCORING`; при неопределённом коэффициенте — `MANUAL_REVIEW`. Это не отказ. По [диаграмме состояний](state-diagram.md) отказ выносит только сотрудник. Позже здесь можно подключить парсинг, внешние проверки и LLM, сохранив рекомендацию и её причины отдельно от решения сотрудника.

## Границы текущего этапа

- Новые три маршрута worker возвращают синтетические или явно помеченные заглушки; REST API заявок их пока не вызывает. В [OpenAPI](openapi.yaml) ещё нет полей согласия и документов, хотя сценарии их требуют.
- Длительные задачи нельзя удерживать в HTTP-запросе. Нужны сохранённые статус и результат задачи, способ их чтения клиентом и повторная обработка после сбоя.
- Redis consumer groups оставляют неподтверждённые сообщения в pending. Автоматическое восстановление через `XAUTOCLAIM` пока не реализовано.
- Старые сообщения в `credit-history:requests` не переносятся автоматически в новый поток `worker:requests`.
