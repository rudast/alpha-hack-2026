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
		prodHandler *handler.ProductHandler
		loanHandler *handler.LoanHandler
		revHandler  *handler.ReviewHandler
		idempotency middleware.IdempotencyStore
	)

	// Каталог продуктов встроен в бинарник и доступен независимо от подключения к БД.
	products, err := catalog.Load()
	if err != nil {
		log.Printf("catalog load error: %v", err)
	} else {
		prodHandler = handler.NewProductHandler(products)
	}

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
						log.Printf("documents store error: %v", err)
					}

					applications := service.NewApplicationService(applicationRepo, companies, offerRepo, products, applicationPublisher, documents, debts)
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
	} else {
		log.Printf("DATABASE_URL is not set; running in web presentation mode with in-memory DemoService")
	}

	// Если внешняя БД или Redis не подключены (деплой на Vercel или демонстрационный режим),
	// активируем демо-сервисы, чтобы весь интерфейс оставался полностью интерактивным.
	if authHandler == nil || authStore == nil || meHandler == nil || appHandler == nil {
		log.Printf("initializing in-memory DemoService (auth, company bootstrap, applications enabled)")
		demo := service.NewDemoPackage()
		if authHandler == nil {
			authHandler = handler.NewAuthHandler(demo.Auth)
		}
		if authStore == nil {
			authStore = demo.Sessions
		}
		if meHandler == nil {
			meHandler = handler.NewMeHandler(demo.Trust, demo.Offers, demo.Profiles)
		}
		if appHandler == nil {
			appHandler = handler.NewApplicationHandler(demo.Apps)
		}
		if loanHandler == nil {
			loanHandler = handler.NewLoanHandler(demo.Loans)
		}
		if idempotency == nil {
			idempotency = demo.Idempotency
		}
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
