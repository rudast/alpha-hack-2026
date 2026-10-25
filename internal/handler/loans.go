package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/middleware"
	"github.com/rudast/alpha-hack-2026/internal/service"
)

type LoanService interface {
	Loans(ctx context.Context, inn string) (service.Loans, error)
}

type LoanHandler struct {
	svc LoanService
}

func NewLoanHandler(svc LoanService) *LoanHandler {
	return &LoanHandler{svc: svc}
}

type paymentView struct {
	Number       int     `json:"number"`
	Date         string  `json:"date"`
	Amount       float64 `json:"amount"`
	Principal    float64 `json:"principal"`
	Interest     float64 `json:"interest"`
	BalanceAfter float64 `json:"balance_after"`
	Status       string  `json:"status"`
}

type nextPaymentView struct {
	Date   string  `json:"date"`
	Amount float64 `json:"amount"`
}

type loanView struct {
	ID             string           `json:"id"`
	ProductID      string           `json:"product_id"`
	ProductName    string           `json:"product_name"`
	ContractNumber string           `json:"contract_number"`
	Amount         int64            `json:"amount"`
	Rate           float64          `json:"rate"`
	TermMonths     int              `json:"term_months"`
	IssuedAt       string           `json:"issued_at"`
	MaturityDate   string           `json:"maturity_date"`
	Status         domain.Status    `json:"status"`
	MonthlyPayment float64          `json:"monthly_payment"`
	PrincipalLeft  int64            `json:"principal_left"`
	PaidTotal      int64            `json:"paid_total"`
	PaymentsMade   int              `json:"payments_made"`
	PaymentsTotal  int              `json:"payments_total"`
	NextPayment    *nextPaymentView `json:"next_payment"`
	OverdueAmount  float64          `json:"overdue_amount"`
	Schedule       []paymentView    `json:"schedule"`
}

type closedLoanView struct {
	ID             string           `json:"id"`
	ProductID      string           `json:"product_id"`
	ProductName    string           `json:"product_name"`
	ContractNumber string           `json:"contract_number"`
	Amount         int64            `json:"amount"`
	Rate           float64          `json:"rate"`
	TermMonths     int              `json:"term_months"`
	IssuedAt       string           `json:"issued_at"`
	ClosedAt       *string          `json:"closed_at"`
	MaturityDate   string           `json:"maturity_date"`
	Status         domain.Status    `json:"status"`
	ClosedEarly    bool             `json:"closed_early"`
	MonthsEarly    int              `json:"months_early"`
	MonthlyPayment float64          `json:"monthly_payment"`
	PrincipalLeft  int64            `json:"principal_left"`
	PaidTotal      int64            `json:"paid_total"`
	Overpayment    int64            `json:"overpayment"`
	PaymentsMade   int              `json:"payments_made"`
	PaymentsTotal  int              `json:"payments_total"`
	LatePayments   int              `json:"late_payments"`
	NextPayment    *nextPaymentView `json:"next_payment"`
	Schedule       []paymentView    `json:"schedule"`
}

type loansResponse struct {
	Loans       []loanView              `json:"loans"`
	ClosedLoans []closedLoanView        `json:"closed_loans"`
	OtherBanks  []service.OtherBankLoan `json:"other_banks"`
}

// List — «Мои кредиты», «Кредитная история» и «Кредиты в других банках»: выданные кредиты компании
// с посчитанным графиком платежей.
func (h *LoanHandler) List(w http.ResponseWriter, r *http.Request) {
	p, _ := middleware.PrincipalFrom(r.Context())
	loans, err := h.svc.Loans(r.Context(), p.CompanyINN)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "company not found")
		return
	case err != nil:
		log.Printf("loans: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := loansResponse{Loans: []loanView{}, ClosedLoans: []closedLoanView{}, OtherBanks: loans.OtherBanks}
	for _, l := range loans.Active {
		resp.Loans = append(resp.Loans, loanView{
			ID: l.ID, ProductID: l.ProductID, ProductName: l.ProductName, ContractNumber: l.ContractNumber,
			Amount: l.Amount, Rate: l.Rate, TermMonths: l.TermMonths,
			IssuedAt: l.IssuedAt.Format(time.DateOnly), MaturityDate: l.MaturityDate.Format(time.DateOnly),
			Status: l.Status, MonthlyPayment: l.MonthlyPayment, PrincipalLeft: l.PrincipalLeft, PaidTotal: l.PaidTotal,
			PaymentsMade: l.PaymentsMade, PaymentsTotal: l.TermMonths, NextPayment: nextPayment(l.NextPayment),
			OverdueAmount: l.OverdueAmount, Schedule: scheduleView(l.Schedule),
		})
	}
	for _, l := range loans.Closed {
		resp.ClosedLoans = append(resp.ClosedLoans, closedLoanView{
			ID: l.ID, ProductID: l.ProductID, ProductName: l.ProductName, ContractNumber: l.ContractNumber,
			Amount: l.Amount, Rate: l.Rate, TermMonths: l.TermMonths,
			IssuedAt: l.IssuedAt.Format(time.DateOnly), ClosedAt: dateOnly(l.ClosedAt), MaturityDate: l.MaturityDate.Format(time.DateOnly),
			Status: l.Status, ClosedEarly: l.ClosedEarly, MonthsEarly: l.MonthsEarly, MonthlyPayment: l.MonthlyPayment,
			PrincipalLeft: l.PrincipalLeft, PaidTotal: l.PaidTotal, Overpayment: l.Overpayment,
			PaymentsMade: l.PaymentsMade, PaymentsTotal: l.TermMonths, LatePayments: l.LatePayments,
			Schedule: scheduleView(l.Schedule),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func nextPayment(p *service.Payment) *nextPaymentView {
	if p == nil {
		return nil
	}
	return &nextPaymentView{Date: p.Date.Format(time.DateOnly), Amount: p.Amount}
}

func scheduleView(s []service.Payment) []paymentView {
	out := make([]paymentView, 0, len(s))
	for _, p := range s {
		out = append(out, paymentView{
			Number: p.Number, Date: p.Date.Format(time.DateOnly), Amount: p.Amount, Principal: p.Principal,
			Interest: p.Interest, BalanceAfter: p.BalanceAfter, Status: p.Status,
		})
	}
	return out
}
