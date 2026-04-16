package connector

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"whatsapp_golang/internal/connector/whatsmeow"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Connector 單個 in-process Connector 實例
type Connector struct {
	id       string
	config   *Config
	dbConfig *model.ConnectorConfig // 資料庫配置（含 ProxyConfig）
	log      *zap.SugaredLogger     // 帶有 connector_id 的結構化 logger

	redis    *redis.Client
	producer *StreamProducer
	consumer *StreamConsumer
	manager  *whatsmeow.Manager

	startTime time.Time
	stopCh    chan struct{}
	wg        sync.WaitGroup
	mu        sync.RWMutex

	// 內部 context，獨立於 HTTP request context，僅在 Stop() 時 cancel
	ctx    context.Context
	cancel context.CancelFunc
}

// SharedDeps holds shared resources managed by Pool.
// Connector holds references but does not own or close these.
type SharedDeps struct {
	Redis     *redis.Client
	Container *sqlstore.Container
	DB        *gorm.DB
}

// NewConnector 建立新的 Connector（deps 由 Pool 注入，Connector 不擁有這些資源）
func NewConnector(cfg *Config, dbConfig *model.ConnectorConfig, deps *SharedDeps) (*Connector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置驗證失敗: %w", err)
	}

	// 建立帶有 connector_id 的結構化 logger
	log := logger.WithConnector(cfg.ConnectorID)

	// 建立事件生產者
	producer := NewStreamProducer(deps.Redis, cfg.ConnectorID)

	// 建立代理配置（如果有的話）
	var proxyConfig *whatsmeow.ProxyConfig
	if dbConfig.ProxyConfig != nil && dbConfig.ProxyConfig.IsEnabled() {
		proxyConfig = &whatsmeow.ProxyConfig{
			Type:     dbConfig.ProxyConfig.Type,
			Host:     dbConfig.ProxyConfig.Host,
			Port:     dbConfig.ProxyConfig.Port,
			Username: dbConfig.ProxyConfig.Username,
			Password: dbConfig.ProxyConfig.Password,
		}

		// 測試 proxy 連線
		log.Infow("測試 proxy 連線", "host", proxyConfig.Host, "port", proxyConfig.Port)
		if err := whatsmeow.TestProxyConnection(proxyConfig, 10*time.Second); err != nil {
			return nil, fmt.Errorf("proxy 連線測試失敗: %w", err)
		}
		log.Infow("Proxy 連線測試成功")
	}

	// 建立帳號管理器（傳入代理配置和 logger）
	manager, err := whatsmeow.NewManagerWithProxy(
		cfg.ConnectorID,
		producer,
		deps.Redis,
		deps.Container,
		deps.DB,
		cfg.MediaDir,
		proxyConfig,
		log,
	)
	if err != nil {
		return nil, fmt.Errorf("建立 whatsmeow.Manager 失敗: %w", err)
	}

	// 建立命令消費者（傳入 logger）
	consumer := NewStreamConsumer(deps.Redis, cfg.ConnectorID, manager, producer, log)

	// 建立內部 context（獨立於 HTTP request context）
	internalCtx, internalCancel := context.WithCancel(context.Background())

	return &Connector{
		id:       cfg.ConnectorID,
		config:   cfg,
		dbConfig: dbConfig,
		log:      log,
		redis:    deps.Redis,
		producer: producer,
		consumer: consumer,
		manager:  manager,
		stopCh:   make(chan struct{}),
		ctx:      internalCtx,
		cancel:   internalCancel,
	}, nil
}

// Start 啟動 Connector
// 注意：傳入的 ctx 僅用於初始化操作，長期運行的 goroutines 使用內部 context
func (c *Connector) Start(ctx context.Context) error {
	c.mu.Lock()
	c.startTime = time.Now()
	c.mu.Unlock()

	// 自動恢復已登入的帳號（從 whatsmeow session store）
	// 在 register 之前執行，確保帳號連線完成後才對外可見
	if err := c.manager.RestoreAllSessions(ctx); err != nil {
		c.log.Warnw("自動恢復 session 失敗", "error", err)
	}

	// 啟動命令消費者（使用內部 context，不受 HTTP request 生命週期影響）
	// 在 register 之前啟動，避免心跳上線後命令被 ackStalePending 丟棄
	if err := c.consumer.Start(c.ctx); err != nil {
		return fmt.Errorf("啟動命令消費者失敗: %w", err)
	}

	// 最後才註冊心跳，確保 consumer 已就緒再對 API 可見
	if err := c.register(ctx); err != nil {
		return fmt.Errorf("註冊 Connector 失敗: %w", err)
	}

	// 啟動心跳（使用內部 context）
	c.wg.Add(1)
	go c.heartbeatLoop(c.ctx)

	c.log.Infow("Connector 已啟動")
	return nil
}

// Stop 停止 Connector
func (c *Connector) Stop(ctx context.Context) {
	c.log.Infow("正在停止 Connector")

	// 取消內部 context（通知所有 goroutines 停止）
	c.cancel()

	// 停止心跳（備用信號）
	close(c.stopCh)

	// 停止命令消費者
	c.consumer.Stop()

	// 關閉所有帳號連線
	c.manager.Shutdown(ctx)

	// 從 Connector 集合移除
	if err := c.unregister(ctx); err != nil {
		c.log.Warnw("取消註冊 Connector 失敗", "error", err)
	}

	// 等待所有 goroutine 結束
	c.wg.Wait()

	// 停止事件生產者，避免後續異步事件嘗試寫入已關閉的 Redis
	c.producer.Stop()

	c.log.Infow("Connector 已停止")
}

