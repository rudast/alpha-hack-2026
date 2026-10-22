package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type fakeUsers []domain.User

func (f fakeUsers) GetByPhone(_ context.Context, role, phone string) (domain.User, error) {
	for _, u := range f {
		if u.Role == role && u.Phone == phone {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (f fakeUsers) GetByEmail(_ context.Context, role, email string) (domain.User, error) {
	for _, u := range f {
		if u.Role == role && u.Email == email {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

type memStore struct {
	sessions   map[string]domain.Principal
	challenges map[string]domain.Challenge
	attempts   map[string]int
	failures   map[string]int
}

func newMemStore() *memStore {
	return &memStore{
		sessions:   map[string]domain.Principal{},
		challenges: map[string]domain.Challenge{},
		attempts:   map[string]int{},
		failures:   map[string]int{},
	}
}

func (m *memStore) CreateSession(_ context.Context, token string, p domain.Principal, _ time.Duration) error {
	m.sessions[token] = p
	return nil
}

func (m *memStore) PutChallenge(_ context.Context, id string, ch domain.Challenge, _ time.Duration) error {
	m.challenges[id] = ch
	return nil
}

func (m *memStore) GetChallenge(_ context.Context, id string) (domain.Challenge, error) {
	ch, ok := m.challenges[id]
	if !ok {
		return domain.Challenge{}, domain.ErrInvalidChallenge
	}
	return ch, nil
}

func (m *memStore) RecordChallengeAttempt(_ context.Context, id string, _ time.Duration) (int, error) {
	m.attempts[id]++
	return m.attempts[id], nil
}

func (m *memStore) DeleteChallenge(_ context.Context, id string) error {
	delete(m.challenges, id)
	delete(m.attempts, id)
	return nil
}

func (m *memStore) LoginFailures(_ context.Context, key string) (int, error) {
	return m.failures[key], nil
}

func (m *memStore) RecordLoginFailure(_ context.Context, key string, _ time.Duration) (int, error) {
	m.failures[key]++
	return m.failures[key], nil
}

func (m *memStore) ResetLoginFailures(_ context.Context, key string) error {
	delete(m.failures, key)
	return nil
}

const testPassword = "demo-password"

func newAuth(t *testing.T) (*AuthService, *memStore) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	users := fakeUsers{
		{ID: "c1", Role: domain.RoleClient, Phone: "+79990000001", Email: "client1@example.test", CompanyINN: "7700000001", PasswordHash: string(hash)},
		{ID: "e1", Role: domain.RoleEmployee, Phone: "+79990000100", Email: "underwriter@example.test", PasswordHash: string(hash)},
	}
	store := newMemStore()
	svc, err := NewAuthService(users, store, StubOTP{Code: "000000"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, store
}

func TestClientLoginFlow(t *testing.T) {
	svc, store := newAuth(t)
	ctx := context.Background()

	id, err := svc.Login(ctx, LoginInput{Role: domain.RoleClient, Identifier: "8 (999) 000-00-01", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.sessions) != 0 {
		t.Fatal("session must not exist before the 2FA step")
	}

	s, err := svc.Verify(ctx, id, "000000")
	if err != nil {
		t.Fatal(err)
	}
	if s.Redirect != RedirectClient || s.Role != domain.RoleClient {
		t.Fatalf("got %+v", s)
	}
	if p := store.sessions[s.Token]; p.CompanyINN != "7700000001" {
		t.Fatalf("session principal %+v", p)
	}
	if _, err := svc.Verify(ctx, id, "000000"); !errors.Is(err, domain.ErrInvalidChallenge) {
		t.Fatalf("challenge must be single-use, got %v", err)
	}
}

func TestEmployeeLoginFlow(t *testing.T) {
	svc, _ := newAuth(t)
	ctx := context.Background()

	id, err := svc.Login(ctx, LoginInput{Role: domain.RoleEmployee, Identifier: " Underwriter@Example.test ", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	s, err := svc.Verify(ctx, id, "000000")
	if err != nil || s.Redirect != RedirectEmployee {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestLoginAcceptsPhoneOrEmailForBothRoles(t *testing.T) {
	svc, store := newAuth(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		in    LoginInput
		wantU string
	}{
		{"client by phone", LoginInput{Role: domain.RoleClient, Identifier: "+79990000001"}, "c1"},
		{"client by email", LoginInput{Role: domain.RoleClient, Identifier: " Client1@Example.test "}, "c1"},
		{"employee by email", LoginInput{Role: domain.RoleEmployee, Identifier: "underwriter@example.test"}, "e1"},
		{"employee by phone", LoginInput{Role: domain.RoleEmployee, Identifier: "8 999 000 01 00"}, "e1"},
	}
	for _, tc := range cases {
		tc.in.Password = testPassword
		id, err := svc.Login(ctx, tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := store.challenges[id].UserID; got != tc.wantU {
			t.Errorf("%s: challenge for user %q, want %q", tc.name, got, tc.wantU)
		}
	}
}

func TestLockoutIsSharedBetweenPhoneAndEmail(t *testing.T) {
	svc, _ := newAuth(t)
	ctx := context.Background()

	for _, login := range []string{"+79990000001", "client1@example.test", "+79990000001"} {
		in := LoginInput{Role: domain.RoleClient, Identifier: login, Password: "nope"}
		if _, err := svc.Login(ctx, in); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("%s: %v", login, err)
		}
	}
	// Три неудачи набраны с разных логинов одного пользователя: правильный пароль
	// не должен пройти ни по одному из них.
	for _, login := range []string{"+79990000001", "client1@example.test"} {
		in := LoginInput{Role: domain.RoleClient, Identifier: login, Password: testPassword}
		if _, err := svc.Login(ctx, in); err == nil {
			t.Fatalf("%s: login succeeded while the user must be locked", login)
		}
	}
}

func TestLoginRejectsBadCredentialsUniformly(t *testing.T) {
	svc, _ := newAuth(t)
	ctx := context.Background()

	cases := map[string]LoginInput{
		"wrong password":           {Role: domain.RoleClient, Identifier: "+79990000001", Password: "nope"},
		"unknown phone":            {Role: domain.RoleClient, Identifier: "+79990009999", Password: testPassword},
		"malformed phone":          {Role: domain.RoleClient, Identifier: "abc", Password: testPassword},
		"employee email as client": {Role: domain.RoleClient, Identifier: "underwriter@example.test", Password: testPassword},
		"client phone as employee": {Role: domain.RoleEmployee, Identifier: "+79990000001", Password: testPassword},
		"unknown role":             {Role: "admin", Identifier: "+79990000001", Password: testPassword},
	}
	for name, in := range cases {
		if _, err := svc.Login(ctx, in); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Errorf("%s: got %v, want ErrInvalidCredentials", name, err)
		}
	}
}

func TestLoginLocksAfterThreeFailures(t *testing.T) {
	svc, _ := newAuth(t)
	ctx := context.Background()
	bad := LoginInput{Role: domain.RoleEmployee, Identifier: "underwriter@example.test", Password: "nope"}

	for i := 0; i < 3; i++ {
		if _, err := svc.Login(ctx, bad); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	good := bad
	good.Password = testPassword
	if _, err := svc.Login(ctx, good); !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("got %v, want ErrTooManyAttempts even with the right password", err)
	}
}

func TestVerifyInvalidatesChallengeAfterThreeWrongCodes(t *testing.T) {
	svc, _ := newAuth(t)
	ctx := context.Background()

	id, err := svc.Login(ctx, LoginInput{Role: domain.RoleClient, Identifier: "+79990000001", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := svc.Verify(ctx, id, "111111"); !errors.Is(err, domain.ErrInvalidCode) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if _, err := svc.Verify(ctx, id, "000000"); !errors.Is(err, domain.ErrInvalidChallenge) {
		t.Fatalf("got %v, want ErrInvalidChallenge", err)
	}
}
