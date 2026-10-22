package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

const (
	maxLoginFailures = 3
	lockoutWindow    = 15 * time.Minute
	challengeTTL     = 5 * time.Minute
	maxCodeAttempts  = 3
	sessionTTL       = 8 * time.Hour

	RedirectClient   = "/personalAccount"
	RedirectEmployee = "/employee"
)

type UserRepository interface {
	GetClientByPhone(ctx context.Context, phone string) (domain.User, error)
	GetEmployeeByEmail(ctx context.Context, email string) (domain.User, error)
}

type AuthStore interface {
	CreateSession(ctx context.Context, token string, p domain.Principal, ttl time.Duration) error
	PutChallenge(ctx context.Context, id string, ch domain.Challenge, ttl time.Duration) error
	GetChallenge(ctx context.Context, id string) (domain.Challenge, error)
	RecordChallengeAttempt(ctx context.Context, id string, ttl time.Duration) (int, error)
	DeleteChallenge(ctx context.Context, id string) error
	LoginFailures(ctx context.Context, key string) (int, error)
	RecordLoginFailure(ctx context.Context, key string, window time.Duration) (int, error)
	ResetLoginFailures(ctx context.Context, key string) error
}

type LoginInput struct {
	Role       string // domain.RoleClient или domain.RoleEmployee
	Identifier string // телефон для клиента, почта для сотрудника
	Password   string
}

type Session struct {
	Token    string
	Role     string
	Redirect string
}

// AuthService реализует вход в два шага: пароль, затем код 2FA.
// Сессия создаётся только после второго шага.
type AuthService struct {
	users UserRepository
	store AuthStore
	otp   OTP

	// dummyHash выравнивает время ответа, когда пользователя не существует.
	dummyHash []byte
}

func NewAuthService(users UserRepository, store AuthStore, otp OTP) (*AuthService, error) {
	dummy, err := bcrypt.GenerateFromPassword([]byte("dummy"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &AuthService{users: users, store: store, otp: otp, dummyHash: dummy}, nil
}

// Login проверяет пароль и отправляет код. Возвращает идентификатор вызова для Verify.
// На любую проблему с учётными данными отвечает одной и той же ошибкой.
func (s *AuthService) Login(ctx context.Context, in LoginInput) (string, error) {
	identifier, ok := normalizeIdentifier(in.Role, in.Identifier)
	if !ok {
		return "", domain.ErrInvalidCredentials
	}
	key := in.Role + ":" + identifier

	failures, err := s.store.LoginFailures(ctx, key)
	if err != nil {
		return "", err
	}
	if failures >= maxLoginFailures {
		return "", domain.ErrTooManyAttempts
	}

	user, err := s.lookup(ctx, in.Role, identifier)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return "", err
	}
	hash := s.dummyHash
	if err == nil {
		hash = []byte(user.PasswordHash)
	}
	passwordOK := bcrypt.CompareHashAndPassword(hash, []byte(in.Password)) == nil

	if err != nil || !passwordOK {
		if _, err := s.store.RecordLoginFailure(ctx, key, lockoutWindow); err != nil {
			return "", err
		}
		return "", domain.ErrInvalidCredentials
	}
	if err := s.store.ResetLoginFailures(ctx, key); err != nil {
		return "", err
	}

	id, err := randomToken(16)
	if err != nil {
		return "", err
	}
	ch := domain.Challenge{UserID: user.ID, Role: user.Role, CompanyINN: user.CompanyINN, Destination: identifier}
	if err := s.store.PutChallenge(ctx, id, ch, challengeTTL); err != nil {
		return "", err
	}
	if err := s.otp.Send(ctx, identifier); err != nil {
		return "", err
	}
	return id, nil
}

// Verify проверяет код 2FA и создаёт сессию. После трёх неверных кодов вызов аннулируется.
func (s *AuthService) Verify(ctx context.Context, challengeID, code string) (Session, error) {
	ch, err := s.store.GetChallenge(ctx, challengeID)
	if err != nil {
		return Session{}, err
	}

	ok, err := s.otp.Verify(ctx, ch.Destination, code)
	if err != nil {
		return Session{}, err
	}
	if !ok {
		attempts, err := s.store.RecordChallengeAttempt(ctx, challengeID, challengeTTL)
		if err != nil {
			return Session{}, err
		}
		if attempts >= maxCodeAttempts {
			if err := s.store.DeleteChallenge(ctx, challengeID); err != nil {
				return Session{}, err
			}
		}
		return Session{}, domain.ErrInvalidCode
	}
	if err := s.store.DeleteChallenge(ctx, challengeID); err != nil {
		return Session{}, err
	}

	token, err := randomToken(32)
	if err != nil {
		return Session{}, err
	}
	p := domain.Principal{UserID: ch.UserID, Role: ch.Role, CompanyINN: ch.CompanyINN}
	if err := s.store.CreateSession(ctx, token, p, sessionTTL); err != nil {
		return Session{}, err
	}

	redirect := RedirectClient
	if ch.Role == domain.RoleEmployee {
		redirect = RedirectEmployee
	}
	return Session{Token: token, Role: ch.Role, Redirect: redirect}, nil
}

func (s *AuthService) lookup(ctx context.Context, role, identifier string) (domain.User, error) {
	if role == domain.RoleEmployee {
		return s.users.GetEmployeeByEmail(ctx, identifier)
	}
	return s.users.GetClientByPhone(ctx, identifier)
}

func normalizeIdentifier(role, raw string) (string, bool) {
	switch role {
	case domain.RoleClient:
		return domain.NormalizePhone(raw)
	case domain.RoleEmployee:
		email := domain.NormalizeEmail(raw)
		return email, email != ""
	}
	return "", false
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
