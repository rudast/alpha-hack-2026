package handler

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/service"
)

type AuthService interface {
	Login(ctx context.Context, in service.LoginInput) (string, error)
	Verify(ctx context.Context, challengeID, code string) (service.Session, error)
}

type AuthHandler struct {
	svc AuthService
}

func NewAuthHandler(svc AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

type loginRequest struct {
	Role     string `json:"role"`
	Login    string `json:"login"` // телефон или почта
	Password string `json:"password"`
}

// Login — первый шаг входа: проверка пароля и отправка кода 2FA.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decode(w, r, &req) {
		return
	}

	id, err := h.svc.Login(r.Context(), service.LoginInput{Role: req.Role, Identifier: req.Login, Password: req.Password})
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"challenge_id": id})
}

type verifyRequest struct {
	ChallengeID string `json:"challenge_id"`
	Code        string `json:"code"`
}

// Verify — второй шаг входа: проверка кода 2FA и выдача токена сессии.
func (h *AuthHandler) Verify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if !decode(w, r, &req) {
		return
	}
	s, err := h.svc.Verify(r.Context(), req.ChallengeID, req.Code)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": s.Token, "role": s.Role, "redirect": s.Redirect})
}

func (h *AuthHandler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid credentials")
	case errors.Is(err, domain.ErrInvalidChallenge):
		writeError(w, http.StatusUnauthorized, "invalid or expired challenge")
	case errors.Is(err, domain.ErrInvalidCode):
		writeError(w, http.StatusUnauthorized, "invalid code")
	case errors.Is(err, domain.ErrTooManyAttempts):
		w.Header().Set("Retry-After", "900")
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again later")
	default:
		log.Printf("auth: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
