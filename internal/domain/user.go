package domain

import (
	"strings"
	"unicode"
)

const (
	RoleClient   = "client"
	RoleEmployee = "employee"
)

type User struct {
	ID           string
	FullName     string
	Position     *string
	Phone        string
	Email        string
	PasswordHash string
	Role         string
	CompanyINN   string
}

// Principal — аутентифицированный пользователь в рамках сессии.
type Principal struct {
	UserID     string `json:"user_id"`
	Role       string `json:"role"`
	CompanyINN string `json:"company_inn,omitempty"`
}

// Challenge — вход, прошедший проверку пароля и ожидающий код 2FA.
type Challenge struct {
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	CompanyINN  string `json:"company_inn,omitempty"`
	Destination string `json:"destination"`
}

// NormalizePhone приводит российский номер к виду +7XXXXXXXXXX.
func NormalizePhone(raw string) (string, bool) {
	var digits strings.Builder
	for _, r := range raw {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	switch {
	case len(d) == 11 && (d[0] == '7' || d[0] == '8'):
		return "+7" + d[1:], true
	case len(d) == 10:
		return "+7" + d, true
	}
	return "", false
}

func NormalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
