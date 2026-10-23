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

-- +goose Down
DELETE FROM users WHERE email IN (
    'client1@example.test', 'client2@example.test', 'client3@example.test',
    'client4@example.test', 'client5@example.test', 'underwriter@example.test'
);
DELETE FROM companies WHERE inn IN ('7700000001', '7700000002', '7700000003', '7700000004', '770000000005');
