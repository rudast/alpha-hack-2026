-- +goose Up
-- Синтетические демонстрационные данные. Пароль всех учётных записей: demo-password.
INSERT INTO companies (inn, org_type, name, okved, total_debt, equity, debt_to_equity, financials_fetched_at) VALUES
    ('7700000001',   'OOO', 'ООО «Демо Торг»',     '47.19', 3000000, 5000000, 0.6000, now() - interval '1 hour'),
    ('7700000002',   'OOO', 'ООО «Демо Строй»',    '41.20', NULL,    NULL,    NULL,   NULL),
    ('7700000003',   'OOO', 'ООО «Демо Логистик»', '49.41', 1500000, 3000000, 0.5000, now() - interval '2 days'),
    ('7700000004',   'OOO', 'ООО «Демо Долг»',     '46.90', 7000000, 2000000, 3.5000, now() - interval '1 hour'),
    ('770000000005', 'IP',  'ИП Иванов Демо',      '56.10', 800000,  2000000, 0.4000, now() - interval '1 hour');

INSERT INTO users (phone, email, password_hash, role, company_inn) VALUES
    ('+79990000001', NULL, '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '7700000001'),
    ('+79990000002', NULL, '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '7700000002'),
    ('+79990000003', NULL, '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '7700000003'),
    ('+79990000004', NULL, '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '7700000004'),
    ('+79990000005', NULL, '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'client', '770000000005');

INSERT INTO users (phone, email, password_hash, role, company_inn) VALUES
    (NULL, 'underwriter@example.test', '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'employee', NULL);

-- +goose Down
DELETE FROM users WHERE email = 'underwriter@example.test' OR phone LIKE '+7999000000_';
DELETE FROM companies WHERE inn IN ('7700000001', '7700000002', '7700000003', '7700000004', '770000000005');
