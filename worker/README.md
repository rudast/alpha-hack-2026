# Python worker

Отдельный сервис обработки задач Go API. Общий вход — Redis Stream `worker:requests`. Маршрутизатор в `router.py` переносит задачи в `worker:fast` или `worker:slow` и выбирает обработчик по `kind`.

| Модуль | Класс | Задачи |
|---|---|---|
| `services/financial_verifier.py` | быстрый | `credit_history`, `verify_application` |
| `services/document_parser.py` | длительный | `parse_documents` — заглушка |
| `services/risk_reviewer.py` | длительный | `critical_risk_review` — заглушка |

Для запуска отдельно от Compose нужны Redis и Python 3.13:

```bash
python3 -m venv .venv
.venv/bin/pip install -r worker/requirements.txt
REDIS_ADDR=localhost:6379 .venv/bin/python -m worker.main
```

Тесты без Redis: `python3 -m unittest discover -s worker/tests`.

Форматы входа и ответа каждого маршрута описаны в [контракте](../docs/worker-tasks.md). Сейчас только `credit_history` вызывается из REST API. Остальные маршруты готовы для подключения к будущему API заявок.
