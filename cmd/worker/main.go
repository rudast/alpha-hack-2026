package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/redis/go-redis/v9"

	"github.com/rudast/alpha-hack-2026/internal/queue"
	"github.com/rudast/alpha-hack-2026/internal/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()

	host, _ := os.Hostname()
	log.Printf("worker %s: waiting for requests", host)
	if err := queue.Serve(ctx, rdb, host, worker.Handle); err != nil {
		log.Fatal(err)
	}
}
