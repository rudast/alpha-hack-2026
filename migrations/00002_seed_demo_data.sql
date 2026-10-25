-- +goose Up
-- Синтетические демонстрационные данные. Пароль всех учётных записей: demo-password.
-- Показатели долговой нагрузки заданы так, чтобы были видны все пути обновления:
--   7700000001, 7700000004, 770000000005 — свежие, воркер не вызывается;
--   7700000002 — ни разу не запрашивались;
--   7700000003 — устарели (2 дня).
-- Должность, расчётный счёт, расшифровка ОКВЭД, кредиты и долги в других банках совпадают с демо-данными
-- кабинета (web/assets/js/mock-api.js, BACKEND_TASKS.md). Долг в других банках плюс остатки наших
-- действующих кредитов дают total_debt (у 7700000002 и 7700000003 — значение, которое вернёт воркер).
INSERT INTO companies
    (inn, org_type, name, okved, okved_title, ogrn, account_number, registration_date,
     total_debt, equity, debt_to_equity, financials_fetched_at, registry, credit_history)
VALUES
    ('7700000001', 'OOO', 'ООО «Демо Торг»', '47.19', 'Прочая розничная торговля в неспециализированных магазинах',
        '1027700000001', '40702810900000000001', '2016-03-14',
        3000000, 5000000, 0.6000, now() - interval '1 hour',
        '{"status": "ACTIVE", "director": "Петров Пётр Петрович", "address": "г. Москва, ул. Демонстрационная, д. 1"}',
        '{"other_banks": [{"bank": "Сбербанк", "kind": "Кредит на оборотные средства", "debt": 958805}]}'),
    ('7700000002', 'OOO', 'ООО «Демо Строй»', '41.20', 'Строительство жилых и нежилых зданий',
        '1027700000002', '40702810900000000002', '2022-09-01',
        NULL, NULL, NULL, NULL,
        '{"status": "ACTIVE", "director": "Смирнова Ольга Ивановна", "address": "г. Москва, ул. Демонстрационная, д. 2"}',
        '{"other_banks": [{"bank": "ВТБ", "kind": "Инвестиционный кредит", "debt": 2400000}]}'),
    ('7700000003', 'OOO', 'ООО «Демо Логистик»', '49.41', 'Деятельность автомобильного грузового транспорта',
        '1027700000003', '40702810900000000003', '2019-06-20',
        1500000, 3000000, 0.5000, now() - interval '2 days',
        '{"status": "ACTIVE", "director": "Кузнецов Андрей Викторович", "address": "г. Москва, ул. Демонстрационная, д. 3"}',
        '{"other_banks": [{"bank": "Т-Банк", "kind": "Овердрафт", "debt": 504676}]}'),
    ('7700000004', 'OOO', 'ООО «Демо Долг»', '46.90', 'Торговля оптовая неспециализированная',
        '1027700000004', '40702810900000000004', '2012-11-05',
        7000000, 2000000, 3.5000, now() - interval '1 hour',
        '{"status": "ACTIVE", "director": "Попов Сергей Николаевич", "address": "г. Москва, ул. Демонстрационная, д. 4"}',
        '{"other_banks": [{"bank": "Сбербанк", "kind": "Кредит под залог недвижимости", "debt": 2214000},
                          {"bank": "ВТБ", "kind": "Оборотный кредит", "debt": 1476802}]}'),
    ('770000000005', 'IP', 'ИП Иванов Демо', '56.10', 'Деятельность ресторанов и услуги по доставке продуктов питания',
        '304770000000005', '40802810900000000005', '2020-02-10',
        800000, 2000000, 0.4000, now() - interval '1 hour',
        '{"status": "ACTIVE", "director": "Иванов Иван Иванович", "address": "г. Москва, ул. Демонстрационная, д. 5"}',
        '{"other_banks": [{"bank": "Совкомбанк", "kind": "Кредит для ИП", "debt": 386612}]}');

