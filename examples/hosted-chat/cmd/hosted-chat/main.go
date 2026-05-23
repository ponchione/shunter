package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/examples/hosted-chat/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := shunter.ConfigFromEnv()
	cfg.EnableProtocol = true
	cfg.Observability.Diagnostics.MountHTTP = true
	if cfg.DataDir == "" {
		cfg.DataDir = "./data/hosted-chat"
	}
	if err := shunter.Run(ctx, app.Module(), cfg); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
