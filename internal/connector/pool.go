package connector

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"whatsapp_golang/internal/config"
	"whatsapp_golang/internal/connector/whatsmeow"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
	_ "github.com/lib/pq"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"gorm.io/gorm"
)

// 分布式鎖相關常量
const (
	connectorLockPrefix       = "wa:connector:lock:"
	connectorLockTTL          = 120 * time.Second // 鎖的 TTL（增加以容忍高負載情況）
	lockRenewalInterval       = 20 * time.Second  // 續約間隔（TTL/6，提供 6 次續約機會）
	lockRenewalMaxFailures    = 3                 // 連續失敗幾次才停止 Connector
	lockRetryInterval         = 2 * time.Second
	lockMaxRetries            = 65 // 最多等 130 秒（需 > connectorLockTTL，確保舊鎖過期後能取得）
)

// Pool 管理多個 in-process Connector
type Pool struct {
	mu         sync.RWMutex
	connectors map[string]*Connector // connectorID -> Connector

	db         *gorm.DB
	redis      *redis.Client
	cfg        *PoolConfig
	instanceID string // 本實例的唯一識別碼，用於分布式鎖

	// Shared whatsmeow session store (owned by Pool, closed in StopAll)
	sessionDB *sql.DB
	container *sqlstore.Container
}

// PoolConfig Pool 配置
type PoolConfig struct {
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
	DatabaseURL       string
	MediaDir          string
	HeartbeatInterval time.Duration
	Version           string
}

// NewPoolConfigFromAppConfig 從應用配置建立 PoolConfig
func NewPoolConfigFromAppConfig(cfg *config.Config) *PoolConfig {
	return &PoolConfig{
		RedisAddr:         cfg.Redis.Addr,
		RedisPassword:     cfg.Redis.Password,
		RedisDB:           cfg.Redis.DB,
		DatabaseURL:       cfg.Database.PostgreSQL.BuildDSN(),
		MediaDir:          cfg.WhatsApp.MediaDir,
		HeartbeatInterval: 30 * time.Second,
		Version:           config.GetConnectorVersion(),
	}
}

// NewPool 建立 ConnectorPool（初始化共享 session store）
func NewPool(db *gorm.DB, redis *redis.Client, cfg *PoolConfig) (*Pool, error) {
	hostname, _ := os.Hostname()
	instanceID := fmt.Sprintf("%s-%d-%d", hostname, os.Getpid(), time.Now().UnixNano())

	// Initialize shared session DB
	dbLog := waLog.Stdout("WhatsApp-Session", "INFO", true)
	sessionDB, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("初始化共享 session DB 失敗: %w", err)
	}
	sessionDB.SetMaxOpenConns(30)
	sessionDB.SetMaxIdleConns(15)
	sessionDB.SetConnMaxLifetime(30 * time.Minute)

	container := sqlstore.NewWithDB(sessionDB, "postgres", dbLog)
	if err := container.Upgrade(context.Background()); err != nil {
		sessionDB.Close()
		return nil, fmt.Errorf("升級 WhatsApp session store 失敗: %w", err)
	}

	return &Pool{
		connectors: make(map[string]*Connector),
		db:         db,
		redis:      redis,
		cfg:        cfg,
		instanceID: instanceID,
		sessionDB:  sessionDB,
		container:  container,
	}, nil
}

