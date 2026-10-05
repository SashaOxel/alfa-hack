package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sashaoxel/alfa-hack/backend/internal/app"
	"github.com/sashaoxel/alfa-hack/backend/internal/config"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	log := logger.New(os.Stderr, cfg.LogFormat, cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg, log); err != nil {
		log.Error("core завершился с ошибкой", "error", err)
		os.Exit(1)
	}
}
