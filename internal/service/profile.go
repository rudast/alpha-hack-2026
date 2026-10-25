package service

import (
	"context"
	"encoding/json"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

type UserGetter interface {
	GetByID(ctx context.Context, id string) (domain.User, error)
}

// Profile — пользователь и его компания для кабинета: приветствие, профиль, подстановка в анкету.
type Profile struct {
	User    domain.User
	Company domain.Company
	// Руководитель, адрес и статус из ответа реестра (companies.registry); пустые, если реестр их не прислал.
	Director string
	Address  string
	Status   string
}

// ProfileService собирает профиль клиента. Пользователь и компания берутся из сессии.
type ProfileService struct {
	users     UserGetter
	companies CompanyGetter
}

func NewProfileService(users UserGetter, companies CompanyGetter) *ProfileService {
	return &ProfileService{users: users, companies: companies}
}

func (s *ProfileService) Profile(ctx context.Context, p domain.Principal) (Profile, error) {
	user, err := s.users.GetByID(ctx, p.UserID)
	if err != nil {
		return Profile{}, err
	}
	company, err := s.companies.GetByINN(ctx, p.CompanyINN)
	if err != nil {
		return Profile{}, err
	}
	var registry struct {
		Director string `json:"director"`
		Address  string `json:"address"`
		Status   string `json:"status"`
	}
	_ = json.Unmarshal(company.Registry, &registry)
	return Profile{User: user, Company: company, Director: registry.Director, Address: registry.Address, Status: registry.Status}, nil
}
