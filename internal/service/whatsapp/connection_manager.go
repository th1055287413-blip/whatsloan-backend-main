package whatsapp

import (
	"context"
	"time"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"go.mau.fi/whatsmeow/types"
)

// start 啟動連接管理器
func (cm *connectionManager) start() {
	cm.mu.Lock()
	if cm.isRunning {
		cm.mu.Unlock()
		return
	}
	cm.isRunning = true
	cm.mu.Unlock()

	logger.Infow("連接管理器已啟動")

	connectionWatchInterval := 30 * time.Second
	ticker := time.NewTicker(connectionWatchInterval)
	defer ticker.Stop()

	syncConfig := cm.service.syncController.GetConfig()

	syncTicker := time.NewTicker(syncConfig.ChatSyncInterval)
	defer syncTicker.Stop()

	contactUpdateTicker := time.NewTicker(syncConfig.ContactUpdateInterval)
	defer contactUpdateTicker.Stop()

	var presenceChan <-chan time.Time
	if syncConfig.PresenceEnabled {
		presenceTicker := time.NewTicker(syncConfig.PresenceInterval)
		defer presenceTicker.Stop()
		presenceChan = presenceTicker.C
	}

	for {
		select {
		case <-cm.stopChan:
			logger.Infow("連接管理器已停止")
			return
		case accountID := <-cm.accountsChan:
			go cm.ensureAccountConnected(accountID)
		case <-ticker.C:
			logger.Debugw("連接管理器定期檢查所有帳號狀態")
			go cm.checkAllAccountsStatus()
		case <-syncTicker.C:
			logger.Debugw("連接管理器開始定期同步聊天列表")
			go cm.syncAllAccountsChats()
		case <-contactUpdateTicker.C:
			logger.Debugw("連接管理器開始定期更新聯絡人名稱")
			go cm.updateAllAccountsContactNames()
		case <-presenceChan:
			logger.Debugw("連接管理器開始發送在線狀態")
			go cm.sendPresenceToAllAccounts()
		}
	}
}

// stop 停止連接管理器
func (cm *connectionManager) stop() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.isRunning {
		return
	}

	cm.isRunning = false
	close(cm.stopChan)
}

// ensureAccountConnected 確保指定帳號保持連接（通過 Redis 隊列限速）
func (cm *connectionManager) ensureAccountConnected(accountID uint) {
	log := logger.WithAccount(accountID)
	log.Debugw("開始處理連接請求")

	var account model.WhatsAppAccount
	if err := cm.service.db.GetDB().Select("status, phone_number").Where("id = ?", accountID).First(&account).Error; err != nil {
		log.Errorw("獲取帳號狀態失敗", "error", err)
		return
	}
	log.Debugw("帳號資料庫狀態", "phone", account.PhoneNumber, "status", account.Status)

	if account.Status == "logged_out" {
		log.Infow("帳號狀態為 logged_out，跳過連接管理")
		return
	}

	cm.service.mu.RLock()
	client, exists := cm.service.clients[accountID]
	cm.service.mu.RUnlock()

	needsConnection := false
	if !exists {
		log.Debugw("客戶端不存在於記憶體中")
		needsConnection = true
	} else if !client.IsConnected() {
		log.Debugw("客戶端存在但未連接", "is_logged_in", client.IsLoggedIn())
		needsConnection = true
	} else if !client.IsLoggedIn() {
		log.Debugw("客戶端已連接但未登入", "is_connected", client.IsConnected())
		needsConnection = true
	} else {
		log.Debugw("連接狀態正常，無需重連")
		return
	}

	if !needsConnection {
		return
	}

	if cm.service.syncQueue != nil {
		log.Debugw("使用 Redis 隊列進行限速連接")
		if err := cm.service.syncQueue.EnqueueAccountConnect(cm.service.syncQueueCtx, accountID); err != nil {
			log.Errorw("入隊失敗", "error", err)
			cm.service.syncStatusService.MarkFailed(accountID, model.SyncStepConnect, "入隊失敗: "+err.Error())
		} else {
			cm.service.syncStatusService.MarkQueued(accountID, model.SyncStepConnect)
			log.Infow("連接請求已加入 Redis 隊列")
		}
	} else {
		log.Errorw("Redis 隊列不可用，帳號無法連接")
		cm.service.syncStatusService.MarkFailed(accountID, model.SyncStepConnect, "Redis 隊列不可用")
	}
}

// checkAllAccountsStatus 檢查所有帳號的連接狀態
func (cm *connectionManager) checkAllAccountsStatus() {
	var accounts []*model.WhatsAppAccount
	if err := cm.service.db.GetDB().Find(&accounts).Error; err != nil {
		logger.Errorw("獲取帳號列表失敗", "error", err)
		return
	}

	for _, account := range accounts {
		if account.Status == "logged_out" {
			continue
		}

		cm.service.mu.RLock()
		client, exists := cm.service.clients[account.ID]
		cm.service.mu.RUnlock()

		if !exists {
			logger.Infow("發現未連接的帳號，嘗試連接", "phone", account.PhoneNumber)
			cm.accountsChan <- account.ID
			continue
		}

		if !client.IsConnected() || !client.IsLoggedIn() {
			logger.Infow("帳號已斷開連接，嘗試重連",
				"phone", account.PhoneNumber,
				"is_connected", client.IsConnected(), "is_logged_in", client.IsLoggedIn())
			cm.accountsChan <- account.ID
			continue
		}

		logger.Infow("帳號狀態正常",
			"phone", account.PhoneNumber,
			"is_connected", client.IsConnected(), "is_logged_in", client.IsLoggedIn())

		err := cm.service.updateAccountStatus(account.ID, map[string]interface{}{
			"status":         "connected",
			"last_connected": time.Now(),
			"last_seen":      time.Now(),
		})
		if err != nil {
			logger.WithAccount(account.ID).Errorw("連接管理器更新帳號狀態失敗", "error", err)
		}
	}
}

