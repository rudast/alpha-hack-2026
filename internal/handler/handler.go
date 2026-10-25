package handler

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/middleware"
	"github.com/rudast/alpha-hack-2026/web"
)

type Deps struct {
	WebDir       string
	Sessions     middleware.SessionReader
	Auth         *AuthHandler
	Me           *MeHandler
	Applications *ApplicationHandler
	Loans        *LoanHandler
	Review       *ReviewHandler
	Products     *ProductHandler
	Idempotency  middleware.IdempotencyStore
	AssistantURL string
}

func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.Recoverer, chimw.Logger)
	r.Get("/healthz", health)

	if d.AssistantURL != "" {
		if u, err := url.Parse(d.AssistantURL); err == nil {
			proxy := httputil.NewSingleHostReverseProxy(u)
			origDirector := proxy.Director
			proxy.Director = func(req *http.Request) {
				origDirector(req)
				req.Host = u.Host
				if !strings.HasPrefix(req.URL.Path, "/api/v1/assistant") {
					req.URL.Path = "/api/v1/assistant" + req.URL.Path
				}
			}
			r.Mount("/api/v1/assistant", proxy)
		}
	}

	r.Route("/api/v1", func(r chi.Router) {
		// Каталог открыт без входа: калькулятор доступен до авторизации.
		if d.Products != nil {
			r.Get("/products", d.Products.List)
		}

		if d.Auth != nil && d.Sessions != nil && d.Applications != nil && d.Me != nil {
			r.Post("/auth/login", d.Auth.Login)
			r.Post("/auth/verify", d.Auth.Verify)

			r.Group(func(r chi.Router) {
				r.Use(middleware.Authenticate(d.Sessions))

				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireRole(domain.RoleClient))
					r.Get("/me", d.Me.Me)
					r.Post("/me/bootstrap", d.Me.Bootstrap)
					r.Get("/me/offer", d.Me.Offer)

					if d.Loans != nil {
						r.Get("/loans", d.Loans.List)
					}

					// Изменяющие запросы по заявкам идемпотентны: нужен заголовок Idempotency-Key.
					idempotent := middleware.Idempotency(d.Idempotency)
					r.Route("/applications", func(r chi.Router) {
						r.With(idempotent).Post("/", d.Applications.Create)
						r.Get("/", d.Applications.List)
						r.Get("/{id}", d.Applications.Get)
						r.With(idempotent).Patch("/{id}", d.Applications.Update)
						r.With(idempotent).Post("/{id}/submit", d.Applications.Submit)
						r.With(idempotent).Post("/{id}/accept", d.Applications.Accept)
						r.With(idempotent).Post("/{id}/decline", d.Applications.Decline)
						r.With(idempotent).Post("/{id}/resume", d.Applications.Resume)

						// Документы черновика: тело с файлом до 10 МБ, поэтому лимит идемпотентности больше обычного.
						r.With(middleware.IdempotencyWithLimit(d.Idempotency, domain.MaxDocumentSize+1<<20)).Post("/{id}/documents", d.Applications.UploadDocument)
						r.With(idempotent).Delete("/{id}/documents/{docId}", d.Applications.DeleteDocument)
					})
				})

				// Рабочее место сотрудника: заявки на ручной проверке (без базы в демо-режиме недоступно).
				if d.Review != nil {
					r.Group(func(r chi.Router) {
						r.Use(middleware.RequireRole(domain.RoleEmployee))
						idempotent := middleware.Idempotency(d.Idempotency)
						r.Route("/employee/applications", func(r chi.Router) {
							r.Get("/", d.Review.List)
							r.Get("/{id}", d.Review.Get)
							r.Get("/{id}/documents/{docId}", d.Review.Document)
							r.With(idempotent).Post("/{id}/take", d.Review.Take)
							r.With(idempotent).Post("/{id}/release", d.Review.Release)
							r.With(idempotent).Post("/{id}/decision", d.Review.Decide)
						})
					})
				}
			})
		} else {
			dbNotice := func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"error": "database or redis is not configured; specify DATABASE_URL and REDIS_ADDR in environment",
				})
			}
			r.HandleFunc("/*", dbNotice)
		}
	})

	// web.Handler отдаёт файлы из встроенного embed FS (Vercel) и с диска при локальной разработке.
	r.Handle("/*", web.Handler(d.WebDir))
	return r
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
