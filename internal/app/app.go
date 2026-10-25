package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rudast/alpha-hack-2026/internal/catalog"
	"github.com/rudast/alpha-hack-2026/internal/config"
	"github.com/rudast/alpha-hack-2026/internal/handler"
	"github.com/rudast/alpha-hack-2026/internal/middleware"
	"github.com/rudast/alpha-hack-2026/internal/queue"
	"github.com/rudast/alpha-hack-2026/internal/repository"
	"github.com/rudast/alpha-hack-2026/internal/service"
)

func Run(ctx context.Context, cfg config.Config) error {
	var (
		authStore   middleware.SessionReader
		authHandler *handler.AuthHandler
		meHandler   *handler.MeHandler
		appHandler  *handler.ApplicationHandler
		idempotency middleware.IdempotencyStore
	)

	if cfg.DatabaseURL != "" {
		pool, err := repository.NewPool(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Printf("postgres connection error: %v (running in web presentation mode)", err)
		} else {
			defer pool.Close()

			rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
			defer rdb.Close()
			if err := rdb.Ping(ctx).Err(); err != nil {
				log.Printf("redis connection error: %v (running in web presentation mode)", err)
			} else {
				aStore := repository.NewAuthStore(rdb)
				authStore = aStore
				idempotency = repository.NewIdempotencyStore(rdb)

				auth, err := service.NewAuthService(repository.NewUserRepo(pool), aStore, service.StubOTP{Code: cfg.OTPStubCode})
				if err != nil {
					log.Printf("auth service error: %v", err)
				} else {
					authHandler = handler.NewAuthHandler(auth)
				}

				companies := repository.NewCompanyRepo(pool)
				trust := service.NewTrustService(companies, queue.NewClient(rdb), cfg.TrustRefreshInterval, cfg.WorkerTimeout)
				meHandler = handler.NewMeHandler(trust)

				products, err := catalog.Load()
				if err != nil {
					log.Printf("catalog load error: %v", err)
				} else {
					applicationRepo := repository.NewApplicationRepo(pool)
					applicationPublisher := queue.NewApplicationPublisher(rdb)
					applications := service.NewApplicationService(applicationRepo, companies, products, applicationPublisher)
					appHandler = handler.NewApplicationHandler(applications)

					sweeper := service.NewApplicationSweeper(applicationRepo, companies, applicationPublisher, time.Minute, 2*time.Minute)
					go sweeper.Run(ctx)

					if consumerName, err := os.Hostname(); err == nil {
						results := service.NewResultProcessor(applicationRepo)
						go queue.NewResultConsumer(rdb).Serve(ctx, consumerName, results.Process)
					}
				}
			}
		}
	} else {
		log.Printf("DATABASE_URL is not set; running in web presentation mode (static files + assistant)")
	}

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: handler.New(handler.Deps{
			WebDir:       cfg.WebDir,
			Sessions:     authStore,
			Auth:         authHandler,
			Me:           meHandler,
			Applications: appHandler,
			Idempotency:  idempotency,
			AssistantURL: cfg.AssistantURL,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
