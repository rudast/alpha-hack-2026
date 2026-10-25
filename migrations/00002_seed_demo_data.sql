-- +goose Up
-- Синтетические демонстрационные данные. Пароль всех учётных записей: demo-password.
-- Показатели долговой нагрузки заданы так, чтобы были видны все пути обновления:
--   7700000001, 7700000004, 770000000005 — свежие, воркер не вызывается;
--   7700000002 — ни разу не запрашивались;
--   7700000003 — устарели (2 дня).
INSERT INTO companies
    (inn, org_type, name, okved, ogrn, registration_date, total_debt, equity, debt_to_equity, financials_fetched_at, registry)
VALUES
    ('7700000001', 'OOO', 'ООО «Демо Торг»', '47.19', '1027700000001', '2016-03-14',
        3000000, 5000000, 0.6000, now() - interval '1 hour',
        '{"status": "ACTIVE", "director": "Петров Пётр Петрович", "address": "г. Москва, ул. Демонстрационная, д. 1"}'),
    ('7700000002', 'OOO', 'ООО «Демо Строй»', '41.20', '1027700000002', '2022-09-01',
        NULL, NULL, NULL, NULL,
        '{"status": "ACTIVE", "director": "Смирнова Ольга Ивановна", "address": "г. Москва, ул. Демонстрационная, д. 2"}'),
    ('7700000003', 'OOO', 'ООО «Демо Логистик»', '49.41', '1027700000003', '2019-06-20',
        1500000, 3000000, 0.5000, now() - interval '2 days',
        '{"status": "ACTIVE", "director": "Кузнецов Андрей Викторович", "address": "г. Москва, ул. Демонстрационная, д. 3"}'),
    ('7700000004', 'OOO', 'ООО «Демо Долг»', '46.90', '1027700000004', '2012-11-05',
        7000000, 2000000, 3.5000, now() - interval '1 hour',
        '{"status": "ACTIVE", "director": "Попов Сергей Николаевич", "address": "г. Москва, ул. Демонстрационная, д. 4"}'),
    ('770000000005', 'IP', 'ИП Иванов Демо', '56.10', '304770000000005', '2020-02-10',
        800000, 2000000, 0.4000, now() - interval '1 hour',
        '{"status": "ACTIVE", "director": "Иванов Иван Иванович", "address": "г. Москва, ул. Демонстрационная, д. 5"}');

