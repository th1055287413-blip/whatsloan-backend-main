package whatsapp

import (
	"context"
	"fmt"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
)

// SyncTaskHandlerImpl 實作 SyncTaskHandler 介面
type SyncTaskHandlerImpl struct {
	service *whatsappService
}

// NewSyncTaskHandler 建立同步任務處理器
func NewSyncTaskHandler(service *whatsappService) *SyncTaskHandlerImpl {
	return &SyncTaskHandlerImpl{service: service}
}

// checkClientReady 檢查帳號客戶端是否已連接且登入
// 返回 nil 表示已就緒，返回 error 表示未就緒（並已標記斷線）
func (h *SyncTaskHandlerImpl) checkClientReady(ctx context.Context, accountID uint, step model.SyncStepType) error {
	h.service.mu.RLock()
	client, exists := h.service.clients[accountID]
	h.service.mu.RUnlock()

	if !exists || !client.IsConnected() || !client.IsLoggedIn() {
		if h.service.syncQueue != nil {
			h.service.syncQueue.MarkAccountDisconnected(ctx, accountID)
		}
		h.service.syncStatusService.MarkFailed(accountID, step, "帳號未連接")
		return fmt.Errorf("帳號 %d 未連接", accountID)
	}
	return nil
}

// HandleAccountConnect 處理帳號連接任務
func (h *SyncTaskHandlerImpl) HandleAccountConnect(ctx context.Context, accountID uint) error {
	log := logger.WithAccount(accountID)
	log.Infow("開始處理帳號連接任務")

	// 標記為執行中
	h.service.syncStatusService.MarkRunning(accountID, model.SyncStepConnect)

	// 檢查帳號狀態
	var account model.WhatsAppAccount
	if err := h.service.db.GetDB().First(&account, accountID).Error; err != nil {
		log.Errorw("帳號獲取失敗", "error", err)
		h.service.syncStatusService.MarkFailed(accountID, model.SyncStepConnect, err.Error())
		return fmt.Errorf("獲取帳號失敗: %w", err)
	}

	log.Debugw("帳號當前狀態", "phone", account.PhoneNumber, "status", account.Status)

	if account.Status == "logged_out" {
		log.Infow("帳號狀態為 logged_out，跳過連接")
		h.service.syncStatusService.MarkCompleted(accountID, model.SyncStepConnect, nil)
		return nil
	}

	// 執行連接
	log.Debugw("開始執行 ConnectAccount")
	if err := h.service.ConnectAccount(accountID); err != nil {
		log.Errorw("帳號連接失敗", "error", err)
		h.service.syncStatusService.MarkFailed(accountID, model.SyncStepConnect, err.Error())
		return fmt.Errorf("連接帳號失敗: %w", err)
	}

	log.Infow("帳號連接成功")
	h.service.syncStatusService.MarkCompleted(accountID, model.SyncStepConnect, nil)

	// 連接成功，取消斷線標記
	if h.service.syncQueue != nil {
		h.service.syncQueue.UnmarkAccountDisconnected(ctx, accountID)
	}

	// 使用 SyncController 統一控制同步行為
	// TestMode 開關在 sync_config.go 中統一配置
	if h.service.syncController != nil {
		// 連接成功後，直接入隊聊天同步任務（不再延遲）
		if err := h.service.syncController.RequestSync(ctx, accountID, SyncTypeChat); err != nil {
			log.Errorw("入隊聊天同步任務失敗", "error", err)
		}
	}

	return nil
}

