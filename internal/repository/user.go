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

// GetByPhone ищет пользователя роли role по телефону в формате +7XXXXXXXXXX.
func (r *UserRepo) GetByPhone(ctx context.Context, role, phone string) (domain.User, error) {
	return r.get(ctx, `WHERE phone = $1 AND role = $2`, phone, role)
}

// GetByEmail ищет пользователя роли role по почте в нижнем регистре.
func (r *UserRepo) GetByEmail(ctx context.Context, role, email string) (domain.User, error) {
	return r.get(ctx, `WHERE lower(email) = $1 AND role = $2`, email, role)
}

func (r *UserRepo) get(ctx context.Context, where string, args ...any) (domain.User, error) {
	var u domain.User
	var phone, email, inn *string
	err := r.db.QueryRow(ctx,
		`SELECT id::text, phone, email, password_hash, role, company_inn FROM users `+where, args...,
	).Scan(&u.ID, &phone, &email, &u.PasswordHash, &u.Role, &inn)
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
