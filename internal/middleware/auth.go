// Package middleware содержит HTTP-прослойки: аутентификация по токену сессии и проверка роли.
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type SessionReader interface {
	GetSession(ctx context.Context, token string) (domain.Principal, error)
}

type principalKey struct{}

func PrincipalFrom(ctx context.Context) (domain.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(domain.Principal)
	return p, ok
}

// Authenticate читает токен из заголовка Authorization: Bearer и кладёт пользователя в контекст.
func Authenticate(sessions SessionReader) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || token == "" {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			p, err := sessions.GetSession(r.Context(), token)
			if errors.Is(err, domain.ErrUnauthorized) {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
		})
	}
}

// RequireRole пропускает только пользователей с указанной ролью. Ставится после Authenticate.
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFrom(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if p.Role != role {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