// register 註冊到 Connector 集合
func (c *Connector) register(ctx context.Context) error {
	// 加入 Connector 集合
	if err := c.redis.SAdd(ctx, protocol.ConnectorsSetKey, c.id).Err(); err != nil {
		return err
	}

	// 設定初始心跳
	heartbeatKey := protocol.GetConnectorHeartbeatKey(c.id)
	if err := c.redis.Set(ctx, heartbeatKey, time.Now().Unix(), 2*c.config.HeartbeatInterval).Err(); err != nil {
		return err
	}

	c.log.Infow("已註冊到 Connector 集合")
	return nil
}

// unregister 從 Connector 集合移除
func (c *Connector) unregister(ctx context.Context) error {
	// 從 Connector 集合移除
	if err := c.redis.SRem(ctx, protocol.ConnectorsSetKey, c.id).Err(); err != nil {
		return err
	}

	// 刪除心跳 Key
	heartbeatKey := protocol.GetConnectorHeartbeatKey(c.id)
	if err := c.redis.Del(ctx, heartbeatKey).Err(); err != nil {
		return err
	}

	c.log.Infow("已從 Connector 集合移除")
	return nil
}

// heartbeatLoop 心跳迴圈
func (c *Connector) heartbeatLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.sendHeartbeat(ctx)
		}
	}
}

// sendHeartbeat 發送心跳
func (c *Connector) sendHeartbeat(ctx context.Context) {
	// 確保在 connector set 中（自癒：若被 CleanupDeadConnector 移除，下次心跳自動恢復）
	if err := c.redis.SAdd(ctx, protocol.ConnectorsSetKey, c.id).Err(); err != nil {
		c.log.Warnw("重新加入 connector set 失敗", "error", err)
	}

	// 更新心跳 Key（設定 TTL 為心跳間隔的 3 倍）
	heartbeatKey := protocol.GetConnectorHeartbeatKey(c.id)
	if err := c.redis.Set(ctx, heartbeatKey, time.Now().Unix(), 3*c.config.HeartbeatInterval).Err(); err != nil {
		c.log.Warnw("更新心跳失敗", "error", err)
	}

	// 取得記憶體使用量
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memoryMB := int(memStats.Alloc / 1024 / 1024)

	// 發送心跳事件
	payload := &protocol.HeartbeatPayload{
		AccountCount:     c.manager.GetAccountCount(),
		AccountIDs:       c.manager.GetAccountIDs(),
		Uptime:           int64(time.Since(c.startTime).Seconds()),
		StartTime:        c.startTime.Unix(),
		MemoryMB:         memoryMB,
		Version:          c.config.Version,
		EventWorkerStats: c.manager.EventWorkerStats(),
	}

	if err := c.producer.PublishHeartbeat(ctx, payload); err != nil {
		c.log.Warnw("發送心跳事件失敗", "error", err)
	}
}

// ExecuteCommand 執行命令（直接調用，繞過 Redis Stream）
func (c *Connector) ExecuteCommand(ctx context.Context, cmd *protocol.Command) error {
	switch cmd.Type {
	case protocol.CmdConnect:
		return c.manager.HandleConnect(ctx, cmd)
	case protocol.CmdDisconnect:
		return c.manager.HandleDisconnect(ctx, cmd)
	case protocol.CmdSyncChats:
		return c.manager.HandleSyncChats(ctx, cmd)
	case protocol.CmdSyncContacts:
		return c.manager.HandleSyncContacts(ctx, cmd)
	case protocol.CmdUpdateProfile:
		return c.manager.HandleUpdateProfile(ctx, cmd)
	default:
		return fmt.Errorf("不支援的命令類型: %s", cmd.Type)
	}
}

// GetManager 取得 whatsmeow Manager
func (c *Connector) GetManager() *whatsmeow.Manager {
	return c.manager
}

// GetAccountCount 取得管理的帳號數量
func (c *Connector) GetAccountCount() int {
	return c.manager.GetAccountCount()
}

// GetAccountIDs 取得管理的帳號 ID 列表
func (c *Connector) GetAccountIDs() []uint {
	return c.manager.GetAccountIDs()
}

// IsAccountManaged 檢查帳號是否被此 Connector 管理
func (c *Connector) IsAccountManaged(accountID uint) bool {
	return c.manager.IsAccountManaged(accountID)
}

// GetUptime 取得運行時間
func (c *Connector) GetUptime() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.startTime)
}

// GetStartTime 取得啟動時間
func (c *Connector) GetStartTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.startTime
}

// GetID 取得 Connector ID
func (c *Connector) GetID() string {
	return c.id
}

// GetProxyConfig 取得代理配置
func (c *Connector) GetProxyConfig() *model.ProxyConfig {
	if c.dbConfig != nil {
		return c.dbConfig.ProxyConfig
	}
	return nil
}
