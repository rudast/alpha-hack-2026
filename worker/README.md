# Python worker

Отдельный сервис обработки задач Go API. Общий вход — Redis Stream `worker:requests`. Маршрутизатор в `router.py` переносит задачи в `worker:fast` или `worker:slow` и выбирает обработчик по `kind`.

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

PDF с текстовым слоем читается через PDFium; для страниц без текста и загруженных изображений запускается OCR. DOCX читается как Word-документ, старый DOC преобразуется в текст через `antiword`. Результат помечается `TEXT_EXTRACTED` и требует проверки: суммы, периоды и другие поля из свободного текста автоматически не устанавливаются. Лимиты: 20 страниц, 12 млн пикселей на OCR-страницу, 30 тыс. символов результата. API принимает файл до 10 МиБ.

Выписки `1CClientBankExchange` в `text/plain` читаются из секций `СекцияРасчСчет`: обороты по месяцам, номер счёта и остатки. Суммы возвращаются строками в рублях с копейками, без округления. Пока поддерживается один счёт в файле; каждая секция должна лежать внутри одного календарного месяца, а секции вместе должны покрывать заявленный период. Файл без итогов секции получает `UNSUPPORTED`; расхождение баланса — `FAILED`. Это не покрывает PDF-выписки банков и все варианты формата 1С.

Форматы входа и ответа каждого маршрута описаны в [контракте](../docs/worker-tasks.md). Go API передаёт файлы в задачах Redis в base64. Worker отправляет результаты в `applications:results`; Go сохраняет их и отдаёт через `GET /api/v1/applications/{id}`.
