package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
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
	GetByPhone(ctx context.Context, role, phone string) (domain.User, error)
	GetByEmail(ctx context.Context, role, email string) (domain.User, error)
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
	Identifier string // телефон или почта
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
	login, ok := parseLogin(in.Role, in.Identifier)
	if !ok {
		return "", domain.ErrInvalidCredentials
	}
	identKey := in.Role + ":" + login.value

	failures, err := s.store.LoginFailures(ctx, identKey)
	if err != nil {
		return "", err
	}
	if failures >= maxLoginFailures {
		return "", domain.ErrTooManyAttempts
	}

	user, err := s.lookup(ctx, in.Role, login)
	notFound := errors.Is(err, domain.ErrNotFound)
	if err != nil && !notFound {
		return "", err
	}

	// Пользователь может входить и по телефону, и по почте, поэтому неудачи считаются
	// ещё и на него самого: смена логина не даёт новых попыток. Заблокированному
	// пользователю отвечаем как при неверном пароле, чтобы не раскрывать, что он существует.
	keys := []string{identKey}
	userLocked := false
	hash := s.dummyHash
	if !notFound {
		userKey := "user:" + user.ID
		n, err := s.store.LoginFailures(ctx, userKey)
		if err != nil {
			return "", err
		}
		userLocked = n >= maxLoginFailures
		keys = append(keys, userKey)
		hash = []byte(user.PasswordHash)
	}
	passwordOK := bcrypt.CompareHashAndPassword(hash, []byte(in.Password)) == nil

	if notFound || userLocked || !passwordOK {
		for _, k := range keys {
			if _, err := s.store.RecordLoginFailure(ctx, k, lockoutWindow); err != nil {
				return "", err
			}
		}
		return "", domain.ErrInvalidCredentials
	}
	for _, k := range keys {
		if err := s.store.ResetLoginFailures(ctx, k); err != nil {
			return "", err
		}
	}

	id, err := randomToken(16)
	if err != nil {
		return "", err
	}
	ch := domain.Challenge{UserID: user.ID, Role: user.Role, CompanyINN: user.CompanyINN, Destination: login.value}
	if err := s.store.PutChallenge(ctx, id, ch, challengeTTL); err != nil {
		return "", err
	}
	if err := s.otp.Send(ctx, login.value); err != nil {
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

// login — нормализованный логин: телефон в виде +7XXXXXXXXXX или почта в нижнем регистре.
type login struct {
	value   string
	isEmail bool
}

func (s *AuthService) lookup(ctx context.Context, role string, l login) (domain.User, error) {
	if l.isEmail {
		return s.users.GetByEmail(ctx, role, l.value)
	}
	return s.users.GetByPhone(ctx, role, l.value)
}

// parseLogin принимает и телефон, и почту: наличие «@» определяет, что это почта.
func parseLogin(role, raw string) (login, bool) {
	if role != domain.RoleClient && role != domain.RoleEmployee {
		return login{}, false
	}
	if strings.Contains(raw, "@") {
		email := domain.NormalizeEmail(raw)
		return login{value: email, isEmail: true}, email != ""
	}
	phone, ok := domain.NormalizePhone(raw)
	return login{value: phone}, ok
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