// Start 啟動指定的 Connector（使用分布式鎖防止多實例競爭）
func (p *Pool) Start(ctx context.Context, connectorID string) error {
	// 先檢查本地是否已在運行
	p.mu.RLock()
	if _, exists := p.connectors[connectorID]; exists {
		p.mu.RUnlock()
		return fmt.Errorf("Connector %s 已在本實例運行中", connectorID)
	}
	p.mu.RUnlock()

	// 嘗試獲取分布式鎖
	lockKey := connectorLockPrefix + connectorID
	acquired, err := p.tryAcquireLock(ctx, lockKey)
	if err != nil {
		return fmt.Errorf("獲取分布式鎖失敗: %w", err)
	}
	if !acquired {
		return fmt.Errorf("Connector %s 已在其他實例上運行", connectorID)
	}

	// 確保失敗時釋放鎖
	success := false
	defer func() {
		if !success {
			p.releaseLock(ctx, lockKey)
		}
	}()

	// 從資料庫取得配置
	var connectorConfig model.ConnectorConfig
	if err := p.db.WithContext(ctx).
		Preload("ProxyConfig").
		Where("id = ? AND deleted_at IS NULL", connectorID).
		First(&connectorConfig).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("Connector 配置 %s 不存在", connectorID)
		}
		return fmt.Errorf("取得 Connector 配置失敗: %w", err)
	}

	// 建立 Connector 配置
	cfg := &Config{
		ConnectorID:       connectorID,
		MediaDir:          p.cfg.MediaDir,
		HeartbeatInterval: p.cfg.HeartbeatInterval,
		Version:           p.cfg.Version,
	}

	// 建立 Connector（注入共享資源）
	deps := &SharedDeps{
		Redis:     p.redis,
		Container: p.container,
		DB:        p.db,
	}
	connector, err := NewConnector(cfg, &connectorConfig, deps)
	if err != nil {
		return fmt.Errorf("建立 Connector 失敗: %w", err)
	}

	// 在啟動過程中就開始續約，避免啟動時間過長導致鎖過期
	startupCtx, cancelStartupRenewal := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(lockRenewalInterval)
		defer ticker.Stop()
		for {
			select {
			case <-startupCtx.Done():
				return
			case <-ticker.C:
				if err := p.redis.Expire(context.Background(), lockKey, connectorLockTTL).Err(); err != nil {
					logger.Warnw("啟動期間續約鎖失敗", "connector_id", connectorID, "error", err)
				}
			}
		}
	}()

	// 啟動 Connector
	if err := connector.Start(ctx); err != nil {
		cancelStartupRenewal()
		return fmt.Errorf("啟動 Connector 失敗: %w", err)
	}

	// 停止啟動期間的續約，改由正式的 lockRenewalLoop 接手
	cancelStartupRenewal()

	// 加入本地管理
	p.mu.Lock()
	p.connectors[connectorID] = connector
	p.mu.Unlock()

	// 啟動鎖續約 goroutine
	go p.lockRenewalLoop(connectorID, lockKey)

	// 更新資料庫狀態（記錄運行實例）
	if err := p.db.WithContext(ctx).Model(&model.ConnectorConfig{}).
		Where("id = ?", connectorID).
		Updates(map[string]interface{}{
			"status":     model.ConnectorStatusRunning,
			"error_msg":  "",
			"updated_at": time.Now(),
		}).Error; err != nil {
		logger.Warnw("更新 Connector 狀態失敗", "connector_id", connectorID, "error", err)
	}

	success = true // 標記成功，避免 defer 釋放鎖
	logger.Infow("Connector 已啟動", "connector_id", connectorID, "instance_id", p.instanceID)
	return nil
}

// Stop 停止指定的 Connector（並釋放分布式鎖，更新狀態為 stopped）
// 此方法用於用戶主動停止 Connector
func (p *Pool) Stop(ctx context.Context, connectorID string) error {
	return p.stopConnector(ctx, connectorID, true)
}

// stopWithoutStatusUpdate 停止 Connector 但不更新資料庫狀態
// 此方法用於 API graceful shutdown，保持狀態為 running 以便下次啟動時自動恢復
func (p *Pool) stopWithoutStatusUpdate(ctx context.Context, connectorID string) error {
	return p.stopConnector(ctx, connectorID, false)
}

// stopConnector 停止 Connector 的內部實現
func (p *Pool) stopConnector(ctx context.Context, connectorID string, updateStatus bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	connector, exists := p.connectors[connectorID]
	if !exists {
		// 即使 Connector 不存在也要釋放鎖（清理殘留）
		lockKey := connectorLockPrefix + connectorID
		p.releaseLock(ctx, lockKey)
		return fmt.Errorf("Connector %s 不在運行中", connectorID)
	}

	// 先釋放 Redis 鎖再停止 Connector（Stop 可能耗時數十秒）
	// p.mu 已保護進程內併發，提前釋放讓新實例可以儘快取得鎖
	lockKey := connectorLockPrefix + connectorID
	p.releaseLock(ctx, lockKey)

	// 停止 Connector
	connector.Stop(ctx)
	delete(p.connectors, connectorID)

	// 更新資料庫狀態（僅在用戶主動停止時）
	if updateStatus {
		if err := p.db.WithContext(ctx).Model(&model.ConnectorConfig{}).
			Where("id = ?", connectorID).
			Updates(map[string]interface{}{
				"status":     model.ConnectorStatusStopped,
				"updated_at": time.Now(),
			}).Error; err != nil {
			logger.Warnw("更新 Connector 狀態失敗", "connector_id", connectorID, "error", err)
		}
	}

	logger.Infow("Connector 已停止", "connector_id", connectorID)
	return nil
}

// Restart 重啟指定的 Connector
func (p *Pool) Restart(ctx context.Context, connectorID string) error {
	// 先停止（忽略未運行的錯誤）
	_ = p.Stop(ctx, connectorID)

	// 再啟動
	return p.Start(ctx, connectorID)
}

// Get 取得指定的 Connector
func (p *Pool) Get(connectorID string) *Connector {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.connectors[connectorID]
}