INSERT INTO users (full_name, phone, email, password_hash, role, company_inn) VALUES
    ('Петров Пётр Петрович', '+79990000001', 'client1@example.test',
        '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '7700000001'),
    ('Смирнова Ольга Ивановна', '+79990000002', 'client2@example.test',
        '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '7700000002'),
    ('Кузнецов Андрей Викторович', '+79990000003', 'client3@example.test',
        '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '7700000003'),
    ('Попов Сергей Николаевич', '+79990000004', 'client4@example.test',
        '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '7700000004'),
    ('Иванов Иван Иванович', '+79990000005', 'client5@example.test',
        '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '770000000005'),
    ('Сидорова Анна Сергеевна', '+79990000100', 'underwriter@example.test',
        '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'employee', NULL);

-- Кредиты, которые компании уже брали в банке. Отдельной таблицы кредитов нет: выданный кредит это заявка
-- в статусе ACTIVE, OVERDUE или CLOSED с полной историей переходов и решением скоринга.
-- В истории {client} и {employee} заменяются на идентификаторы клиента компании и андеррайтера.
-- Ставки в decision посчитаны по формуле docs/scoring.md (раздел 10). Документы не заполнены: загрузка не реализована.
--   7700000001  working_capital 2 000 000 ₽ / 24 мес, ACTIVE;  express 500 000 ₽ / 12 мес, CLOSED
--   7700000002  loan_up_to_1m 800 000 ₽ / 12 мес, CLOSED
--   7700000003  auto_loan 1 200 000 ₽ / 36 мес, ACTIVE (залог: автомобиль)
--   7700000004  ooo_loan 3 000 000 ₽ / 36 мес, ACTIVE (одобрен андеррайтером после ручной проверки);
--               working_capital 2 500 000 ₽ / 18 мес, OVERDUE
--   770000000005  ip_loan 600 000 ₽ / 24 мес, ACTIVE
INSERT INTO applications
    (version, company_inn, created_by, assigned_to, product_id, amount, term_months, purpose, status,
     form, history, decision, submitted_at, created_at, updated_at)
SELECT v.version, c.company_inn, c.id, CASE WHEN v.manual THEN e.id END,
       v.product_id, v.amount, v.term_months, v.purpose, v.status,
       v.form::jsonb,
       replace(replace(v.history, '{client}', c.id::text), '{employee}', e.id::text)::jsonb,
       v.decision::jsonb,
       v.submitted_at, v.submitted_at - interval '1 day', v.updated_at
FROM (VALUES
    ('+79990000001', 10, false, 'working_capital', 2000000::bigint, 24, 'Пополнение оборотных средств под сезонные закупки', 'ACTIVE',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": false}}',
        '[{"from": "DRAFT", "to": "SUBMITTED", "at": "2025-03-10T10:00:00+03:00", "by": "{client}"},
          {"from": "SUBMITTED", "to": "DATA_CHECK", "at": "2025-03-10T10:01:00+03:00", "by": "worker"},
          {"from": "DATA_CHECK", "to": "SCORING", "at": "2025-03-10T10:04:00+03:00", "by": "worker"},
          {"from": "SCORING", "to": "APPROVED", "at": "2025-03-10T10:05:00+03:00", "by": "worker"},
          {"from": "APPROVED", "to": "SIGNING", "at": "2025-03-11T09:30:00+03:00", "by": "{client}"},
          {"from": "SIGNING", "to": "SIGNED", "at": "2025-03-11T09:42:00+03:00", "by": "{client}"},
          {"from": "SIGNED", "to": "DISBURSED", "at": "2025-03-12T11:00:00+03:00", "by": "{employee}"},
          {"from": "DISBURSED", "to": "ACTIVE", "at": "2025-03-12T11:00:00+03:00", "by": "{employee}"}]',
        '{"model_version": "scoring-1", "product_id": "working_capital", "class": "U", "score": 82.4, "outcome": "APPROVED",
          "rate": 21.64, "amount": 2000000, "term_months": 24,
          "reasons_positive": [{"factor": "C1a", "text": "Нет просрочек в кредитной истории"},
                               {"factor": "C2", "text": "Умеренная долговая нагрузка"}],
          "reasons_negative": []}',
        timestamptz '2025-03-10 10:00:00+03', timestamptz '2025-03-12 11:00:00+03'),

    ('+79990000001', 11, false, 'express', 500000, 12, 'Закупка товара', 'CLOSED',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": false}}',
        '[{"from": "DRAFT", "to": "SUBMITTED", "at": "2023-04-05T15:20:00+03:00", "by": "{client}"},
          {"from": "SUBMITTED", "to": "DATA_CHECK", "at": "2023-04-05T15:21:00+03:00", "by": "worker"},
          {"from": "DATA_CHECK", "to": "SCORING", "at": "2023-04-05T15:23:00+03:00", "by": "worker"},
          {"from": "SCORING", "to": "APPROVED", "at": "2023-04-05T15:24:00+03:00", "by": "worker"},
          {"from": "APPROVED", "to": "SIGNING", "at": "2023-04-05T16:10:00+03:00", "by": "{client}"},
          {"from": "SIGNING", "to": "SIGNED", "at": "2023-04-05T16:15:00+03:00", "by": "{client}"},
          {"from": "SIGNED", "to": "DISBURSED", "at": "2023-04-06T10:00:00+03:00", "by": "{employee}"},
          {"from": "DISBURSED", "to": "ACTIVE", "at": "2023-04-06T10:00:00+03:00", "by": "{employee}"},
          {"from": "ACTIVE", "to": "CLOSED", "at": "2024-04-08T12:00:00+03:00", "by": "{employee}", "comment": "Кредит погашен полностью"}]',
        '{"model_version": "scoring-1", "product_id": "express", "class": "X", "score": 88.0, "outcome": "APPROVED",
          "rate": 24.8, "amount": 500000, "term_months": 12,
          "reasons_positive": [{"factor": "C3", "text": "Компания работает больше 5 лет"},
                               {"factor": "C1b", "text": "Мало запросов в БКИ за последние 90 дней"}],
          "reasons_negative": []}',
        timestamptz '2023-04-05 15:20:00+03', timestamptz '2024-04-08 12:00:00+03'),

    ('+79990000002', 11, false, 'loan_up_to_1m', 800000, 12, 'Аренда спецтехники на сезон', 'CLOSED',
        '{"applicant": {"position": "Директор", "has_collateral": false}}',
        '[{"from": "DRAFT", "to": "SUBMITTED", "at": "2023-11-14T11:00:00+03:00", "by": "{client}"},
          {"from": "SUBMITTED", "to": "DATA_CHECK", "at": "2023-11-14T11:01:00+03:00", "by": "worker"},
          {"from": "DATA_CHECK", "to": "SCORING", "at": "2023-11-14T11:05:00+03:00", "by": "worker"},
          {"from": "SCORING", "to": "APPROVED", "at": "2023-11-14T11:06:00+03:00", "by": "worker"},
          {"from": "APPROVED", "to": "SIGNING", "at": "2023-11-15T10:00:00+03:00", "by": "{client}"},
          {"from": "SIGNING", "to": "SIGNED", "at": "2023-11-15T10:20:00+03:00", "by": "{client}"},
          {"from": "SIGNED", "to": "DISBURSED", "at": "2023-11-16T09:30:00+03:00", "by": "{employee}"},
          {"from": "DISBURSED", "to": "ACTIVE", "at": "2023-11-16T09:30:00+03:00", "by": "{employee}"},
          {"from": "ACTIVE", "to": "CLOSED", "at": "2024-11-18T12:00:00+03:00", "by": "{employee}", "comment": "Кредит погашен полностью"}]',
        '{"model_version": "scoring-1", "product_id": "loan_up_to_1m", "class": "U", "score": 76.0, "outcome": "APPROVED",
          "rate": 22.6, "amount": 800000, "term_months": 12,
          "reasons_positive": [{"factor": "P1", "text": "Платёж небольшой относительно оборота"}],
          "reasons_negative": [{"factor": "C3", "text": "Компания работает меньше 3 лет"}]}',
        timestamptz '2023-11-14 11:00:00+03', timestamptz '2024-11-18 12:00:00+03'),

    ('+79990000003', 10, false, 'auto_loan', 1200000, 36, 'Покупка грузового фургона для доставки', 'ACTIVE',
        '{"applicant": {"position": "Генеральный директор", "has_collateral": true},
          "collateral": {"type": "vehicle", "description": "Грузовой фургон", "value": 1900000},
          "vehicle": {"make": "ГАЗ", "model": "ГАЗель NEXT", "year": 2024, "vin": "X96A21R32R2000001"}}',
        '[{"from": "DRAFT", "to": "SUBMITTED", "at": "2024-06-03T12:30:00+03:00", "by": "{client}"},
          {"from": "SUBMITTED", "to": "DATA_CHECK", "at": "2024-06-03T12:31:00+03:00", "by": "worker"},
          {"from": "DATA_CHECK", "to": "SCORING", "at": "2024-06-03T12:36:00+03:00", "by": "worker"},
          {"from": "SCORING", "to": "APPROVED", "at": "2024-06-03T12:37:00+03:00", "by": "worker"},
          {"from": "APPROVED", "to": "SIGNING", "at": "2024-06-04T10:00:00+03:00", "by": "{client}"},
          {"from": "SIGNING", "to": "SIGNED", "at": "2024-06-04T10:25:00+03:00", "by": "{client}"},
          {"from": "SIGNED", "to": "DISBURSED", "at": "2024-06-05T14:00:00+03:00", "by": "{employee}"},
          {"from": "DISBURSED", "to": "ACTIVE", "at": "2024-06-05T14:00:00+03:00", "by": "{employee}"}]',
        '{"model_version": "scoring-1", "product_id": "auto_loan", "class": "S", "score": 84.0, "outcome": "APPROVED",
          "rate": 16.4, "amount": 1200000, "term_months": 36,
          "reasons_positive": [{"factor": "C1a", "text": "Нет просрочек в кредитной истории"},
                               {"factor": "C2", "text": "Низкая долговая нагрузка"}],
          "reasons_negative": []}',
        timestamptz '2024-06-03 12:30:00+03', timestamptz '2024-06-05 14:00:00+03'),

    ('+79990000004', 11, true, 'ooo_loan', 3000000, 36, 'Расширение склада и закупка стеллажей', 'ACTIVE',
        '{"applicant": {"position": "Финансовый директор", "has_collateral": false}}',
        '[{"from": "DRAFT", "to": "SUBMITTED", "at": "2024-02-19T09:40:00+03:00", "by": "{client}"},
          {"from": "SUBMITTED", "to": "DATA_CHECK", "at": "2024-02-19T09:41:00+03:00", "by": "worker"},
          {"from": "DATA_CHECK", "to": "SCORING", "at": "2024-02-19T09:47:00+03:00", "by": "worker"},
          {"from": "SCORING", "to": "MANUAL_REVIEW", "at": "2024-02-19T09:48:00+03:00", "by": "worker", "comment": "Балл ниже порога встречного предложения"},
          {"from": "MANUAL_REVIEW", "to": "APPROVED", "at": "2024-02-21T16:00:00+03:00", "by": "{employee}", "comment": "Одобрено андеррайтером: стабильные контракты с крупными покупателями, ставка 24,5 %"},
          {"from": "APPROVED", "to": "SIGNING", "at": "2024-02-22T10:00:00+03:00", "by": "{client}"},
          {"from": "SIGNING", "to": "SIGNED", "at": "2024-02-22T10:30:00+03:00", "by": "{client}"},
          {"from": "SIGNED", "to": "DISBURSED", "at": "2024-02-26T11:00:00+03:00", "by": "{employee}"},
          {"from": "DISBURSED", "to": "ACTIVE", "at": "2024-02-26T11:00:00+03:00", "by": "{employee}"}]',
        '{"model_version": "scoring-1", "product_id": "ooo_loan", "class": "U", "score": 48.2, "outcome": "MANUAL_REVIEW",
          "rate": 26.77, "amount": 3000000, "term_months": 36,
          "reasons_positive": [{"factor": "C3", "text": "Компания работает больше 5 лет"}],
          "reasons_negative": [{"factor": "C2", "text": "Высокая долговая нагрузка"},
                               {"factor": "C1b", "text": "Много запросов в БКИ за последние 90 дней"}]}',
        timestamptz '2024-02-19 09:40:00+03', timestamptz '2024-02-26 11:00:00+03'),

    ('+79990000004', 11, false, 'working_capital', 2500000, 18, 'Оплата поставщикам по крупному контракту', 'OVERDUE',
        '{"applicant": {"position": "Финансовый директор", "has_collateral": false}}',
        '[{"from": "DRAFT", "to": "SUBMITTED", "at": "2025-05-20T14:00:00+03:00", "by": "{client}"},
          {"from": "SUBMITTED", "to": "DATA_CHECK", "at": "2025-05-20T14:01:00+03:00", "by": "worker"},
          {"from": "DATA_CHECK", "to": "SCORING", "at": "2025-05-20T14:06:00+03:00", "by": "worker"},
          {"from": "SCORING", "to": "APPROVED", "at": "2025-05-20T14:07:00+03:00", "by": "worker"},
          {"from": "APPROVED", "to": "SIGNING", "at": "2025-05-21T09:15:00+03:00", "by": "{client}"},
          {"from": "SIGNING", "to": "SIGNED", "at": "2025-05-21T09:40:00+03:00", "by": "{client}"},
          {"from": "SIGNED", "to": "DISBURSED", "at": "2025-05-22T12:00:00+03:00", "by": "{employee}"},
          {"from": "DISBURSED", "to": "ACTIVE", "at": "2025-05-22T12:00:00+03:00", "by": "{employee}"},
          {"from": "ACTIVE", "to": "OVERDUE", "at": "2026-09-05T00:00:00+03:00", "by": "{employee}", "comment": "Платёж за август не поступил, просрочка 35 дней"}]',
        '{"model_version": "scoring-1", "product_id": "working_capital", "class": "U", "score": 72.0, "outcome": "APPROVED",
          "rate": 23.2, "amount": 2500000, "term_months": 18,
          "reasons_positive": [{"factor": "C3", "text": "Компания работает больше 5 лет"},
                               {"factor": "P1", "text": "Платёж небольшой относительно оборота"}],
          "reasons_negative": [{"factor": "C2", "text": "Повышенная долговая нагрузка"}]}',
        timestamptz '2025-05-20 14:00:00+03', timestamptz '2026-09-05 00:00:00+03'),

    ('+79990000005', 10, false, 'ip_loan', 600000, 24, 'Ремонт и оборудование кафе', 'ACTIVE',
        '{"applicant": {"position": "Индивидуальный предприниматель", "has_collateral": false}}',
        '[{"from": "DRAFT", "to": "SUBMITTED", "at": "2025-08-11T18:10:00+03:00", "by": "{client}"},
          {"from": "SUBMITTED", "to": "DATA_CHECK", "at": "2025-08-11T18:11:00+03:00", "by": "worker"},
          {"from": "DATA_CHECK", "to": "SCORING", "at": "2025-08-11T18:14:00+03:00", "by": "worker"},
          {"from": "SCORING", "to": "APPROVED", "at": "2025-08-11T18:15:00+03:00", "by": "worker"},
          {"from": "APPROVED", "to": "SIGNING", "at": "2025-08-12T10:05:00+03:00", "by": "{client}"},
          {"from": "SIGNING", "to": "SIGNED", "at": "2025-08-12T10:20:00+03:00", "by": "{client}"},
          {"from": "SIGNED", "to": "DISBURSED", "at": "2025-08-13T11:00:00+03:00", "by": "{employee}"},
          {"from": "DISBURSED", "to": "ACTIVE", "at": "2025-08-13T11:00:00+03:00", "by": "{employee}"}]',
        '{"model_version": "scoring-1", "product_id": "ip_loan", "class": "U", "score": 86.0, "outcome": "APPROVED",
          "rate": 21.1, "amount": 600000, "term_months": 24,
          "reasons_positive": [{"factor": "C2", "text": "Низкая долговая нагрузка"},
                               {"factor": "C1a", "text": "Нет просрочек в кредитной истории"}],
          "reasons_negative": []}',
        timestamptz '2025-08-11 18:10:00+03', timestamptz '2025-08-13 11:00:00+03')
) AS v(phone, version, manual, product_id, amount, term_months, purpose, status, form, history, decision, submitted_at, updated_at)
JOIN users c ON c.phone = v.phone
CROSS JOIN (SELECT id FROM users WHERE email = 'underwriter@example.test') AS e;

-- +goose Down
DELETE FROM applications WHERE company_inn IN ('7700000001', '7700000002', '7700000003', '7700000004', '770000000005');
DELETE FROM offers WHERE company_inn IN ('7700000001', '7700000002', '7700000003', '7700000004', '770000000005');
DELETE FROM users WHERE email IN (
    'client1@example.test', 'client2@example.test', 'client3@example.test',
    'client4@example.test', 'client5@example.test', 'underwriter@example.test'
);
DELETE FROM companies WHERE inn IN ('7700000001', '7700000002', '7700000003', '7700000004', '770000000005');
