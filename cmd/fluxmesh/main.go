package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"fluxmesh/internal/app"
	"fluxmesh/internal/config"
	"fluxmesh/internal/logx"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		logx.Error("加载配置失败", "err", err)
		os.Exit(1)
	}

	a, err := app.New(cfg)
	if err != nil {
		logx.Error("创建应用实例失败", "err", err)
		os.Exit(1)
	}

	if err := a.Run(ctx); err != nil {
		logx.Error("启动应用失败", "err", err)
		_ = a.Shutdown(context.Background())
		os.Exit(1)
	}

	<-ctx.Done()
	if err := a.Shutdown(context.Background()); err != nil {
		logx.Error("应用关闭失败", "err", err)
		os.Exit(1)
	}
}
