package service

import (
	"context"
	"errors"
	"io"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

// errDemoUnsupported — действие недоступно в демо-режиме без базы (DemoApplications хранит только
// черновики и отправку). Эти методы нужны, чтобы DemoApplications удовлетворял интерфейсу обработчика.
var errDemoUnsupported = errors.New("not available in demo mode without a database")

func (a *DemoApplications) CreateFromOffer(context.Context, domain.Principal, string, domain.DraftPatch) (domain.Application, bool, error) {
	return domain.Application{}, false, domain.ErrOfferNotFound
}

func (a *DemoApplications) Accept(context.Context, domain.Principal, string) (domain.Application, error) {
	return domain.Application{}, errDemoUnsupported
}

func (a *DemoApplications) Decline(context.Context, domain.Principal, string) (domain.Application, error) {
	return domain.Application{}, errDemoUnsupported
}

func (a *DemoApplications) Resume(context.Context, domain.Principal, string) (domain.Application, error) {
	return domain.Application{}, errDemoUnsupported
}

func (a *DemoApplications) UploadDocument(context.Context, domain.Principal, string, string, string, io.Reader) (domain.Document, error) {
	return domain.Document{}, errDemoUnsupported
}

func (a *DemoApplications) DeleteDocument(context.Context, domain.Principal, string, string) error {
	return errDemoUnsupported
}
