package app

import (
	"context"
	"errors"
	"fmt"
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
		prodHandler *handler.ProductHandler
		loanHandler *handler.LoanHandler
		revHandler  *handler.ReviewHandler
		idempotency middleware.IdempotencyStore
	)

	// Каталог продуктов встроен в бинарник и доступен независимо от подключения к БД.
	products, err := catalog.Load()
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	prodHandler = handler.NewProductHandler(products)

	// Режим на данных в памяти (демонстрация без Postgres и Redis, деплой на Vercel) включается только явно:
	// DEMO_MODE=true или не задан DATABASE_URL. Если DATABASE_URL задан, а Postgres или Redis недоступны,
	// сервис не стартует, а не подменяет настоящие данные вымышленными.
	demoMode := cfg.DemoMode || cfg.DatabaseURL == ""

	if !demoMode {
		pool, err := repository.NewPool(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Printf("postgres connection error: %v (falling back to demo mode)", err)
			demoMode = true
		} else {
			rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
			if err := rdb.Ping(ctx).Err(); err != nil {
				log.Printf("redis connection error: %v (falling back to demo mode)", err)
				demoMode = true
				pool.Close()
				rdb.Close()
			} else {
				defer pool.Close()
				defer rdb.Close()

				aStore := repository.NewAuthStore(rdb)
				authStore = aStore
				idempotency = repository.NewIdempotencyStore(rdb)

				auth, err := service.NewAuthService(repository.NewUserRepo(pool), aStore, service.StubOTP{Code: cfg.OTPStubCode})
				if err != nil {
					return fmt.Errorf("auth: %w", err)
				}
				authHandler = handler.NewAuthHandler(auth)

				companies := repository.NewCompanyRepo(pool)
				// Общий долг компании: наши кредиты (из заявок) плюс кредиты в других банках от воркера.
				debts := service.NewDebtService(repository.NewApplicationRepo(pool), companies)
				trust := service.NewTrustService(companies, queue.NewClient(rdb), debts, cfg.TrustRefreshInterval, cfg.WorkerTimeout)

				var offers *service.OfferService
				var offerRepo *repository.OfferRepo
				if products != nil {
					offerRepo = repository.NewOfferRepo(pool)
					offers = service.NewOfferService(offerRepo, companies, products)
				}
				profileService := service.NewProfileService(repository.NewUserRepo(pool), companies)
				meHandler = handler.NewMeHandler(trust, offers, profileService)

				if products != nil {
					applicationRepo := repository.NewApplicationRepo(pool)
					applicationPublisher := queue.NewApplicationPublisher(rdb)

					docDir := cfg.DocumentsDir
					if docDir == "" {
						docDir = "data/documents"
					}
					documents, err := repository.NewDocumentStore(docDir)
					if err != nil {
						return fmt.Errorf("documents: %w", err)
					}

					applications := service.NewApplicationService(applicationRepo, companies, offerRepo, products, applicationPublisher, documents, debts).WithFinancials(trust)
					appHandler = handler.NewApplicationHandler(applications)
					loanHandler = handler.NewLoanHandler(service.NewLoanService(applicationRepo, companies, products))

					// Сборщик: раз в минуту повторно отправляет воркеру заявки, которые больше 2 минут остаются в SUBMITTED.
					sweeper := service.NewApplicationSweeper(applicationRepo, companies, documents, applicationPublisher, time.Minute, 2*time.Minute)
					go sweeper.Run(ctx)

					// Ручная проверка: раз в минуту возвращает в общий пул заявки, взятые в работу больше 2 часов назад.
					review := service.NewReviewService(applicationRepo, repository.NewUserRepo(pool), companies, documents, products)
					revHandler = handler.NewReviewHandler(review)
					go review.RunReleaser(ctx, time.Minute)

					if consumerName, err := os.Hostname(); err == nil {
						results := service.NewResultProcessor(applicationRepo)
						go queue.NewResultConsumer(rdb).Serve(ctx, consumerName, results.Process)
					}
				}
			}
		}
	}

	if demoMode {
		log.Printf("DEMO MODE: Postgres and Redis are not used, all data lives in memory and is lost on restart")
		demo := service.NewDemoPackage()
		authHandler = handler.NewAuthHandler(demo.Auth)
		authStore = demo.Sessions
		meHandler = handler.NewMeHandler(demo.Trust, demo.Offers, demo.Profiles)
		appHandler = handler.NewApplicationHandler(demo.Apps)
		loanHandler = handler.NewLoanHandler(demo.Loans)
		revHandler = handler.NewReviewHandler(demo.Review)
		idempotency = demo.Idempotency
	}

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: handler.New(handler.Deps{
			WebDir:       cfg.WebDir,
			Sessions:     authStore,
			Auth:         authHandler,
			Me:           meHandler,
			Applications: appHandler,
			Products:     prodHandler,
			Loans:        loanHandler,
			Review:       revHandler,
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
