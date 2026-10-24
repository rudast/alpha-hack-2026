# Контракт Go API ↔ Python worker

Go API — единственная внешняя точка входа. Он отправляет задачи в Redis Stream `worker:requests`. Python worker читает их, выбирает сервис по `kind` и переносит сообщение в `worker:fast` или `worker:slow`. Сервисы находятся отдельно в `worker/services/`; маршруты перечислены в `worker/router.py`.

Сообщение Redis содержит поле `task` с JSON:

```json
{"id":"уникальный-id","kind":"credit_history","payload":{"inn":"7700000002"}}
```

Ответ записывается в список `worker:reply:<id>` на 60 секунд. Успех: `{"data":{...}}`; ошибка валидации: `{"error":"..."}`. Go-клиент в `internal/queue` умеет отправить задачу (`Submit`) и дождаться ответа (`Await`). Для заявок Go сначала сохраняет задание в `worker_jobs`, затем публикует его с `persist: true`. Worker публикует событие в Redis Stream `applications:results`; Go API сохраняет результат в PostgreSQL. `GET /api/v1/applications/{id}` возвращает статусы и результаты заданий.

Событие результата содержит `event_id` (совпадает с `job_id` для защиты от повторов), `job_id`, `application_id`, `from`, `to`, `kind`, `result`, `error`, `comment`. Предварительная проверка также может заполнить `decision` с показателями риска и `info_request` с недостающими полями. Сейчас каждое событие сообщает переход `SUBMITTED → DATA_CHECK` по исходному снимку заявки; Go применяет его при первом результате, а после получения всех трёх результатов выбирает итоговый статус `INFO_REQUESTED`, `SCORING` или `MANUAL_REVIEW`. Redis consumer group `api-results` подтверждает событие после транзакции PostgreSQL.

Пример события предварительной проверки:

```json
{"event_id":"job-uuid","job_id":"job-uuid","application_id":"application-uuid","from":"SUBMITTED","to":"DATA_CHECK","kind":"verify_application","result":{"next_status":"INFO_REQUESTED","missing_fields":["bki_consent"]},"decision":{"debt_to_equity":null,"risk_flags":[]},"info_request":{"missing_fields":["bki_consent"]},"error":null,"comment":"verify_application completed"}
```

## Быстрые сервисы: `worker/services/financial_verifier.py`

### `credit_history`

Запрос: `{"inn":"7700000002"}`. Ответ: `{"debt":2400000,"equity":4000000}`.

Этот маршрут нужен для `POST /api/v1/me/bootstrap`: Go API запрашивает данные, если они отсутствуют или старше 12 часов. Python возвращает синтетические значения, совместимые с демонстрационными компаниями из миграции. Отношение долга к капиталу считает и сохраняет Go API. Пустой или неверный ИНН даёт ошибку.

### `verify_application` — предварительная проверка

```json
{
  "application": {
    "inn": "7700000004", "org_type": "OOO", "product_id": "working_capital",
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

## Длительные сервисы

### `parse_documents` — `worker/services/document_parser.py`

Запрос:

```json
{"application_id":"application-uuid","documents":[{"id":"document-uuid","kind":"financial_statements","filename":"report.json","media_type":"application/json","content_base64":"eyJwZXJpb2QiOiIyMDI1In0="}]}
```

Go API берёт содержимое документов из `application_documents` и передаёт его в задаче через `content_base64`. Worker не подключается к PostgreSQL. Поддержаны CSV-выписки с явными колонками даты, поступления, списания и остатка (английские или русские заголовки, рубли с копейками, проверка последовательности остатков), универсальные CSV-отчёты маркетплейса с месяцем, продажами, возвратами и выплатами, частично текстовый формат `1CClientBankExchange` и учебные JSON-файлы видов `financial_statements`, `target_financial_statements`, `marketplace_sales_report`, `valuation_report`. Для 1С поддерживается один счёт; каждая секция `СекцияРасчСчет` должна лежать внутри одного месяца, а секции вместе должны покрывать заявленный период; суммы сохраняются строками в рублях с копейками. Формат описан в [официальной спецификации 1С](https://v8.1c.ru/tekhnologii/obmen-dannymi-i-integratsiya/standarty-i-formaty/standart-obmena-s-sistemami-klient-banka/formaty-obmena/). JSON содержит `period` и соответствующие числовые поля: `revenue,net_profit,equity,liabilities`; `sales,returns,payouts`; либо `appraised_value,asset_id,owner,rights,encumbrances`. Каждое извлечённое поле содержит `value`, `source_document_id`, `period`, `verification_status: "UNVERIFIED"`. Цена, заявленная клиентом в `form.collateral.value`, остаётся отдельной от стоимости в отчёте об оценке. Для PDF, DOC, DOCX, TXT, PNG, JPEG и TIFF извлекается текст: PDFium читает текстовый слой, Tesseract распознаёт страницы без текста и изображения, `python-docx` читает DOCX, `antiword` переводит старый DOC в текст. Такой документ получает `TEXT_EXTRACTED`; поле `document_text` содержит метод извлечения и признак усечения. Из свободного текста финансовые показатели пока не извлекаются, период остаётся `null`, общий результат требует проверки. Неверный файл получает `FAILED`; отказ по кредиту из этого не следует. Go API сохраняет результаты в `application_documents.extracted` и `worker_jobs.result`.

`parse_documents` возвращает `review_reasons` по каждому документу, который ещё не прошёл проверку: `PASSPORT_NOT_VERIFIED`, `BKI_CONSENT_NOT_VERIFIED`, `FIELDS_NOT_EXTRACTED`, `FORMAT_UNSUPPORTED` или `DOCUMENT_INVALID`. Извлечение текста из паспорта или согласия не подтверждает личность или действительность согласия. Такие заявки остаются на ручной проверке до появления специализированных валидаторов. Подробная [матрица покрытия](parser-coverage.md) показывает статус всех 17 видов документов.

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

- `credit_history` и проверка критического риска пока используют синтетические показатели и предварительные правила. `verify_application` не принимает кредитное решение.
- Go хранит задания до публикации и повторяет отправку каждые 5 секунд. Worker забирает зависшие Redis-сообщения через `XAUTOCLAIM` после 15 минут простоя с учётом длительного OCR. Обработка должна оставаться идемпотентной.
- Согласие на БКИ сейчас определяется наличием загруженного документа вида `bki_consent`; проверка субъекта и подписи ещё не реализована.
- Старые сообщения в `credit-history:requests` не переносятся автоматически в новый поток `worker:requests`.
