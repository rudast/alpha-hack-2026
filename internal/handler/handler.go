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
)

type Deps struct {
	WebDir       string
	Sessions     middleware.SessionReader
	Auth         *AuthHandler
	Me           *MeHandler
	Applications *ApplicationHandler
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
		if d.Auth != nil && d.Sessions != nil && d.Applications != nil && d.Me != nil {
			r.Post("/auth/login", d.Auth.Login)
			r.Post("/auth/verify", d.Auth.Verify)

			r.Group(func(r chi.Router) {
				r.Use(middleware.Authenticate(d.Sessions))

				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireRole(domain.RoleClient))
					r.Post("/me/bootstrap", d.Me.Bootstrap)

					// Изменяющие запросы по заявкам идемпотентны: нужен заголовок Idempotency-Key.
					idempotent := middleware.Idempotency(d.Idempotency)
					r.Route("/applications", func(r chi.Router) {
						r.With(idempotent).Post("/", d.Applications.Create)
						r.Get("/", d.Applications.List)
						r.Get("/{id}", d.Applications.Get)
						r.With(idempotent).Patch("/{id}", d.Applications.Update)
						r.With(idempotent).Post("/{id}/submit", d.Applications.Submit)
					})
				})
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

	r.Handle("/*", http.FileServer(http.Dir(d.WebDir)))
	return r
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
