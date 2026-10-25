# Проверка на открытых образцах

Проверка 25.10.2026. Образцы загружались во временный каталог и не включены в репозиторий. Публикация файла не доказывает его обезличивание, подлинность или пригодность для кредитного решения; результаты ниже проверяют только считывание и извлечение указанных полей.

| Образец | Что проверено | Результат |
|---|---|---|
| [Пример выписки ЕГРН, PDF, 7 страниц](https://dineka.ru/services/doc_realty/egrn/DINEKA_Example_EGRN_basic_inf%20%28land%29.pdf) | Текстовый слой PDF → `egrn_extract` | `TEXT_EXTRACTED`, кадастровый номер `50:20:0020202:375`, `FIELDS_UNVERIFIED` |
| [Тестовая выписка 1С Tochka](https://github.com/tochka-public/odin_palace/blob/master/src/tests/suites/one_document/input.txt) | CP1251 → `account_statement` | `PARSED`, поступления за 2024-04 `67770.00`, конечный остаток `67770.00` |
| [Обезличенная Ozon-фикстура финансовой операции](https://github.com/sotchenkov/ozon_to_google_sheets/blob/main/tests/fixtures/ordinary_operation.json) | JSON → `marketplace_sales_report` | `UNSUPPORTED`: это формат операций API с `accruals` и `last_id`, а не поддерживаемый месячный отчёт с `period`, `sales`, `returns`, `payouts` |

Для отчётов маркетплейсов, бухгалтерских форм, паспортов, согласий и договоров открытых обезличенных файлов с известными ожидаемыми значениями пока не найдено. Тестовые фикстуры не заменяют оценку точности на данных конкретных банков и площадок. Текущий формат результата — [контракт worker](worker-tasks.md), статус всех видов — [матрица покрытия](parser-coverage.md).
