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
	"github.com/rudast/alpha-hack-2026/internal/queue"
	"github.com/rudast/alpha-hack-2026/internal/repository"
	"github.com/rudast/alpha-hack-2026/internal/service"
)

func Run(ctx context.Context, cfg config.Config) error {
	pool, err := repository.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer pool.Close()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}

	authStore := repository.NewAuthStore(rdb)
	auth, err := service.NewAuthService(repository.NewUserRepo(pool), authStore, service.StubOTP{Code: cfg.OTPStubCode})
	if err != nil {
		return err
	}
	companies := repository.NewCompanyRepo(pool)
	trust := service.NewTrustService(companies, queue.NewClient(rdb), cfg.TrustRefreshInterval, cfg.WorkerTimeout)

	products, err := catalog.Load()
	if err != nil {
		return err
	}
	applicationRepo := repository.NewApplicationRepo(pool)
	applicationPublisher := queue.NewApplicationPublisher(rdb)
	documents, err := repository.NewDocumentStore(cfg.DocumentsDir)
	if err != nil {
		return fmt.Errorf("documents: %w", err)
	}
	offerRepo := repository.NewOfferRepo(pool)
	offers := service.NewOfferService(offerRepo, companies, products)
	applications := service.NewApplicationService(applicationRepo, companies, offerRepo, products, applicationPublisher, documents)

	// Сборщик: раз в минуту повторно отправляет воркеру заявки, которые больше 2 минут остаются в SUBMITTED.
	sweeper := service.NewApplicationSweeper(applicationRepo, companies, documents, applicationPublisher, time.Minute, 2*time.Minute)
	go sweeper.Run(ctx)

	// Приём результатов воркера скоринга: читает applications:results и пишет статусы и решения в Postgres.
	consumerName, err := os.Hostname()
	if err != nil {
		return err
	}
	results := service.NewResultProcessor(applicationRepo)
	go queue.NewResultConsumer(rdb).Serve(ctx, consumerName, results.Process)

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: handler.New(handler.Deps{
			WebDir:       cfg.WebDir,
			Sessions:     authStore,
			Auth:         handler.NewAuthHandler(auth),
			Me:           handler.NewMeHandler(trust, offers, service.NewProfileService(repository.NewUserRepo(pool), companies)),
			Applications: handler.NewApplicationHandler(applications),
			Products:     handler.NewProductHandler(products),
			Loans:        handler.NewLoanHandler(service.NewLoanService(applicationRepo, companies, products)),
			Idempotency:  repository.NewIdempotencyStore(rdb),
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
