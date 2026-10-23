package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/rudast/alpha-hack-2026/internal/domain"
	"github.com/rudast/alpha-hack-2026/internal/middleware"
)

type Deps struct {
	WebDir       string
	Sessions     middleware.SessionReader
	Auth         *AuthHandler
	Me           *MeHandler
	Applications *ApplicationHandler
}

func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.Recoverer, chimw.Logger)
	r.Get("/healthz", health)
	r.Get("/openapi.yaml", openAPISpec)
	r.Get("/docs", apiDocs)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", d.Auth.Login)
		r.Post("/auth/verify", d.Auth.Verify)

		r.Group(func(r chi.Router) {
			r.Use(middleware.Authenticate(d.Sessions))

			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(domain.RoleClient))
				r.Post("/me/bootstrap", d.Me.Bootstrap)
				r.Post("/applications", d.Applications.Create)
				r.Get("/applications/{id}", d.Applications.Get)
				r.Post("/applications/{id}/documents", d.Applications.AddDocument)
				r.Post("/applications/{id}/submit", d.Applications.Submit)
			})
		})
	})

	r.Handle("/*", http.FileServer(http.Dir(d.WebDir)))
	return r
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