// HandleChatSync 處理聊天列表同步任務
func (h *SyncTaskHandlerImpl) HandleChatSync(ctx context.Context, accountID uint) error {
	log := logger.WithAccount(accountID)
	log.Infow("開始處理聊天同步任務")

	// 標記為執行中
	h.service.syncStatusService.MarkRunning(accountID, model.SyncStepChat)

	h.service.mu.RLock()
	client, exists := h.service.clients[accountID]
	h.service.mu.RUnlock()

	if !exists {
		log.Warnw("客戶端不存在，跳過聊天同步")
		h.service.syncStatusService.MarkFailed(accountID, model.SyncStepChat, "客戶端不存在")
		return fmt.Errorf("帳號 %d 未連接", accountID)
	}

	isConnected := client.IsConnected()
	isLoggedIn := client.IsLoggedIn()
	log.Debugw("客戶端狀態", "is_connected", isConnected, "is_logged_in", isLoggedIn)

	if !isConnected || !isLoggedIn {
		log.Warnw("未連接或未登入，跳過聊天同步")
		h.service.syncStatusService.MarkFailed(accountID, model.SyncStepChat, "未連接或未登入")
		return fmt.Errorf("帳號 %d 未連接", accountID)
	}

	// 同步聊天列表
	log.Debugw("開始執行 syncChatsFromWhatsApp")
	h.service.syncChatsFromWhatsApp(accountID)
	log.Debugw("syncChatsFromWhatsApp 完成")

	// 獲取同步的聊天數量
	chats, _ := h.service.GetChats(accountID)
	chatCount := len(chats)

	// 標記完成
	h.service.syncStatusService.MarkCompleted(accountID, model.SyncStepChat, map[string]interface{}{
		"chat_sync_count": chatCount,
	})

	// 同步完成後，直接將歷史訊息同步任務加入隊列（由限速器控制執行速度）
	if h.service.syncQueue != nil {
		h.enqueueHistorySyncTasks(accountID)
	}

	return nil
}

// enqueueHistorySyncTasks 將每個聊天的歷史同步任務加入隊列
func (h *SyncTaskHandlerImpl) enqueueHistorySyncTasks(accountID uint) {
	log := logger.WithAccount(accountID)

	// 檢查帳號是否仍然連接
	h.service.mu.RLock()
	client, exists := h.service.clients[accountID]
	h.service.mu.RUnlock()

	if !exists || !client.IsConnected() || !client.IsLoggedIn() {
		log.Warnw("帳號未連接，跳過歷史同步任務入隊")
		return
	}

	// 檢查是否已有歷史同步任務在隊列中或執行中，避免重複入隊
	status, err := h.service.syncStatusService.GetByAccountID(accountID)
	if err != nil {
		log.Errorw("獲取同步狀態失敗", "error", err)
		return
	}
	if status != nil && (status.HistorySyncStatus == model.SyncStateQueued || status.HistorySyncStatus == model.SyncStateRunning) {
		log.Warnw("歷史同步任務已在隊列中或執行中，跳過重複入隊",
			"status", status.HistorySyncStatus)
		return
	}

	chats, err := h.service.GetChats(accountID)
	if err != nil {
		log.Errorw("獲取聊天列表失敗", "error", err)
		return
	}

	totalChats := len(chats)
	if totalChats == 0 {
		log.Infow("沒有聊天需要同步歷史訊息，直接進入聯絡人同步")
		// 沒有聊天，直接標記歷史同步完成並入隊聯絡人同步
		h.service.syncStatusService.MarkCompleted(accountID, model.SyncStepHistory, nil)
		if h.service.syncQueue != nil {
			h.service.syncStatusService.MarkQueued(accountID, model.SyncStepContact)
			h.service.syncQueue.EnqueueContactSync(h.service.syncQueueCtx, accountID)
		}
		return
	}

	// 標記歷史同步為已入隊
	h.service.syncStatusService.MarkQueued(accountID, model.SyncStepHistory)

	// 全部聊天都入隊，讓限速隊列自己控制執行速度
	log.Infow("開始入隊歷史同步任務", "total_chats", totalChats)

	for i, chat := range chats {
		// 每個聊天同步 100 則訊息，帶上索引和總數
		if err := h.service.syncQueue.EnqueueHistorySync(h.service.syncQueueCtx, accountID, chat.JID, 100, i, totalChats); err != nil {
			log.Errorw("入隊歷史同步任務失敗", "chat_jid", chat.JID, "error", err)
		}
	}

	log.Infow("歷史同步任務已全部入隊，將由限速隊列依序處理", "total_chats", totalChats)
}

