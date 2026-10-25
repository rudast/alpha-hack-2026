package domain

import (
	"encoding/json"
	"slices"
	"time"
)

type Status string

const (
	StatusDraft         Status = "DRAFT"
	StatusSubmitted     Status = "SUBMITTED"
	StatusDataCheck     Status = "DATA_CHECK"
	StatusInfoRequested Status = "INFO_REQUESTED"
	StatusScoring       Status = "SCORING"
	StatusManualReview  Status = "MANUAL_REVIEW"
	StatusApproved      Status = "APPROVED"
	StatusCounterOffer  Status = "COUNTER_OFFER"
	StatusRejected      Status = "REJECTED"
	StatusSigning       Status = "SIGNING"
	StatusSigned        Status = "SIGNED"
	StatusDisbursed     Status = "DISBURSED"
	StatusActive        Status = "ACTIVE"
	StatusOverdue       Status = "OVERDUE"
	StatusClosed        Status = "CLOSED"
)

// transitions повторяет docs/state-diagram.md. Отказ выносит только сотрудник,
// поэтому из SCORING в REJECTED перехода нет.
var transitions = map[Status][]Status{
	StatusDraft:         {StatusSubmitted},
	StatusSubmitted:     {StatusDataCheck},
	StatusDataCheck:     {StatusInfoRequested, StatusScoring},
	StatusInfoRequested: {StatusDataCheck},
	StatusScoring:       {StatusManualReview, StatusApproved, StatusCounterOffer},
	StatusManualReview:  {StatusApproved, StatusCounterOffer, StatusRejected},
	StatusApproved:      {StatusSigning},
	StatusCounterOffer:  {StatusSigning, StatusDraft},
	StatusRejected:      {StatusDraft},
	StatusSigning:       {StatusSigned},
	StatusSigned:        {StatusDisbursed},
	StatusDisbursed:     {StatusActive},
	StatusActive:        {StatusOverdue, StatusClosed},
	StatusOverdue:       {StatusActive},
}

// CanTransition сообщает, допустим ли переход из статуса from в статус to.
func CanTransition(from, to Status) bool {
	for _, next := range transitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// KnownStatus сообщает, существует ли такой статус.
func KnownStatus(s Status) bool {
	if _, ok := transitions[s]; ok {
		return true
	}
	return s == StatusClosed
}

// workerTransitions — переходы, которые вправе выполнять воркер скоринга: автоматическая проверка
// данных и скоринг. Права заданы переходами, а не целевыми статусами: APPROVED воркер может выставить
// только по итогам скоринга, а не вместо сотрудника из MANUAL_REVIEW. Всё, что начинается
// с MANUAL_REVIEW, решает сотрудник; отказ (REJECTED) воркер не выносит вовсе.
var workerTransitions = map[Status][]Status{
	StatusSubmitted: {StatusDataCheck},
	StatusDataCheck: {StatusInfoRequested, StatusScoring},
	StatusScoring:   {StatusApproved, StatusCounterOffer, StatusManualReview},
}

// WorkerMayTransition сообщает, может ли воркер перевести заявку из статуса from в статус to.
func WorkerMayTransition(from, to Status) bool {
	return slices.Contains(workerTransitions[from], to)
}

// HistoryEntry — один переход статуса; By — кто его выполнил (идентификатор пользователя, позже воркер).
type HistoryEntry struct {
	From Status    `json:"from"`
	To   Status    `json:"to"`
	At   time.Time `json:"at"`
	By   string    `json:"by"`

	Comment string `json:"comment,omitempty"`
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
	Documents   []Document      `json:"documents"` // файлы из тома; в базе не хранятся
	History     []HistoryEntry  `json:"history"`
	Decision    json.RawMessage `json:"decision,omitempty"`
	SubmittedAt *time.Time      `json:"submitted_at"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`

	// OfferID — предварительное предложение, по которому создана заявка; nil у обычной заявки.
	OfferID *string `json:"offer_id"`

	// Поля выданного кредита (заявка в ACTIVE, OVERDUE, CLOSED). Клиенту отдаются через GET /loans
	// вместе с посчитанным графиком, поэтому в JSON заявки не входят.
	Rate           *float64   `json:"-"` // ставка, % годовых
	ContractNumber *string    `json:"-"`
	IssuedAt       *time.Time `json:"-"` // дата выдачи; её день месяца — день платежа
	PaidPayments   int        `json:"-"` // сколько ежемесячных платежей внесено
	LatePayments   int        `json:"-"` // сколько из них внесено с опозданием
	ClosedAt       *time.Time `json:"-"`
}

// IsLoan сообщает, выдан ли кредит по заявке (такие заявки показываются в «Моих кредитах»).
func (a Application) IsLoan() bool {
	return a.Status == StatusActive || a.Status == StatusOverdue || a.Status == StatusClosed
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

	// Decision — решение по заявке (в том числе встречное предложение decision.offer), чтобы список
	// показывал его без открытия заявки; null, пока решения нет.
	Decision json.RawMessage `json:"decision"`
}

// LoanTerms — условия, на которых выдаётся кредит, когда клиент принимает решение банка.
type LoanTerms struct {
	Amount         int64
	TermMonths     int
	Rate           float64
	ContractNumber string
	IssuedAt       time.Time
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
