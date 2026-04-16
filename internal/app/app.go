package app

import (
	"context"
	"fmt"
	"time"

	appConfig "whatsapp_golang/internal/config"
	"whatsapp_golang/internal/database"
	"whatsapp_golang/internal/handler/realtime"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/metrics"
	"whatsapp_golang/internal/middleware"
	"whatsapp_golang/internal/model"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/robfig/cron/v3"
)

// App 應用結構
type App struct {
	config        *appConfig.Config
	db            database.Database
	redis         *redis.Client
	services      *Services
	handlers      *Handlers
	router        *gin.Engine
	cronScheduler  *cron.Cron
	aiAnalysisStop chan struct{}
	aiAnalysisDone chan struct{}
}

// NewApp 創建應用實例
func NewApp(cfg *appConfig.Config) (*App, error) {
	// 初始化日誌
	if err := logger.Init(cfg); err != nil {
		return nil, fmt.Errorf("初始化日誌失敗: %v", err)
	}

	logger.Info("正在初始化應用")

	// 初始化數據庫
	db, err := database.NewDatabase(cfg)
	if err != nil {
		return nil, fmt.Errorf("初始化數據庫失敗: %v", err)
	}

	// 執行數據庫遷移
	if err := db.Migrate(); err != nil {
		return nil, fmt.Errorf("數據庫遷移失敗: %v", err)
	}

	logger.Info("數據庫初始化完成")

	// 初始化 Redis 客戶端
	redisClient := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     50,
		MinIdleConns: 10,
	})

	// 測試 Redis 連接
	ctx := context.Background()
	_, err = redisClient.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("連接 Redis 失敗: %v", err)
	}

	logger.Info("Redis 客戶端初始化完成")

	// 初始化所有服務
	services, err := initServices(cfg, db, redisClient)
	if err != nil {
		return nil, fmt.Errorf("初始化服務失敗: %v", err)
	}

	// 初始化所有 Handler
	handlers := initHandlers(cfg, db, redisClient, services)

	// 設置 Gin 模式
	if !cfg.Server.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	// 創建路由
	router := gin.New()
	if cfg.Log.Level != "error" {
		router.Use(gin.Logger())
	}
	router.Use(gin.Recovery(), middleware.CORSMiddleware(), middleware.LoggerMiddleware(), metrics.GinMiddleware())

	app := &App{
		config:         cfg,
		db:             db,
		redis:          redisClient,
		services:       services,
		handlers:       handlers,
		router:         router,
		aiAnalysisStop: make(chan struct{}),
	}

	// 註冊 Prometheus 業務指標 collector
	prometheus.MustRegister(metrics.NewBusinessCollector(redisClient))

	// 設置路由
	app.setupRoutes()

	// 啟動後台任務
	app.startBackgroundTasks()

	// 初始化定時任務
	app.initCronScheduler()

	logger.Info("應用初始化完成")
	return app, nil
}

// startBackgroundTasks 啟動後台任務
func (a *App) startBackgroundTasks() {
	// 設定 Gateway 訊息廣播回調（WebSocket + SSE）
	a.services.Gateway.SetMessageBroadcast(func(accountID uint, msg *model.WhatsAppMessage) {
		messageData := map[string]interface{}{
			"id":             msg.ID,
			"message_id":     msg.MessageID,
			"from_jid":       msg.FromJID,
			"to_jid":         msg.ToJID,
			"from_phone_jid": msg.FromPhoneJID, // LID 對應的 PhoneJID
			"to_phone_jid":   msg.ToPhoneJID,   // LID 對應的 PhoneJID
			"content":        msg.Content,
			"original_text":  msg.OriginalText,
			"type":           msg.Type,
			"media_url":      msg.MediaURL,
			"is_from_me":     msg.IsFromMe,
			"timestamp":      msg.Timestamp.Format(time.RFC3339),
			"created_at":     msg.CreatedAt.Format(time.RFC3339),
		}
		realtime.BroadcastMessage(accountID, messageData)
		realtime.BroadcastSSE(accountID, "new_message", messageData) // SSE 廣播
	})

	// 設定 Gateway 通用事件廣播回調（SSE）
	a.services.Gateway.SetEventBroadcast(func(accountID uint, eventType string, data interface{}) {
		realtime.BroadcastSSE(accountID, eventType, data)
	})

	// 啟動 EventConsumer
	go func() {
		ctx := context.Background()
		if err := a.services.Gateway.Start(ctx); err != nil {
			logger.Errorw("Connector Gateway 啟動失敗", "error", err)
		}
	}()
	logger.Info("Connector Gateway EventConsumer 已啟動")

	// 啟動 Analysis WorkerPool
	if a.services.AnalysisWorkerPool != nil {
		a.services.AnalysisWorkerPool.Start()
		logger.Info("Analysis WorkerPool 已啟動")
	}

	// 系統啟動時異步批量更新帳號用戶資料（透過 Gateway）
	go func() {
		time.Sleep(5 * time.Second)
		logger.Info("開始批量更新帳號用戶資料...")

		// 查詢需要更新的帳號（已連線且缺失資料）
		var accounts []model.WhatsAppAccount
		if err := a.db.GetDB().Where(
			"status = ? AND (push_name = '' OR push_name IS NULL OR avatar = '' OR avatar IS NULL)",
			"connected",
		).Find(&accounts).Error; err != nil {
			logger.Errorw("查詢待更新帳號失敗", "error", err)
			return
		}

		if len(accounts) == 0 {
			logger.Info("沒有需要更新資料的帳號")
			return
		}

		logger.Infow("找到需要更新資料的帳號", "count", len(accounts))

		ctx := context.Background()
		successCount := 0
		for _, account := range accounts {
			if err := a.services.Gateway.UpdateAccountProfile(ctx, account.ID); err != nil {
				logger.Warnw("帳號更新資料請求失敗", "account_id", account.ID, "error", err)
			} else {
				successCount++
			}
			// 避免速率限制
			time.Sleep(500 * time.Millisecond)
		}

		logger.Infow("批量更新帳號用戶資料完成", "success_count", successCount, "total", len(accounts))
	}()

	logger.Info("頭像自動同步已暫停 (避免觸發 WhatsApp 速率限制)")
}

