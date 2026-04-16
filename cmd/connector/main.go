package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"whatsapp_golang/internal/config"
	"whatsapp_golang/internal/connector"
	"whatsapp_golang/internal/logger"
	"github.com/go-redis/redis/v8"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("載入配置失敗: %v", err)
	}

	if err := logger.Init(cfg); err != nil {
		log.Fatalf("初始化日誌失敗: %v", err)
	}

	logger.Infow("Connector 服務啟動中", "version", config.GetConnectorVersion())

	// Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     50,
		MinIdleConns: 10,
	})
	if _, err := redisClient.Ping(context.Background()).Result(); err != nil {
		log.Fatalf("連接 Redis 失敗: %v", err)
	}

	// GORM
	db, err := gorm.Open(postgres.Open(cfg.GetDSN()), &gorm.Config{
		Logger: gormLogger.Default.LogMode(gormLogger.Silent),
	})
	if err != nil {
		log.Fatalf("連接資料庫失敗: %v", err)
	}

	// Pool
	poolCfg := connector.NewPoolConfigFromAppConfig(cfg)
	pool, err := connector.NewPool(db, redisClient, poolCfg)
	if err != nil {
		log.Fatalf("建立 ConnectorPool 失敗: %v", err)
	}

	// ManageConsumer 用的 StreamProducer（instanceID 作為 connectorID）
	instanceID := fmt.Sprintf("%s-%d", mustHostname(), os.Getpid())
	producer := connector.NewStreamProducer(redisClient, instanceID)
	manageConsumer := connector.NewManageConsumer(redisClient, instanceID, pool, producer, logger.BaseLogger)

	// HTTP server
	httpAddr := fmt.Sprintf(":%s", getEnv("CONNECTOR_HTTP_PORT", "8785"))
	httpServer := connector.NewHTTPServer(httpAddr, pool, logger.BaseLogger)
	if err := httpServer.Start(); err != nil {
		log.Fatalf("啟動 HTTP 伺服器失敗: %v", err)
	}

	// 啟動管理命令消費者
	ctx := context.Background()
	manageConsumer.Start(ctx)

	// 恢復所有狀態為 running 的 Connector
	go func() {
		if err := pool.RestoreAll(ctx); err != nil {
			logger.Errorw("恢復 Connector 失敗", "error", err)
		}
	}()

	logger.Infow("Connector 服務已就緒", "http", httpAddr)

	// 等待中斷信號
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logger.Info("正在關閉 Connector 服務...")

	manageConsumer.Stop()
	producer.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool.StopAll(shutdownCtx)

	httpServer.Stop()
	redisClient.Close()

	logger.Info("Connector 服務已關閉")
}

func mustHostname() string {
	h, _ := os.Hostname()
	if h == "" {
		return "unknown"
	}
	return h
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
