package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/examples/hosted-chat/internal/app"
)

func main() {
	cfg := shunter.ConfigFromEnv()
	cfg.EnableProtocol = true
	cfg.Observability.Diagnostics.MountHTTP = true
	if cfg.DataDir == "" {
		cfg.DataDir = "./data/hosted-chat"
	}
	if err := shunter.Run(context.Background(), app.Module(), cfg); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
