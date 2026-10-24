# Python worker

Отдельный сервис обработки задач Go API. `WORKER_PROTOCOL=legacy` (по умолчанию) использует Redis Stream `worker:requests`: маршрутизатор в `router.py` переносит задачи в `worker:fast` или `worker:slow`. Контракт этого режима описан в [worker-tasks.md](../docs/worker-tasks.md).

`WORKER_PROTOCOL=backend` включает контракт ветки `origin/backend`. В нём восемь быстрых потребителей читают `credit-history:requests` и `applications:submitted`; два медленных потребителя читают `worker:backend:slow`. Ответ БКИ идёт в список `credit-history:reply:<request_id>`, а изменения статуса заявки — отдельными полями `event_id`, `application_id`, `from`, `to`, `decision`, `info_request`, `comment` в `applications:results`. Redis-публикация и подтверждение сообщения выполняются одной транзакцией. Оба режима работают без доступа к PostgreSQL.

Режимы запускаются по отдельности: два варианта API используют разные форматы сообщений в одном `applications:results`. При объединении веток выберите `WORKER_PROTOCOL=backend` в `.env` или окружении Compose и выполните `docker compose up -d --build worker`. Старые задания `worker:requests` этот режим не читает.

| Модуль | Класс | Задачи |
|---|---|---|
| `services/financial_verifier.py` | быстрый | `credit_history`, `verify_application` |
| `services/document_parser.py` | длительный | `parse_documents` — CSV-выписки, частично выписки 1С и учебные JSON-отчёты; текст PDF/DOC/DOCX и OCR сканов |
| `services/document_readers.py` | длительный | Ограниченное по объёму извлечение текста из PDF, Word и изображений |
| `services/risk_reviewer.py` | длительный | `critical_risk_review` — заглушка |

Для запуска отдельно от Compose нужны Redis, Python 3.13, `antiword`, `tesseract-ocr` и языковые пакеты `eng`, `rus`. PostgreSQL доступен только Go API:

```bash
python3 -m venv .venv
.venv/bin/pip install -r worker/requirements.txt
REDIS_ADDR=localhost:6379 .venv/bin/python -m worker.main
```

Для нового backend: `WORKER_PROTOCOL=backend REDIS_ADDR=localhost:6379 .venv/bin/python -m worker.main`. В Compose достаточно задать `WORKER_PROTOCOL=backend` перед запуском worker.

Backend сейчас передаёт в `applications:submitted` снимок формы и только метаданные документов. Worker просит паспорт и согласие на БКИ, если их нет, через `INFO_REQUESTED`; перечень недостающего дублируется в `comment`, так как backend пока не сохраняет `info_request`. При наличии метаданных worker переходит через `DATA_CHECK` и `SCORING` к `MANUAL_REVIEW`: без байтов документа нельзя подтвердить его содержание и автоматически одобрить заявку. Если backend позже передаст `content_base64` или `content_text` в `documents`, медленный обработчик использует существующий парсер и вложит непроверенные извлечённые поля в `decision`. `event_id` строится из заявки, идентификатора входного Redis-сообщения и статуса: повтор обработки того же сообщения сохраняет идентификатор, новая отправка получает новый.

Ограничения backend остаются за пределами worker: на существующей БД ему нужна новая миграция для `applications.version` и `applications.enqueued_at`; текущий API не принимает ответ клиента после `INFO_REQUESTED` и не сохраняет `info_request`. При нескольких экземплярах API его consumer group может применить последовательные события одной заявки не по порядку.

PDF с текстовым слоем читается через PDFium; для страниц без текста и загруженных изображений запускается OCR. DOCX читается как Word-документ, старый DOC преобразуется в текст через `antiword`. Результат помечается `TEXT_EXTRACTED` и требует проверки: суммы, периоды и другие поля из свободного текста автоматически не устанавливаются. Лимиты: 20 страниц, 12 млн пикселей на OCR-страницу, 30 тыс. символов результата. API принимает файл до 10 МиБ.

Выписки `1CClientBankExchange` в `text/plain` читаются из секций `СекцияРасчСчет`: обороты по месяцам, номер счёта и остатки. Суммы возвращаются строками в рублях с копейками, без округления. Пока поддерживается один счёт в файле; каждая секция должна лежать внутри одного календарного месяца, а секции вместе должны покрывать заявленный период. Файл без итогов секции получает `UNSUPPORTED`; расхождение баланса — `FAILED`. Это не покрывает PDF-выписки банков и все варианты формата 1С.

В режиме `legacy` Go API передаёт файлы в задачах Redis в base64. Worker отправляет результаты в `applications:results`; Go сохраняет их и отдаёт через `GET /api/v1/applications/{id}`.