// requestAccountConnection 請求連接指定帳號（優先使用 Redis 隊列）
func (cm *connectionManager) requestAccountConnection(accountID uint) {
	if cm.service.syncQueue != nil {
		logger.WithAccount(accountID).Infow("重連請求已加入 Redis 隊列")
		if err := cm.service.syncQueue.EnqueueAccountConnect(cm.service.syncQueueCtx, accountID); err != nil {
			logger.WithAccount(accountID).Errorw("入隊失敗", "error", err)
			select {
			case cm.accountsChan <- accountID:
			default:
				go cm.ensureAccountConnected(accountID)
			}
		}
		return
	}

	select {
	case cm.accountsChan <- accountID:
	default:
		go cm.ensureAccountConnected(accountID)
	}
}

// syncAllAccountsChats 同步所有帳號的聊天列表（通過 SyncController 統一控制）
func (cm *connectionManager) syncAllAccountsChats() {
	if cm.service.syncController == nil || !cm.service.syncController.GetConfig().ShouldSync() {
		logger.Debugw("同步已停用，跳過定期聊天同步")
		return
	}

	var accounts []model.WhatsAppAccount
	err := cm.service.db.GetDB().Where("status = ?", "connected").Find(&accounts).Error
	if err != nil {
		logger.Errorw("獲取已連接帳號列表失敗", "error", err)
		return
	}

	logger.Infow("開始定期同步帳號的聊天列表", "account_count", len(accounts))

	for _, account := range accounts {
		if cm.shouldSyncAccount(account.ID) {
			if err := cm.service.syncController.RequestSync(cm.service.syncQueueCtx, account.ID, SyncTypeChat); err != nil {
				logger.WithAccount(account.ID).Errorw("聊天同步請求失敗", "error", err)
			} else {
				cm.updateLastSyncTime(account.ID)
			}
		} else {
			logger.WithAccount(account.ID).Debugw("最近已同步，跳過")
		}
	}
}

// shouldSyncAccount 檢查帳號是否需要同步
func (cm *connectionManager) shouldSyncAccount(accountID uint) bool {
	cm.syncMu.RLock()
	lastSync, exists := cm.lastSyncTime[accountID]
	cm.syncMu.RUnlock()

	if !exists {
		return true
	}

	return time.Since(lastSync) > 25*time.Minute
}

// updateLastSyncTime 更新帳號的最後同步時間
func (cm *connectionManager) updateLastSyncTime(accountID uint) {
	cm.syncMu.Lock()
	cm.lastSyncTime[accountID] = time.Now()
	cm.syncMu.Unlock()
}

// updateAllAccountsContactNames 更新所有已連接帳號的聯絡人名稱（通過 SyncController 統一控制）
func (cm *connectionManager) updateAllAccountsContactNames() {
	if cm.service.syncController == nil || !cm.service.syncController.GetConfig().ShouldSync() {
		logger.Debugw("同步已停用，跳過定期聯絡人更新")
		return
	}

	var accounts []model.WhatsAppAccount
	err := cm.service.db.GetDB().Where("status = ?", "connected").Find(&accounts).Error
	if err != nil {
		logger.Errorw("獲取已連接帳號列表失敗", "error", err)
		return
	}

	logger.Infow("開始更新帳號的聯絡人名稱", "account_count", len(accounts))

	for _, account := range accounts {
		if err := cm.service.syncController.RequestSync(cm.service.syncQueueCtx, account.ID, SyncTypeContact); err != nil {
			logger.WithAccount(account.ID).Errorw("聯絡人同步請求失敗", "error", err)
		}
	}
}

// sendPresenceToAllAccounts 向所有帳號發送在線狀態
func (cm *connectionManager) sendPresenceToAllAccounts() {
	var accounts []*model.WhatsAppAccount
	if err := cm.service.db.GetDB().Find(&accounts).Error; err != nil {
		logger.Errorw("獲取 WhatsApp 帳號列表失敗", "error", err)
		return
	}

	for _, account := range accounts {
		go cm.sendPresenceToAccount(account.ID)
	}
}

// sendPresenceToAccount 向指定帳號發送在線狀態
func (cm *connectionManager) sendPresenceToAccount(accountID uint) {
	cm.service.mu.RLock()
	client, exists := cm.service.clients[accountID]
	cm.service.mu.RUnlock()

	if !exists || !client.IsConnected() || !client.IsLoggedIn() {
		return
	}

	err := client.SendPresence(context.Background(), types.PresenceAvailable)
	if err != nil {
		logger.WithAccount(accountID).Warnw("發送在線狀態失敗，等待 keepalive 判定連線狀態", "error", err)
	} else {
		updateErr := cm.service.updateAccountStatus(accountID, map[string]interface{}{
			"last_seen": time.Now(),
		})
		if updateErr != nil {
			logger.WithAccount(accountID).Errorw("更新在線狀態失敗", "error", updateErr)
		}
	}
}
