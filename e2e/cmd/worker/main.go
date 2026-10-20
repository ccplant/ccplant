package main

import (
	"context"
	"github.com/ccplant/ccplant/e2e/internal/worker"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	host, _ := os.Hostname()
	w := worker.Worker{API: env("E2E_API_URL", "http://localhost:8081"), ID: env("E2E_WORKER_ID", host), Root: env("E2E_ROOT", ".")}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := w.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
