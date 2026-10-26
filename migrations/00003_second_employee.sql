-- +goose Up
-- Второй сотрудник банка, чтобы показать распределение заявок: взятая одним заявка пропадает из пула у другого.
-- Пароль: demo-password (тот же хэш, что у остальных демо-учётных записей).
INSERT INTO users (full_name, position, phone, email, password_hash, role, company_inn)
SELECT 'Петров Дмитрий Андреевич', 'Андеррайтер МСБ', '+79990000101', 'underwriter2@example.test',
    '$2a$10$JzEUypfdZw.rEpujzM0lO.DaWGUsuFpZlUjgUckzhcxumhLMzASCG', 'employee', NULL
WHERE NOT EXISTS (SELECT 1 FROM users WHERE lower(email) = 'underwriter2@example.test');

-- /employee/me теперь берёт должность из базы, а у первого сотрудника в сиде она не заполнена.
UPDATE users SET position = 'Андеррайтер МСБ' WHERE lower(email) = 'underwriter@example.test' AND position IS NULL;

-- +goose Down
UPDATE users SET position = NULL WHERE lower(email) = 'underwriter@example.test' AND position = 'Андеррайтер МСБ';
DELETE FROM users WHERE lower(email) = 'underwriter2@example.test';
