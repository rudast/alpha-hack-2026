package domain

import (
	"encoding/json"
	"time"
)

type Status string

// Пока заявка проходит только путь клиента: черновик и отправка.
// Остальные статусы появятся вместе с обработкой воркером.
const (
	StatusDraft     Status = "DRAFT"
	StatusSubmitted Status = "SUBMITTED"
)

// HistoryEntry — один переход статуса; By — кто его выполнил (идентификатор пользователя, позже воркер).
type HistoryEntry struct {
	From Status    `json:"from"`
	To   Status    `json:"to"`
	At   time.Time `json:"at"`
	By   string    `json:"by"`
}

type Application struct {
	ID          string          `json:"id"`
	Version     int             `json:"version"`
	CompanyINN  string          `json:"company_inn"`
	ProductID   string          `json:"product_id"`
	Amount      int64           `json:"amount"`
	TermMonths  int             `json:"term_months"`
	Purpose     string          `json:"purpose"`
	Status      Status          `json:"status"`
	Form        map[string]any  `json:"form"`
	Documents   json.RawMessage `json:"documents"`
	History     []HistoryEntry  `json:"history"`
	Decision    json.RawMessage `json:"decision,omitempty"`
	SubmittedAt *time.Time      `json:"submitted_at"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// ApplicationSummary — строка списка заявок в кабинете.
type ApplicationSummary struct {
	ID          string     `json:"id"`
	ProductID   string     `json:"product_id"`
	ProductName string     `json:"product_name"`
	Amount      int64      `json:"amount"`
	TermMonths  int        `json:"term_months"`
	Status      Status     `json:"status"`
	SubmittedAt *time.Time `json:"submitted_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// DraftPatch — изменения черновика. Нулевой указатель (или nil у Form) означает «поле не передано»,
// передан может быть и ноль, и пустая строка. Form, если передана, заменяет форму целиком.
type DraftPatch struct {
	ProductID  *string
	Amount     *int64
	TermMonths *int
	Purpose    *string
	Form       map[string]any
}