INSERT INTO users (full_name, position, phone, email, password_hash, role, company_inn) VALUES
    ('Петров Пётр Петрович', 'Генеральный директор', '+79990000001', 'client1@example.test',
        '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '7700000001'),
    ('Смирнова Ольга Ивановна', 'Генеральный директор', '+79990000002', 'client2@example.test',
        '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '7700000002'),
    ('Кузнецов Андрей Викторович', 'Генеральный директор', '+79990000003', 'client3@example.test',
        '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '7700000003'),
    ('Попов Сергей Николаевич', 'Генеральный директор', '+79990000004', 'client4@example.test',
        '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '7700000004'),
    ('Иванов Иван Иванович', 'Индивидуальный предприниматель', '+79990000005', 'client5@example.test',
        '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '770000000005'),
    ('Сидорова Анна Сергеевна', NULL, '+79990000100', 'underwriter@example.test',
        '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'employee', NULL);

-- Кредиты, которые компании уже брали в банке. Отдельной таблицы кредитов нет: выданный кредит это заявка
-- в статусе ACTIVE, OVERDUE или CLOSED с полной историей переходов и решением скоринга. График платежей
-- не хранится, его считает API (GET /loans) из суммы, ставки, срока, даты выдачи и числа внесённых платежей.
--
-- Даты действующих кредитов считаются от дня применения миграции, чтобы ближайший платёж был в будущем:
--   D = ближайшее число pay_day не раньше сегодняшнего (для просроченного — последнее, не позже чем 5 дней назад);
--   issued_at = D − (paid_payments + 1) месяцев: платежи 1…paid_payments уже прошли, следующий — D.
-- Со временем непогашенный платёж станет просроченным, как и было бы в жизни. Чтобы вернуть исходную
-- картину, базу нужно пересоздать (docker compose down -v). У закрытых кредитов даты фиксированные,
-- closed_at — дата последнего внесённого платежа.
--
-- У 7700000002 действующих кредитов нет: это сценарий «пустой кабинет».
WITH spec (phone, product_id, contract_number, amount, rate, term_months, paid_payments, pay_day, overdue,
           fixed_issued_at, late_payments, score, purpose, form) AS (
    VALUES
    -- Действующие
    ('+79990000001', 'working_capital', '0ОС-2026/1187', 2000000::bigint, 19.00::numeric, 18, 7, 15, false,
        NULL::date, 0, 82.4, 'Пополнение оборотных средств под сезонные закупки',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": false}}'),
    ('+79990000001', 'auto_loan', '0АК-2025/0342', 1500000, 15.50, 36, 20, 5, false,
        NULL, 0, 84.0, 'Покупка автомобиля для разъездов',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": true},
          "collateral": {"type": "vehicle", "description": "Легковой автомобиль", "value": 2100000},
          "vehicle": {"make": "Toyota", "model": "Camry", "year": 2024, "vin": "JTNB11HK0M3000001"}}'),
    ('+79990000003', 'equipment_loan', '0ОБ-2024/2210', 2400000, 16.50, 48, 26, 20, false,
        NULL, 0, 80.5, 'Покупка складского погрузчика',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": true},
          "collateral": {"type": "equipment", "description": "Вилочный погрузчик", "value": 3500000},
          "supply": {"supplier": "ООО «Техсклад»", "contract_details": "№ 41/24 от 12.08.2024"}}'),
    ('+79990000004', 'ooo_loan', '0КО-2026/0415', 3000000, 20.00, 18, 5, 10, true,
        NULL, 0, 71.2, 'Расширение склада и закупка стеллажей',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": false}}'),
    ('+79990000004', 'credit_line', '0ВКЛ-2026/0098', 1500000, 17.00, 24, 8, 25, false,
        NULL, 0, 73.6, 'Пополнение оборотных средств',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": false},
          "account": {"account_number": "40702810900000000004"}, "credit_line": {"desired_limit": 1500000}}'),
    ('+79990000005', 'express', '0ЭК-2026/0731', 600000, 20.50, 12, 4, 12, false,
        NULL, 0, 86.0, 'Ремонт и оборудование кафе',
        '{"applicant": {"position": "Индивидуальный предприниматель", "has_collateral": false}}'),
    -- Закрытые
    ('+79990000001', 'working_capital', '0ОС-2023/0412', 1500000, 18.00, 18, 18, NULL, false,
        '2023-04-12', 0, 81.0, 'Закупка товара к сезону',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": false}}'),
    ('+79990000001', 'express', '0ЭК-2024/1120', 500000, 21.00, 12, 7, NULL, false,
        '2024-11-20', 0, 88.0, 'Закупка товара',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": false}}'),
    ('+79990000002', 'ooo_loan', '0КО-2023/0905', 800000, 20.00, 12, 12, NULL, false,
        '2023-09-05', 0, 76.0, 'Аренда спецтехники на сезон',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": false}}'),
    ('+79990000003', 'auto_loan', '0АК-2020/1015', 2000000, 14.50, 36, 36, NULL, false,
        '2020-10-15', 0, 83.0, 'Покупка грузового фургона',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": true},
          "collateral": {"type": "vehicle", "description": "Грузовой фургон", "value": 2600000},
          "vehicle": {"make": "ГАЗ", "model": "ГАЗель NEXT", "year": 2020, "vin": "X96A21R32L2000001"}}'),
    ('+79990000003', 'loan_up_to_1m', '0КМ-2023/0301', 900000, 21.50, 12, 9, NULL, false,
        '2023-03-01', 0, 78.0, 'Ремонт автопарка',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": false}}'),
    ('+79990000004', 'working_capital', '0ОС-2022/0718', 2500000, 17.00, 24, 24, NULL, false,
        '2022-07-18', 2, 72.0, 'Оплата поставщикам по крупному контракту',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": false}}'),
    ('+79990000005', 'ip_loan', '0КИ-2024/0210', 400000, 22.00, 12, 12, NULL, false,
        '2024-02-10', 0, 85.0, 'Покупка кухонного оборудования',
        '{"applicant": {"position": "Индивидуальный предприниматель", "has_collateral": false}}')
),
dated AS (
    SELECT s.*,
        CASE
            WHEN s.fixed_issued_at IS NOT NULL THEN s.fixed_issued_at
            ELSE (CASE
                WHEN NOT s.overdue AND m.d0 >= current_date THEN m.d0
                WHEN NOT s.overdue THEN (m.d0 + interval '1 month')::date
                WHEN m.d0 <= current_date - 5 THEN m.d0
                WHEN (m.d0 - interval '1 month')::date <= current_date - 5 THEN (m.d0 - interval '1 month')::date
                ELSE (m.d0 - interval '2 months')::date
            END - make_interval(months => s.paid_payments + 1))::date
        END AS issued_at,
        CASE WHEN s.fixed_issued_at IS NOT NULL THEN 'CLOSED' WHEN s.overdue THEN 'OVERDUE' ELSE 'ACTIVE' END AS status
    FROM spec s
    -- d0 — день платежа в текущем месяце (дни платежа в демо-данных не больше 28, сдвиг на месяц точен).
    CROSS JOIN LATERAL (SELECT date_trunc('month', current_date)::date + (coalesce(s.pay_day, 1) - 1) AS d0) m
),
built AS (
    SELECT d.*, c.id AS client_id, c.company_inn, e.id AS employee_id,
        (d.issued_at - 3) + time '10:00' AS submitted_at,
        CASE WHEN d.status = 'CLOSED' THEN (d.issued_at + make_interval(months => d.paid_payments))::date END AS closed_at,
        -- Когда наступил просроченный платёж (только у OVERDUE).
        (d.issued_at + make_interval(months => d.paid_payments + 1))::date AS missed_at
    FROM dated d
    JOIN users c ON c.phone = d.phone
    CROSS JOIN (SELECT id FROM users WHERE email = 'underwriter@example.test') e
),
history AS (
    SELECT b.*,
        jsonb_build_array(
            jsonb_build_object('from', 'DRAFT', 'to', 'SUBMITTED', 'at', (b.submitted_at)::timestamptz, 'by', b.client_id::text),
            jsonb_build_object('from', 'SUBMITTED', 'to', 'DATA_CHECK', 'at', (b.submitted_at + interval '1 minute')::timestamptz, 'by', 'worker'),
            jsonb_build_object('from', 'DATA_CHECK', 'to', 'SCORING', 'at', (b.submitted_at + interval '5 minutes')::timestamptz, 'by', 'worker'),
            jsonb_build_object('from', 'SCORING', 'to', 'APPROVED', 'at', (b.submitted_at + interval '6 minutes')::timestamptz, 'by', 'worker'),
            jsonb_build_object('from', 'APPROVED', 'to', 'SIGNING', 'at', ((b.issued_at - 1) + time '11:00')::timestamptz, 'by', b.client_id::text),
            jsonb_build_object('from', 'SIGNING', 'to', 'SIGNED', 'at', ((b.issued_at - 1) + time '11:15')::timestamptz, 'by', b.client_id::text),
            jsonb_build_object('from', 'SIGNED', 'to', 'DISBURSED', 'at', (b.issued_at + time '10:00')::timestamptz, 'by', b.employee_id::text),
            jsonb_build_object('from', 'DISBURSED', 'to', 'ACTIVE', 'at', (b.issued_at + time '10:00')::timestamptz, 'by', b.employee_id::text)
        ) || CASE b.status
            WHEN 'CLOSED' THEN jsonb_build_array(jsonb_build_object(
                'from', 'ACTIVE', 'to', 'CLOSED', 'at', (b.closed_at + time '12:00')::timestamptz, 'by', b.employee_id::text,
                'comment', CASE WHEN b.paid_payments < b.term_months THEN 'Кредит погашен досрочно' ELSE 'Кредит погашен полностью' END))
            WHEN 'OVERDUE' THEN jsonb_build_array(jsonb_build_object(
                'from', 'ACTIVE', 'to', 'OVERDUE', 'at', ((b.missed_at + 1) + time '00:00')::timestamptz, 'by', b.employee_id::text,
                'comment', 'Платёж № ' || (b.paid_payments + 1) || ' не поступил'))
            ELSE '[]'::jsonb
        END AS history,
        CASE b.status
            WHEN 'CLOSED' THEN b.closed_at + time '12:00'
            WHEN 'OVERDUE' THEN (b.missed_at + 1) + time '00:00'
            ELSE b.issued_at + time '10:00'
        END AS updated_at
    FROM built b
)
INSERT INTO applications
    (version, company_inn, created_by, product_id, amount, term_months, purpose, status, form, history, decision,
     submitted_at, created_at, updated_at, rate, contract_number, issued_at, paid_payments, late_payments, closed_at)
SELECT jsonb_array_length(h.history) + 2, h.company_inn, h.client_id, h.product_id, h.amount, h.term_months,
       h.purpose, h.status, h.form::jsonb, h.history,
       jsonb_build_object('model_version', 'scoring-1', 'product_id', h.product_id, 'outcome', 'APPROVED',
           'score', h.score, 'rate', h.rate, 'amount', h.amount, 'term_months', h.term_months),
       h.submitted_at, h.submitted_at - interval '1 day', h.updated_at,
       h.rate, h.contract_number, h.issued_at, h.paid_payments, h.late_payments, h.closed_at
FROM history h;

-- +goose Down
DELETE FROM applications WHERE company_inn IN ('7700000001', '7700000002', '7700000003', '7700000004', '770000000005');
DELETE FROM offers WHERE company_inn IN ('7700000001', '7700000002', '7700000003', '7700000004', '770000000005');
DELETE FROM users WHERE email IN (
    'client1@example.test', 'client2@example.test', 'client3@example.test',
    'client4@example.test', 'client5@example.test', 'underwriter@example.test'
);
DELETE FROM companies WHERE inn IN ('7700000001', '7700000002', '7700000003', '7700000004', '770000000005');