// HandleHistorySync 處理歷史訊息同步任務
func (h *SyncTaskHandlerImpl) HandleHistorySync(ctx context.Context, accountID uint, chatJID string, count int, chatIndex int, totalChats int) error {
	log := logger.WithAccount(accountID)

	// 快速檢查：帳號是否已標記為斷線
	if h.service.syncQueue != nil && h.service.syncQueue.IsAccountDisconnected(ctx, accountID) {
		log.Debugw("帳號已標記為斷線，快速跳過歷史同步任務")
		return nil // 不返回錯誤，任務正常 ACK
	}

	log.Infow("限速處理歷史同步任務",
		"chat_jid", chatJID, "count", count,
		"index", chatIndex+1, "total", totalChats)

	// 標記為執行中（如果是第一個歷史同步任務）
	h.service.syncStatusService.MarkRunning(accountID, model.SyncStepHistory)

	if err := h.checkClientReady(ctx, accountID, model.SyncStepHistory); err != nil {
		return err
	}

	// 更新進度（使用任務攜帶的索引資訊）
	if totalChats > 0 {
		h.service.syncStatusService.UpdateHistoryProgress(accountID, chatIndex+1, totalChats)
	}

	// 同步該聊天的歷史訊息
	if err := h.service.SyncChatHistory(accountID, chatJID, count); err != nil {
		// 加密會話錯誤降級為警告
		log.Warnw("同步聊天歷史訊息失敗", "chat_jid", chatJID, "error", err)
		// 不返回錯誤，讓任務標記為完成
	}

	// 檢查是否為最後一個聊天（使用任務攜帶的索引資訊）
	if chatIndex == totalChats-1 {
		h.service.syncStatusService.MarkCompleted(accountID, model.SyncStepHistory, nil)
		// 歷史同步完成後，入隊聯絡人同步
		if h.service.syncQueue != nil {
			h.service.syncStatusService.MarkQueued(accountID, model.SyncStepContact)
			h.service.syncQueue.EnqueueContactSync(h.service.syncQueueCtx, accountID)
		}
	}

	log.Infow("聊天歷史訊息同步完成",
		"chat_jid", chatJID, "index", chatIndex+1, "total", totalChats)
	return nil
}

// HandleContactSync 處理聯絡人同步任務
// 注意：聯絡人由 whatsmeow 自動管理到 whatsmeow_contacts 表
// 此函數現在僅負責更新聊天名稱（從 whatsmeow_contacts 讀取）
func (h *SyncTaskHandlerImpl) HandleContactSync(ctx context.Context, accountID uint) error {
	log := logger.WithAccount(accountID)

	// 快速檢查：帳號是否已標記為斷線
	if h.service.syncQueue != nil && h.service.syncQueue.IsAccountDisconnected(ctx, accountID) {
		log.Debugw("帳號已標記為斷線，快速跳過聯絡人同步任務")
		return nil // 不返回錯誤，任務正常 ACK
	}

	log.Infow("限速處理聯絡人同步任務")

	// 標記為執行中
	h.service.syncStatusService.MarkRunning(accountID, model.SyncStepContact)

	if err := h.checkClientReady(ctx, accountID, model.SyncStepContact); err != nil {
		return err
	}

	// 聯絡人由 whatsmeow 自動同步到 whatsmeow_contacts 表，無需手動同步
	// 這裡僅更新聊天名稱（從 whatsmeow_contacts 讀取聯絡人名稱）
	h.service.updateContactNames(accountID)

	// 取得 whatsmeow_contacts 中的聯絡人數量（用於統計）
	var account model.WhatsAppAccount
	var syncCount int64
	if err := h.service.db.GetDB().Select("device_id").Where("id = ?", accountID).First(&account).Error; err == nil && account.DeviceID != "" {
		h.service.db.GetDB().Table("whatsmeow_contacts").Where("our_jid = ?", account.DeviceID).Count(&syncCount)
	}

	// 標記完成（這是最後一步，也更新 last_full_sync_at）
	h.service.syncStatusService.MarkCompleted(accountID, model.SyncStepContact, map[string]interface{}{
		"contact_sync_count": syncCount,
	})

	log.Infow("聯絡人同步完成（whatsmeow 自動管理）", "count", syncCount)
	return nil
}

// 確保 SyncTaskHandlerImpl 實作 SyncTaskHandler 介面
var _ SyncTaskHandler = (*SyncTaskHandlerImpl)(nil)
