package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type UserRepo struct {
	db *pgxpool.Pool
}

func NewUserRepo(db *pgxpool.Pool) *UserRepo {
	return &UserRepo{db: db}
}

// GetByPhone ищет активного пользователя роли role по телефону в формате +7XXXXXXXXXX.
// Отключённые пользователи (is_active = false) не находятся, поэтому войти не могут.
func (r *UserRepo) GetByPhone(ctx context.Context, role, phone string) (domain.User, error) {
	return r.get(ctx, `WHERE phone = $1 AND role = $2 AND is_active`, phone, role)
}

// GetByID ищет активного пользователя по идентификатору (из сессии).
func (r *UserRepo) GetByID(ctx context.Context, id string) (domain.User, error) {
	u, err := r.get(ctx, `WHERE id = $1::text::uuid AND is_active`, id)
	return u, mapNotFound(err)
}

// GetByEmail ищет пользователя роли role по почте в нижнем регистре.
func (r *UserRepo) GetByEmail(ctx context.Context, role, email string) (domain.User, error) {
	return r.get(ctx, `WHERE lower(email) = $1 AND role = $2 AND is_active`, email, role)
}

func (r *UserRepo) get(ctx context.Context, where string, args ...any) (domain.User, error) {
	var u domain.User
	var phone, email, inn *string
	err := r.db.QueryRow(ctx,
		`SELECT id::text, full_name, position, phone, email, password_hash, role, company_inn FROM users `+where, args...,
	).Scan(&u.ID, &u.FullName, &u.Position, &phone, &email, &u.PasswordHash, &u.Role, &inn)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	u.Phone, u.Email, u.CompanyINN = deref(phone), deref(email), deref(inn)
	return u, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