// initCronScheduler 初始化定時任務調度器
func (a *App) initCronScheduler() {
	a.cronScheduler = cron.New()

	// 添加每天 0 點更新聯繫人頭像的定時任務 (已暫停 - 避免觸發 WhatsApp 速率限制)
	// _, err := a.cronScheduler.AddFunc("0 0 * * *", func() {
	// 	logger.Info("定時任務: 開始每日批量更新聯繫人頭像...")
	// 	if wsService, ok := a.services.WhatsApp.(interface{ UpdateAllContactsAvatars() error }); ok {
	// 		if err := wsService.UpdateAllContactsAvatars(); err != nil {
	// 			logger.Errorf("定時任務: 批量更新聯繫人頭像失敗: %v", err)
	// 		} else {
	// 			logger.Info("定時任務: 批量更新聯繫人頭像完成")
	// 		}
	// 	}
	// })
	// if err != nil {
	// 	logger.Errorf("添加聯繫人頭像定時任務失敗: %v", err)
	// } else {
	// 	logger.Info("聯繫人頭像定時任務已註冊 (每天00:00執行)")
	// }
	logger.Info("每日頭像定時任務已暫停 (避免觸發 WhatsApp 速率限制)")

	// 每 5 分鐘同步敏感詞標籤到聊天室
	_, err := a.cronScheduler.AddFunc("*/5 * * * *", func() {
		logger.Info("定時任務: 開始同步聊天室標籤...")
		if err := a.services.ChatTag.SyncFromSensitiveWordAlerts(); err != nil {
			logger.Errorw("定時任務: 同步聊天室標籤失敗", "error", err)
		} else {
			logger.Info("定時任務: 同步聊天室標籤完成")
		}
	})
	if err != nil {
		logger.Errorw("新增聊天室標籤同步定時任務失敗", "error", err)
	} else {
		logger.Info("聊天室標籤同步定時任務已註冊 (每5分鐘執行)")
	}

	// AI 聊天室標籤分析（開關從 system_configs 動態讀取）
	if a.services.ChatAIAnalysis != nil {
		a.aiAnalysisDone = make(chan struct{})
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			defer close(a.aiAnalysisDone)
			for {
				select {
				case <-ticker.C:
					enabled, _ := a.services.Config.GetConfig("ai_analysis.enabled")
					if enabled != "true" {
						continue
					}
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
					if err := a.services.ChatAIAnalysis.RunAnalysis(ctx); err != nil {
						logger.Errorw("AI 聊天室分析失敗", "error", err)
					}
					cancel()
				case <-a.aiAnalysisStop:
					return
				}
			}
		}()
		logger.Info("AI 聊天室分析排程已啟動")
	}

	// 啟動 cron 調度器
	a.cronScheduler.Start()
	logger.Info("Cron 定時任務調度器已啟動")
}

// Start 啟動應用
func (a *App) Start() error {
	addr := fmt.Sprintf("%s:%d", a.config.Server.Host, a.config.Server.Port)
	logger.Infow("服務器啟動", "addr", addr)
	return a.router.Run(addr)
}

// Stop 停止應用
func (a *App) Stop() {
	logger.Info("正在停止應用...")

	// 停止 Analysis WorkerPool
	if a.services.AnalysisWorkerPool != nil {
		a.services.AnalysisWorkerPool.Stop()
		logger.Info("Analysis WorkerPool 已停止")
	}

	// 停止 Connector Gateway
	if a.services.Gateway != nil {
		a.services.Gateway.Stop()
		logger.Info("Connector Gateway 已停止")
	}

	// 停止 AI 分析 ticker
	if a.aiAnalysisDone != nil {
		close(a.aiAnalysisStop)
		<-a.aiAnalysisDone
		logger.Info("AI 聊天室分析已停止")
	}

	// 停止 cron 調度器
	if a.cronScheduler != nil {
		a.cronScheduler.Stop()
		logger.Info("Cron 調度器已停止")
	}

	// 關閉 Redis
	if a.redis != nil {
		if err := a.redis.Close(); err != nil {
			logger.Errorw("關閉 Redis 連接失敗", "error", err)
		} else {
			logger.Info("Redis 連接已關閉")
		}
	}

	// 關閉數據庫
	if a.db != nil {
		if err := a.db.Close(); err != nil {
			logger.Errorw("關閉數據庫連接失敗", "error", err)
		} else {
			logger.Info("數據庫連接已關閉")
		}
	}

	logger.Info("應用已停止")
}
