package domain

import "errors"

var (
	ErrNotFound           = errors.New("not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrTooManyAttempts    = errors.New("too many attempts")
	ErrInvalidChallenge   = errors.New("invalid or expired challenge")
	ErrInvalidCode        = errors.New("invalid code")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrUpstreamTimeout    = errors.New("credit history service did not answer in time")
	ErrUpstreamFailed     = errors.New("credit history service returned an error")
)
