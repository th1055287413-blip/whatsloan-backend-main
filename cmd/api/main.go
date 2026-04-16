package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"whatsapp_golang/internal/app"
	"whatsapp_golang/internal/config"
)

func main() {
	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化应用
	app, err := app.NewApp(cfg)
	if err != nil {
		log.Fatalf("初始化应用失败: %v", err)
	}

	// 启动应用
	go func() {
		if err := app.Start(); err != nil {
			log.Fatalf("启动应用失败: %v", err)
		}
	}()

	// 等待中断信号
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	log.Println("正在关闭应用...")
	app.Stop()
	log.Println("应用已关闭")
}