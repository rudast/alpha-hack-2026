package domain

import (
	"encoding/json"
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

// workerStatuses — статусы, которые вправе выставлять воркер скоринга. REJECTED среди них нет:
// отказ выносит только сотрудник.
var workerStatuses = map[Status]bool{
	StatusDataCheck:     true,
	StatusInfoRequested: true,
	StatusScoring:       true,
	StatusManualReview:  true,
	StatusApproved:      true,
	StatusCounterOffer:  true,
}

// WorkerMaySet сообщает, может ли воркер перевести заявку в статус s.
func WorkerMaySet(s Status) bool { return workerStatuses[s] }

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
