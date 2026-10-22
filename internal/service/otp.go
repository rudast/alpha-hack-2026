package service

import (
	"context"
	"crypto/subtle"
)

// OTP — второй фактор аутентификации.
type OTP interface {
	Send(ctx context.Context, destination string) error
	Verify(ctx context.Context, destination, code string) (bool, error)
}

// StubOTP — заглушка двухфакторной аутентификации: ничего не отправляет
// и принимает один фиксированный код.
type StubOTP struct {
	Code string
}

func (StubOTP) Send(context.Context, string) error { return nil }

func (o StubOTP) Verify(_ context.Context, _, code string) (bool, error) {
	return subtle.ConstantTimeCompare([]byte(o.Code), []byte(code)) == 1, nil
}
