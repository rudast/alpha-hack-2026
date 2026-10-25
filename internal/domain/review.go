package domain

import (
	"errors"
	"time"
)

// Ручная проверка заявок сотрудником. Очередь — заявки в MANUAL_REVIEW: общий пул (ни за кем
// не закреплены) и заявки, которые сотрудник взял в работу (assigned_to). Отдельного статуса «в работе»
// нет. Решение по заявке принимает только сотрудник, который её взял.

// ReviewAssignTimeout — через сколько взятая, но не решённая заявка сама возвращается в общий пул.
const ReviewAssignTimeout = 2 * time.Hour

var (
	ErrNotInReview      = errors.New("application is not under manual review")
	ErrAlreadyAssigned  = errors.New("application is already taken by another employee")
	ErrNotAssignedToYou = errors.New("application is not taken by you")
	ErrNotInfoRequested = errors.New("application is not waiting for additional documents")
)

// AssignedError — заявку уже взял другой сотрудник; Name — его ФИО для показа.
type AssignedError struct {
	Name string
}

func (e *AssignedError) Error() string { return ErrAlreadyAssigned.Error() + ": " + e.Name }
func (e *AssignedError) Unwrap() error { return ErrAlreadyAssigned }

// ReviewSummary — строка списка заявок у сотрудника.
type ReviewSummary struct {
	ID           string     `json:"id"`
	CompanyINN   string     `json:"company_inn"`
	CompanyName  string     `json:"company_name"`
	ProductID    string     `json:"product_id"`
	ProductName  string     `json:"product_name"`
	Amount       int64      `json:"amount"`
	TermMonths   int        `json:"term_months"`
	SubmittedAt  *time.Time `json:"submitted_at"`
	AssignedTo   *string    `json:"assigned_to"`
	AssignedName *string    `json:"assigned_name"`
	AssignedAt   *time.Time `json:"assigned_at"`
}

// Действия сотрудника по заявке на ручной проверке.
const (
	ReviewApprove      = "approve"
	ReviewReject       = "reject"
	ReviewRequestInfo  = "request_info"
	ReviewCounterOffer = "counter_offer"
)

// ReviewDecision — ответ сотрудника. Reason обязателен при отказе, InfoRequest — при запросе документов.
// Для встречного предложения: ProductID (по умолчанию продукт заявки), Amount, TermMonths и Rate.
type ReviewDecision struct {
	Action      string
	Reason      string
	InfoRequest string
	ProductID   string
	Amount      int64
	TermMonths  int
	Rate        float64
}

// ReviewUpdate — что записать в заявку по решению сотрудника (собирает сервис, пишет репозиторий).
type ReviewUpdate struct {
	To          Status
	Decision    []byte  // объект, который сливается с решением скоринга (decision || update)
	Comment     string  // в историю
	InfoRequest *string // для INFO_REQUESTED
}