// List 列出所有運行中的 Connector 狀態
func (p *Pool) List() []*ConnectorStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]*ConnectorStatus, 0, len(p.connectors))
	for id, c := range p.connectors {
		result = append(result, &ConnectorStatus{
			ID:           id,
			AccountCount: c.GetAccountCount(),
			AccountIDs:   c.GetAccountIDs(),
			Uptime:       c.GetUptime(),
			StartTime:    c.GetStartTime(),
		})
	}
	return result
}

// ConnectorStatus Connector 狀態
type ConnectorStatus struct {
	ID           string
	AccountCount int
	AccountIDs   []uint
	Uptime       time.Duration
	StartTime    time.Time
}

// IsRunning 檢查指定的 Connector 是否正在運行
func (p *Pool) IsRunning(connectorID string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, exists := p.connectors[connectorID]
	return exists
}

// RestoreAll 恢復所有狀態為 running 的 Connector
func (p *Pool) RestoreAll(ctx context.Context) error {
	var configs []model.ConnectorConfig
	if err := p.db.WithContext(ctx).
		Preload("ProxyConfig").
		Where("status = ? AND deleted_at IS NULL", model.ConnectorStatusRunning).
		Find(&configs).Error; err != nil {
		return fmt.Errorf("查詢運行中的 Connector 失敗: %w", err)
	}

	if len(configs) == 0 {
		logger.Infow("沒有需要恢復的 Connector")
		return nil
	}

	logger.Infow("開始恢復 Connector", "count", len(configs))

	var successCount, failCount int
	for _, cfg := range configs {
		var lastErr error
		for attempt := 0; attempt <= lockMaxRetries; attempt++ {
			lastErr = p.Start(ctx, cfg.ID)
			if lastErr == nil {
				break
			}
			// 非鎖競爭錯誤，不重試
			if !strings.Contains(lastErr.Error(), "已在其他實例上運行") {
				break
			}
			if attempt < lockMaxRetries {
				logger.Infow("等待舊實例釋放鎖", "connector_id", cfg.ID, "attempt", attempt+1, "max", lockMaxRetries)
				time.Sleep(lockRetryInterval)
			}
		}
		if lastErr != nil {
			logger.Warnw("恢復 Connector 失敗", "connector_id", cfg.ID, "error", lastErr)
			failCount++
			p.db.WithContext(ctx).Model(&model.ConnectorConfig{}).
				Where("id = ?", cfg.ID).
				Updates(map[string]interface{}{
					"status":     model.ConnectorStatusError,
					"error_msg":  lastErr.Error(),
					"updated_at": time.Now(),
				})
		} else {
			successCount++
		}
	}

	logger.Infow("Connector 恢復完成", "success_count", successCount, "fail_count", failCount)
	return nil
}

// StopAll 停止所有 Connector（用於 graceful shutdown，不更新狀態以便下次自動恢復）
func (p *Pool) StopAll(ctx context.Context) {
	p.mu.Lock()
	connectorIDs := make([]string, 0, len(p.connectors))
	for id := range p.connectors {
		connectorIDs = append(connectorIDs, id)
	}
	p.mu.Unlock()

	// 先釋放所有 Redis 鎖，讓新實例可以立即取得鎖啟動
	// 即使後續 Stop 未完成就被 SIGKILL，新實例也不必等 TTL 過期
	for _, id := range connectorIDs {
		lockKey := connectorLockPrefix + id
		p.releaseLock(ctx, lockKey)
	}
	logger.Infow("所有 Connector 鎖已釋放", "count", len(connectorIDs))

	for _, id := range connectorIDs {
		if err := p.stopWithoutStatusUpdate(ctx, id); err != nil {
			logger.Warnw("停止 Connector 失敗", "connector_id", id, "error", err)
		}
	}

	// Close shared session store (Pool owns this)
	if p.sessionDB != nil {
		p.sessionDB.Close()
		p.sessionDB = nil
		p.container = nil
	}

	logger.Infow("所有 Connector 已停止（狀態保持 running，下次啟動將自動恢復）")
}

// GetManager 取得指定 Connector 的 Manager（用於直接操作）
func (p *Pool) GetManager(connectorID string) *whatsmeow.Manager {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if c, exists := p.connectors[connectorID]; exists {
		return c.GetManager()
	}
	return nil
}

// GetActiveConnectorIDs 取得所有活動 Connector 的 ID
func (p *Pool) GetActiveConnectorIDs() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	ids := make([]string, 0, len(p.connectors))
	for id := range p.connectors {
		ids = append(ids, id)
	}
	return ids
}

// GetTotalAccountCount 取得所有 Connector 管理的帳號總數
func (p *Pool) GetTotalAccountCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	total := 0
	for _, c := range p.connectors {
		total += c.GetAccountCount()
	}
	return total
}

// FindConnectorByAccountID 根據帳號 ID 找到管理它的 Connector
func (p *Pool) FindConnectorByAccountID(accountID uint) *Connector {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, c := range p.connectors {
		if c.IsAccountManaged(accountID) {
			return c
		}
	}
	return nil
}

