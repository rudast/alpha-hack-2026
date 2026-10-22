-- +goose Up
-- Вход возможен и по телефону, и по почте, поэтому у каждой демонстрационной
-- учётной записи заданы оба логина.
UPDATE users SET email = 'client1@example.test' WHERE phone = '+79990000001';
UPDATE users SET email = 'client2@example.test' WHERE phone = '+79990000002';
UPDATE users SET email = 'client3@example.test' WHERE phone = '+79990000003';
UPDATE users SET email = 'client4@example.test' WHERE phone = '+79990000004';
UPDATE users SET email = 'client5@example.test' WHERE phone = '+79990000005';
UPDATE users SET phone = '+79990000100' WHERE email = 'underwriter@example.test';

-- +goose Down
UPDATE users SET email = NULL WHERE email LIKE 'client_@example.test';
UPDATE users SET phone = NULL WHERE email = 'underwriter@example.test';
