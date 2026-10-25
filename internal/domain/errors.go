package domain

import (
	"errors"
	"strings"
)

var (
	ErrNotFound           = errors.New("not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrTooManyAttempts    = errors.New("too many attempts")
	ErrInvalidChallenge   = errors.New("invalid or expired challenge")
	ErrInvalidCode        = errors.New("invalid code")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrUpstreamTimeout    = errors.New("credit history service did not answer in time")
	ErrUpstreamFailed     = errors.New("credit history service returned an error")

	ErrConflict           = errors.New("application is not a draft")
	ErrVersionConflict    = errors.New("application was changed by another request")
	ErrAlreadyApplied     = errors.New("result is already applied")
	ErrStatusMismatch     = errors.New("application status differs from the one in the result")
	ErrInvalidInput       = errors.New("invalid field value")
	ErrUnknownProduct     = errors.New("unknown product")
	ErrProductUnavailable = errors.New("product is not available for this company or parameters")
)

// ValidationError перечисляет обязательные поля, которые ещё не заполнены.
type ValidationError struct {
	Fields []string
}

func (e *ValidationError) Error() string {
	return "missing fields: " + strings.Join(e.Fields, ", ")
}
