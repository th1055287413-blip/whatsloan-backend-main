package whatsapp

import (
	"context"
	"sync"
	"time"

	"whatsapp_golang/internal/logger"
)

// SyncController 統一同步控制器
// 所有同步操作都應該通過這個控制器，確保：
// 1. 統一的測試模式控制
// 2. 防止同一帳號重複同步
// 3. 統一的限速和隊列管理
type SyncController struct {
	service *whatsappService
	config  *SyncConfig

	// 同步狀態追蹤
	mu          sync.RWMutex
	activeSyncs map[uint]map[SyncType]bool // accountID -> syncType -> isActive
}

// NewSyncController 創建同步控制器
func NewSyncController(service *whatsappService, config *SyncConfig) *SyncController {
	if config == nil {
		config = NewSyncConfig()
	}
	return &SyncController{
		service:     service,
		config:      config,
		activeSyncs: make(map[uint]map[SyncType]bool),
	}
}

// GetConfig 獲取同步配置
func (sc *SyncController) GetConfig() *SyncConfig {
	return sc.config
}

// SetTestMode 設置測試模式
func (sc *SyncController) SetTestMode(enabled bool) {
	sc.config.TestMode = enabled
	if enabled {
		logger.Warnw("測試模式已啟用，所有主動同步已停用")
	} else {
		logger.Infow("測試模式已關閉，同步功能已恢復")
	}
}

// RequestSync 請求同步（統一入口）
// 所有同步請求都應該通過這個方法，它會：
// 1. 檢查全局開關和測試模式
// 2. 檢查是否已在同步中（防止重複）
// 3. 通過 Redis 隊列排隊執行
func (sc *SyncController) RequestSync(ctx context.Context, accountID uint, syncType SyncType) error {
	log := logger.WithAccount(accountID)

	// 1. 檢查全局開關
	if !sc.config.ShouldSync() {
		log.Warnw("同步已停用，跳過",
			"test_mode", sc.config.TestMode, "enabled", sc.config.Enabled,
			"sync_type", syncType)
		return nil
	}

	// 2. 檢查帳號是否存在且已連接
	sc.service.mu.RLock()
	client, exists := sc.service.clients[accountID]
	sc.service.mu.RUnlock()

	if !exists || client == nil || !client.IsConnected() || !client.IsLoggedIn() {
		log.Debugw("帳號未連接，跳過同步", "sync_type", syncType)
		return nil
	}

	// 3. 檢查是否已在同步中（防止重複）
	if sc.isAccountSyncing(accountID, syncType) {
		log.Debugw("同步已在進行中，跳過", "sync_type", syncType)
		return nil
	}

	// 4. 根據類型分發到對應的隊列
	return sc.enqueueSync(ctx, accountID, syncType)
}

// RequestSyncForNewAccount 為新帳號請求同步（帶延遲）
func (sc *SyncController) RequestSyncForNewAccount(ctx context.Context, accountID uint) {
	log := logger.WithAccount(accountID)

	if !sc.config.ShouldSync() {
		log.Warnw("同步已停用，跳過新帳號初始同步")
		return
	}

	delay := sc.config.NewAccountSyncDelay
	log.Infow("新帳號將延遲後開始同步", "delay", delay)

	time.AfterFunc(delay, func() {
		if err := sc.RequestSync(ctx, accountID, SyncTypeChat); err != nil {
			log.Errorw("新帳號聊天同步請求失敗", "error", err)
		}
	})
}

// enqueueSync 將同步任務加入隊列
func (sc *SyncController) enqueueSync(ctx context.Context, accountID uint, syncType SyncType) error {
	log := logger.WithAccount(accountID)

	// 優先使用 Redis 隊列
	if sc.service.syncQueue != nil {
		switch syncType {
		case SyncTypeChat:
			log.Infow("聊天同步已加入 Redis 隊列")
			return sc.service.syncQueue.EnqueueChatSync(ctx, accountID)
		case SyncTypeContact:
			log.Infow("聯絡人同步已加入 Redis 隊列")
			return sc.service.syncQueue.EnqueueContactSync(ctx, accountID)
		default:
			log.Warnw("未知的同步類型", "sync_type", syncType)
			return nil
		}
	}

	// Redis 不可用時，直接執行（但仍受測試模式控制）
	log.Warnw("Redis 隊列不可用，直接執行同步", "sync_type", syncType)
	return sc.executeSyncDirectly(accountID, syncType)
}

// executeSyncDirectly 直接執行同步（Redis 不可用時的備選）
func (sc *SyncController) executeSyncDirectly(accountID uint, syncType SyncType) error {
	sc.markSyncStart(accountID, syncType)
	defer sc.markSyncEnd(accountID, syncType)

	switch syncType {
	case SyncTypeChat:
		go sc.service.syncChatsFromWhatsApp(accountID)
	case SyncTypeContact:
		go sc.service.updateContactNames(accountID)
	}
	return nil
}

// RequestHistorySync 請求歷史訊息同步（特殊處理，因為需要額外參數）
func (sc *SyncController) RequestHistorySync(ctx context.Context, accountID uint, chatJID string, count int, chatIndex int, totalChats int) error {
	if !sc.config.ShouldSync() {
		logger.WithAccount(accountID).Warnw("同步已停用，跳過歷史同步")
		return nil
	}

	if sc.service.syncQueue != nil {
		return sc.service.syncQueue.EnqueueHistorySync(ctx, accountID, chatJID, count, chatIndex, totalChats)
	}

	// 直接執行
	return sc.service.SyncChatHistory(accountID, chatJID, count)
}

// isAccountSyncing 檢查帳號是否正在進行指定類型的同步
func (sc *SyncController) isAccountSyncing(accountID uint, syncType SyncType) bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	if accountSyncs, exists := sc.activeSyncs[accountID]; exists {
		return accountSyncs[syncType]
	}
	return false
}

// markSyncStart 標記同步開始
func (sc *SyncController) markSyncStart(accountID uint, syncType SyncType) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.activeSyncs[accountID] == nil {
		sc.activeSyncs[accountID] = make(map[SyncType]bool)
	}
	sc.activeSyncs[accountID][syncType] = true
}

// markSyncEnd 標記同步結束
func (sc *SyncController) markSyncEnd(accountID uint, syncType SyncType) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if accountSyncs, exists := sc.activeSyncs[accountID]; exists {
		delete(accountSyncs, syncType)
		if len(accountSyncs) == 0 {
			delete(sc.activeSyncs, accountID)
		}
	}
}

// ClearAccountSyncState 清除帳號的同步狀態（帳號斷線時調用）
func (sc *SyncController) ClearAccountSyncState(accountID uint) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	delete(sc.activeSyncs, accountID)
}

// ShouldSyncAvatar 檢查是否應該同步頭像
func (sc *SyncController) ShouldSyncAvatar() bool {
	return sc.config.ShouldSyncAvatar()
}

// LogStatus 輸出同步控制器狀態
func (sc *SyncController) LogStatus() {
	logger.Infow("同步控制器狀態",
		"enabled", sc.config.Enabled, "test_mode", sc.config.TestMode)
	if sc.config.TestMode {
		logger.Warnw("測試模式已啟用，所有主動同步已停用")
	}
}
