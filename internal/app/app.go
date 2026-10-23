package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	trust := service.NewTrustService(repository.NewCompanyRepo(pool), queue.NewClient(rdb), cfg.TrustRefreshInterval, cfg.WorkerTimeout)
	products, err := catalog.Load()
	if err != nil {
		return err
	}
	applications := service.NewApplicationService(repository.NewApplicationRepo(pool), products, queue.NewClient(rdb))
	go applications.RunDispatcher(ctx)
	go applications.RunResults(ctx)

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: handler.New(handler.Deps{
			WebDir:       cfg.WebDir,
			Sessions:     authStore,
			Auth:         handler.NewAuthHandler(auth),
			Me:           handler.NewMeHandler(trust),
			Applications: handler.NewApplicationHandler(applications),
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
