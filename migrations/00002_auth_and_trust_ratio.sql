-- +goose Up
-- Клиент входит по телефону, сотрудник по почте.
ALTER TABLE users ADD COLUMN phone text UNIQUE;
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_login_identifier_check CHECK (
    (role = 'client' AND phone IS NOT NULL) OR (role = 'employee' AND email IS NOT NULL)
);

-- Показатели долговой нагрузки из кредитной истории.
-- financials_fetched_at IS NULL означает, что данные ещё ни разу не запрашивались.
-- debt_to_equity равен NULL, если собственный капитал нулевой или отрицательный (коэффициент не определён).
ALTER TABLE companies ADD COLUMN total_debt bigint;
ALTER TABLE companies ADD COLUMN equity bigint;
ALTER TABLE companies ADD COLUMN debt_to_equity numeric(12, 4);
ALTER TABLE companies ADD COLUMN financials_fetched_at timestamptz;

-- +goose Down
ALTER TABLE companies DROP COLUMN financials_fetched_at;
ALTER TABLE companies DROP COLUMN debt_to_equity;
ALTER TABLE companies DROP COLUMN equity;
ALTER TABLE companies DROP COLUMN total_debt;
ALTER TABLE users DROP CONSTRAINT users_login_identifier_check;
ALTER TABLE users ALTER COLUMN email SET NOT NULL;
ALTER TABLE users DROP COLUMN phone;