// BroadcastCommand 廣播命令到所有 Connector
func (p *Pool) BroadcastCommand(ctx context.Context, cmd *protocol.Command) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var lastErr error
	for id, c := range p.connectors {
		if err := c.ExecuteCommand(ctx, cmd); err != nil {
			logger.Warnw("Connector 執行命令失敗", "connector_id", id, "error", err)
			lastErr = err
		}
	}
	return lastErr
}

// ============ 分布式鎖相關方法 ============

// tryAcquireLock 嘗試獲取分布式鎖
func (p *Pool) tryAcquireLock(ctx context.Context, lockKey string) (bool, error) {
	// 使用 SET NX 原子操作嘗試獲取鎖
	result, err := p.redis.SetNX(ctx, lockKey, p.instanceID, connectorLockTTL).Result()
	if err != nil {
		return false, err
	}
	return result, nil
}

// releaseLock 釋放分布式鎖（僅當鎖由本實例持有時）
func (p *Pool) releaseLock(ctx context.Context, lockKey string) {
	// 使用 Lua 腳本確保只釋放自己持有的鎖
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		end
		return 0
	`
	if err := p.redis.Eval(ctx, script, []string{lockKey}, p.instanceID).Err(); err != nil {
		logger.Warnw("釋放分布式鎖失敗", "lock_key", lockKey, "error", err)
	}
}

// lockRenewalLoop 鎖續約迴圈（在 Connector 運行期間持續續約）
func (p *Pool) lockRenewalLoop(connectorID, lockKey string) {
	ticker := time.NewTicker(lockRenewalInterval)
	defer ticker.Stop()

	consecutiveFailures := 0

	for {
		<-ticker.C

		// 檢查 Connector 是否還在運行
		p.mu.RLock()
		_, running := p.connectors[connectorID]
		p.mu.RUnlock()

		if !running {
			logger.Debugw("Connector 已停止，結束鎖續約", "connector_id", connectorID)
			return
		}

		// 續約鎖（僅當鎖由本實例持有時）
		script := `
			if redis.call("GET", KEYS[1]) == ARGV[1] then
				return redis.call("PEXPIRE", KEYS[1], ARGV[2])
			end
			return 0
		`
		result, err := p.redis.Eval(context.Background(), script, []string{lockKey}, p.instanceID, connectorLockTTL.Milliseconds()).Result()
		if err != nil {
			consecutiveFailures++
			logger.Warnw("續約鎖失敗", "connector_id", connectorID, "consecutive_failures", consecutiveFailures, "error", err)
			if consecutiveFailures >= lockRenewalMaxFailures {
				logger.Errorw("連續續約失敗，停止 Connector", "consecutive_failures", consecutiveFailures, "connector_id", connectorID)
				p.stopConnectorAsync(connectorID)
				return
			}
			continue
		}

		// 如果續約失敗（鎖不存在或已被其他實例持有）
		if result.(int64) == 0 {
			consecutiveFailures++

			// 檢查鎖的實際狀態以提供更精確的錯誤訊息
			holder, getErr := p.redis.Get(context.Background(), lockKey).Result()
			if getErr == redis.Nil {
				logger.Warnw("鎖已過期消失", "connector_id", connectorID, "consecutive_failures", consecutiveFailures)
			} else if getErr != nil {
				logger.Warnw("檢查鎖狀態失敗", "connector_id", connectorID, "error", getErr)
			} else {
				logger.Warnw("鎖已被其他實例持有", "connector_id", connectorID, "holder", holder, "consecutive_failures", consecutiveFailures)
			}

			if consecutiveFailures >= lockRenewalMaxFailures {
				logger.Errorw("連續續約失敗，停止 Connector", "consecutive_failures", consecutiveFailures, "connector_id", connectorID)
				p.stopConnectorAsync(connectorID)
				return
			}
			continue
		}

		// 續約成功，重置失敗計數
		if consecutiveFailures > 0 {
			logger.Infow("鎖續約恢復正常", "connector_id", connectorID, "previous_failures", consecutiveFailures)
		}
		consecutiveFailures = 0
	}
}

// stopConnectorAsync 非同步停止 Connector
func (p *Pool) stopConnectorAsync(connectorID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := p.Stop(ctx, connectorID); err != nil {
			logger.Errorw("停止 Connector 失敗", "connector_id", connectorID, "error", err)
		}
	}()
}

// IsLockHolder 檢查本實例是否持有指定 Connector 的鎖
func (p *Pool) IsLockHolder(ctx context.Context, connectorID string) bool {
	lockKey := connectorLockPrefix + connectorID
	holder, err := p.redis.Get(ctx, lockKey).Result()
	if err != nil {
		return false
	}
	return holder == p.instanceID
}
