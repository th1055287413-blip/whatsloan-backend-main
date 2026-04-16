package whatsapp

import (
	"context"
	"fmt"
	"sync"
	"time"

	_ "github.com/lib/pq" // PostgreSQL 驱动
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"whatsapp_golang/internal/config"
	"whatsapp_golang/internal/database"
	"whatsapp_golang/internal/logger"

	"github.com/go-redis/redis/v8"
)

// whatsappService WhatsApp服务实现
type whatsappService struct {
	db                database.Database
	config            *config.Config
	clients           map[uint]*whatsmeow.Client
	container         *sqlstore.Container
	mu                sync.RWMutex
	reconnectAttempts map[uint]int
	reconnectMu       sync.RWMutex
	connectionManager *connectionManager
	accountLocks      *accountDBLocks // 账号级别的数据库操作锁
	// 多会话支持
	qrSessions      map[string]*qrSession
	pairingSessions map[string]*pairingSession
	sessionMu       sync.RWMutex
	// 敏感词拦截器
	interceptor MessageInterceptor
	// 同步任务队列 (Redis Stream)
	syncQueue       *SyncQueue
	syncQueueCtx    context.Context
	syncQueueCancel context.CancelFunc
	redisClient     *redis.Client
	// 同步狀態服務
	syncStatusService SyncStatusService
	// 統一同步控制器
	syncController *SyncController
	// JID 映射服務
	jidMappingService JIDMappingService
}

// connectionManager 连接管理器
type connectionManager struct {
	service      *whatsappService
	isRunning    bool
	stopChan     chan struct{}
	accountsChan chan uint
	mu           sync.RWMutex
	lastSyncTime map[uint]time.Time // 记录每个账号的最后同步时间
	syncMu       sync.RWMutex       // 同步时间的读写锁
}

// getWhatsAppLogLevel 将配置的日志级别转换为WhatsApp客户端库需要的格式
func getWhatsAppLogLevel(configLevel string) string {
	switch configLevel {
	case "debug":
		return "DEBUG"
	case "info":
		return "INFO"
	case "warn":
		return "WARN"
	case "error":
		return "ERROR"
	default:
		return "INFO"
	}
}

// NewService 创建WhatsApp服务
func NewService(db database.Database, cfg *config.Config, redisClient *redis.Client) (Service, error) {
	// 初始化WhatsApp会话数据库容器
	waLogLevel := getWhatsAppLogLevel(cfg.Log.Level)
	dbLog := waLog.Stdout("WhatsApp-Session", waLogLevel, true)

	// 使用 PostgreSQL 作为 WhatsApp 会话数据库,解决 SQLite 并发锁表问题
	var container *sqlstore.Container
	var err error

	// 使用 PostgreSQL 存储 WhatsApp 会话数据
	sessionDBPath := cfg.GetDSN()
	container, err = sqlstore.New(context.Background(), "postgres", sessionDBPath, dbLog)
	if err != nil {
		return nil, fmt.Errorf("初始化WhatsApp会话数据库(PostgreSQL)失败: %v", err)
	}
	logger.Infow("WhatsApp 會話資料庫使用 PostgreSQL")

	service := &whatsappService{
		db:                db,
		config:            cfg,
		clients:           make(map[uint]*whatsmeow.Client),
		container:         container,
		reconnectAttempts: make(map[uint]int),
		accountLocks:      newAccountDBLocks(),
		qrSessions:        make(map[string]*qrSession),
		pairingSessions:   make(map[string]*pairingSession),
		redisClient:       redisClient,
		syncStatusService: NewSyncStatusService(db.GetDB()),
		jidMappingService: NewJIDMappingService(db.GetDB()),
	}

	// 初始化統一同步控制器
	service.syncController = NewSyncController(service, NewSyncConfig())
	service.syncController.LogStatus()

	// 初始化连接管理器
	service.connectionManager = &connectionManager{
		service:      service,
		isRunning:    false,
		stopChan:     make(chan struct{}),
		accountsChan: make(chan uint, 100),
		lastSyncTime: make(map[uint]time.Time),
	}

	// 启动连接管理器
	go service.connectionManager.start()

	// 启动同步任务队列（使用 Redis Stream）
	if redisClient != nil {
		service.startSyncQueue()
	}

	// 启动时连接所有已保存的账号
	go service.connectExistingAccounts()

	return service, nil
}

// startSyncQueue 启动同步任务队列（使用 Redis Stream）
func (s *whatsappService) startSyncQueue() {
	if s.redisClient == nil {
		logger.Warnw("Redis 未配置，同步任務隊列不啟動")
		return
	}

	// 创建 SyncTaskHandler
	handler := NewSyncTaskHandler(s)

	// 配置队列：每 5 秒处理一个任务
	queueCfg := &SyncQueueConfig{
		RateLimit: 0.2, // 每 5 秒 1 个任务
		BurstSize: 1,
	}

	syncQueue, err := NewSyncQueue(s.redisClient, s.config, handler, queueCfg)
	if err != nil {
		logger.Errorw("建立同步任務隊列失敗", "error", err)
		return
	}

	s.syncQueue = syncQueue
	s.syncQueueCtx, s.syncQueueCancel = context.WithCancel(context.Background())

	// 启动消费者
	syncQueue.Start(s.syncQueueCtx)

	logger.Infow("Redis Stream 同步任務隊列已啟動，限速每 5 秒處理 1 個任務")
}

// SetInterceptor 设置消息拦截器
func (s *whatsappService) SetInterceptor(interceptor MessageInterceptor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interceptor = interceptor
}

// GetWhatsAppClient 获取指定账号的 WhatsApp 客户端
func (s *whatsappService) GetWhatsAppClient(accountID uint) (*whatsmeow.Client, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	client, exists := s.clients[accountID]
	if !exists {
		return nil, fmt.Errorf("帳號 %d 的客戶端不存在", accountID)
	}

	return client, nil
}

// StopConnectionManager 停止连接管理器
func (s *whatsappService) StopConnectionManager() {
	if s.connectionManager != nil {
		s.connectionManager.stop()
	}
}

// StopSyncQueue 停止同步任务队列
func (s *whatsappService) StopSyncQueue() {
	if s.syncQueueCancel != nil {
		s.syncQueueCancel()
	}
	if s.syncQueue != nil {
		s.syncQueue.Stop()
	}
}

// GetSyncStatusService 獲取同步狀態服務
func (s *whatsappService) GetSyncStatusService() SyncStatusService {
	return s.syncStatusService
}

// GetJIDMappingService 獲取 JID 映射服務
func (s *whatsappService) GetJIDMappingService() JIDMappingService {
	return s.jidMappingService
}
