package gateway

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	"whatsapp_golang/internal/protocol"
	"whatsapp_golang/internal/service/whatsapp"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MessageInterceptor 訊息攔截器介面
type MessageInterceptor interface {
	CheckMessage(accountID uint, messageID, chatID, senderJID, senderName, receiverName, content string, isFromMe bool, messageTimestamp int64, toJID string, sentByAdminID *uint) error
}

// JIDMappingService JID 映射服務介面
type JIDMappingService interface {
	SaveMapping(accountID uint, senderJID, senderAltJID string) error
	GetCanonicalJID(accountID uint, jid string) string
	GetAlternativeJIDs(accountID uint, jid string) []string
	FindExistingChat(db *gorm.DB, accountID uint, jid string) *model.WhatsAppChat
	GetPhoneJID(accountID uint, jid string) string
	GetOrCreateChat(db *gorm.DB, accountID uint, jid, name string, isGroup bool) (*model.WhatsAppChat, error)
	ReconcileDuplicateChats(db *gorm.DB, accountID uint) int
	ResolvePhoneJID(jid string) string
}

// ChatSyncTrigger 觸發 Chat 同步和帳號資料更新的介面
type ChatSyncTrigger interface {
	SyncChats(ctx context.Context, accountID uint) error
	SyncChatsAsync(ctx context.Context, accountID uint) error
	UpdateAccountProfile(ctx context.Context, accountID uint) error
	UpdateAccountProfileAsync(ctx context.Context, accountID uint) error
}

// WorkgroupAutoAssigner 工作組自動分配介面
type WorkgroupAutoAssigner interface {
	AutoAssignAccount(accountID uint, channelID *uint, sourceAgentID *uint) error
}

// ReferralSessionInfo 推荐码会话信息
// ReferralSessionService 推荐码会话服务接口
type ReferralSessionService interface {
	GetReferralSession(ctx context.Context, sessionID string) (*whatsapp.ReferralSessionInfo, error)
	DeleteReferralSession(ctx context.Context, sessionID string) error
}

// WhatsAppEventHandler 處理來自 Connector 的 WhatsApp 事件
// LoginSessionReader 讀取登入會話（用於取得 channelCode）
type LoginSessionReader interface {
	GetLoginSession(ctx context.Context, sessionID string) (*LoginSession, error)
}

type WhatsAppEventHandler struct {
	db                     *gorm.DB
	gateway                ChatSyncTrigger        // 用於觸發 chat 同步
	loginSessionReader     LoginSessionReader     // 用於讀取登入會話
	referralSessionService ReferralSessionService // 用於讀取推荐码会话
	workgroupAutoAssigner  WorkgroupAutoAssigner  // 工作組自動分配
	messageInterceptor     MessageInterceptor
	jidMappingService      JIDMappingService
	messageBroadcast       func(accountID uint, message *model.WhatsAppMessage)
	eventBroadcast         func(accountID uint, eventType string, data interface{}) // 通用事件廣播
	qrCodeCallback         func(accountID uint, sessionID string, qrCode string)
	pairingCallback        func(accountID uint, sessionID string, code string)
	loginSuccessCallback   func(accountID uint, sessionID string, jid string, phoneNumber string)
	loginFailedCallback    func(accountID uint, sessionID string, reason string)
	bindAccountCallback    func(sessionID string, accountID uint) // 綁定帳號回調（通知 Connector 更新 accountID）

	ownerActivityMu         sync.Mutex
	ownerActivityLastUpdate map[uint]time.Time

	// post-connect sync debounce（避免快速重連時 goroutine 堆積）
	syncDebounce sync.Map // map[uint]time.Time

	// push_name 快取（避免每條訊息查 DB）
	pushNameCache sync.Map // map[uint]pushNameEntry
}

// NewWhatsAppEventHandler 建立事件處理器
func NewWhatsAppEventHandler(db *gorm.DB) *WhatsAppEventHandler {
	return &WhatsAppEventHandler{
		db:                      db,
		ownerActivityLastUpdate: make(map[uint]time.Time),
	}
}

// SetMessageBroadcast 設定訊息廣播回調
func (h *WhatsAppEventHandler) SetMessageBroadcast(callback func(accountID uint, message *model.WhatsAppMessage)) {
	h.messageBroadcast = callback
}

// SetEventBroadcast 設定通用事件廣播回調
func (h *WhatsAppEventHandler) SetEventBroadcast(callback func(accountID uint, eventType string, data interface{})) {
	h.eventBroadcast = callback
}

// SetQRCodeCallback 設定 QR Code 回調
func (h *WhatsAppEventHandler) SetQRCodeCallback(callback func(accountID uint, sessionID string, qrCode string)) {
	h.qrCodeCallback = callback
}

// SetPairingCallback 設定配對碼回調
func (h *WhatsAppEventHandler) SetPairingCallback(callback func(accountID uint, sessionID string, code string)) {
	h.pairingCallback = callback
}

// SetLoginSuccessCallback 設定登入成功回調
func (h *WhatsAppEventHandler) SetLoginSuccessCallback(callback func(accountID uint, sessionID string, jid string, phoneNumber string)) {
	h.loginSuccessCallback = callback
}

// SetReferralSessionService 設定推荐码会话服务
func (h *WhatsAppEventHandler) SetReferralSessionService(service ReferralSessionService) {
	h.referralSessionService = service
}

// SetWorkgroupAutoAssigner 設定工作組自動分配服務
func (h *WhatsAppEventHandler) SetWorkgroupAutoAssigner(assigner WorkgroupAutoAssigner) {
	h.workgroupAutoAssigner = assigner
}

// SetLoginFailedCallback 設定登入失敗回調
func (h *WhatsAppEventHandler) SetLoginFailedCallback(callback func(accountID uint, sessionID string, reason string)) {
	h.loginFailedCallback = callback
}

// SetMessageInterceptor 設定訊息攔截器
func (h *WhatsAppEventHandler) SetMessageInterceptor(interceptor MessageInterceptor) {
	h.messageInterceptor = interceptor
}

// SetBindAccountCallback 設定綁定帳號回調
func (h *WhatsAppEventHandler) SetBindAccountCallback(callback func(sessionID string, accountID uint)) {
	h.bindAccountCallback = callback
}

// SetJIDMappingService 設定 JID 映射服務
func (h *WhatsAppEventHandler) SetJIDMappingService(service JIDMappingService) {
	h.jidMappingService = service
}

// SetGateway 設定 Connector 閘道（用於觸發 chat 同步）
func (h *WhatsAppEventHandler) SetGateway(gateway ChatSyncTrigger) {
	h.gateway = gateway
}

// SetLoginSessionReader 設定登入會話讀取器（用於渠道綁定）
func (h *WhatsAppEventHandler) SetLoginSessionReader(reader LoginSessionReader) {
	h.loginSessionReader = reader
}

// OnMessageReceived 處理收到訊息事件
func (h *WhatsAppEventHandler) OnMessageReceived(ctx context.Context, event *protocol.Event, payload *protocol.MessageReceivedPayload) error {
	logger.Ctx(ctx).Debugw("收到訊息", "from_jid", payload.SenderJID, "content", payload.Content)

	// 儲存 LID ↔ PhoneJID 映射
	if h.jidMappingService != nil && payload.SenderAltJID != "" {
		go h.jidMappingService.SaveMapping(event.AccountID, payload.SenderJID, payload.SenderAltJID)
	}

	// 取得或建立聊天（考慮 JID 映射）
	chat, err := h.getOrCreateChatWithMapping(event.AccountID, payload.ChatJID, payload.SenderName, payload.IsGroup)
	if err != nil {
		logger.Ctx(ctx).Errorw("取得聊天失敗", "error", err)
		return err
	}

	// 建立訊息記錄
	message := &model.WhatsAppMessage{
		AccountID:     event.AccountID,
		ChatID:        chat.ID,
		MessageID:     payload.MessageID,
		FromJID:       payload.SenderJID,
		ToJID:         payload.ChatJID,
		Content:       payload.Content,
		Type:          payload.ContentType,
		MediaURL:      payload.MediaURL,
		Timestamp:     time.UnixMilli(payload.Timestamp),
		IsFromMe:      payload.IsFromMe,
		SentByAdminID: payload.SentByAdminID,
		SenderType:    payload.SenderType,
	}

	if err := h.db.Clauses(clause.OnConflict{DoNothing: true}).Create(message).Error; err != nil {
		logger.Ctx(ctx).Errorw("儲存訊息失敗", "error", err)
		return err
	}
	if message.ID == 0 {
		logger.Ctx(ctx).Debugw("訊息已存在，跳過", "message_id", payload.MessageID)
		return nil
	}

	// 更新聊天的最後訊息
	h.db.Model(&model.WhatsAppChat{}).Where("id = ?", chat.ID).Updates(map[string]interface{}{
		"last_message": truncateString(payload.Content, 255),
		"last_time":    message.Timestamp,
		"unread_count": gorm.Expr("unread_count + 1"),
	})

	// 敏感詞檢測（非阻塞）
	if h.messageInterceptor != nil {
		go func() {
			receiverName := h.getAccountPushName(event.AccountID)
			if err := h.messageInterceptor.CheckMessage(
				event.AccountID,
				payload.MessageID,
				payload.ChatJID,
				payload.SenderJID,
				payload.SenderName,
				receiverName,
				payload.Content,
				payload.IsFromMe,
				payload.Timestamp,
				payload.ChatJID,
				payload.SentByAdminID,
			); err != nil {
				logger.Warnw("敏感詞檢測失敗", "account_id", event.AccountID, "error", err)
			}
		}()
	}

	// 主人從手機發訊息 → 更新 owner activity
	if payload.IsFromMe && payload.SentByAdminID == nil && !payload.IsHistory {
		h.updateOwnerActivity(event.AccountID)
	}

	// 廣播訊息
	if h.messageBroadcast != nil {
		// 填充 PhoneJID 映射（用於前端合併 LID 與 PhoneJID 的聊天室）
		if h.jidMappingService != nil {
			message.FromPhoneJID = h.jidMappingService.GetPhoneJID(event.AccountID, message.FromJID)
			message.ToPhoneJID = h.jidMappingService.GetPhoneJID(event.AccountID, message.ToJID)
		}
		h.messageBroadcast(event.AccountID, message)
	}

	return nil
}

// OnMessageSent 處理訊息已發送事件
func (h *WhatsAppEventHandler) OnMessageSent(ctx context.Context, event *protocol.Event, payload *protocol.MessageSentPayload) error {
	logger.Ctx(ctx).Infow("訊息已發送", "message_id", payload.MessageID)

	// 更新訊息狀態（如果有追蹤命令的話）
	h.db.Model(&model.WhatsAppMessage{}).
		Where("account_id = ? AND message_id = ?", event.AccountID, payload.MessageID).
		Update("send_status", "sent")

	return nil
}

// OnReceipt 處理訊息回執事件
func (h *WhatsAppEventHandler) OnReceipt(ctx context.Context, event *protocol.Event, payload *protocol.ReceiptPayload) error {
	logger.Ctx(ctx).Debugw("收到回執", "message_id", payload.MessageID, "receipt_type", payload.ReceiptType)

	status := payload.ReceiptType // delivered, read, played
	h.db.Model(&model.WhatsAppMessage{}).
		Where("account_id = ? AND message_id = ?", event.AccountID, payload.MessageID).
		Update("send_status", status)

	// 主人已讀 → 更新 owner activity
	// read-self / played-self: 已讀回條關閉時的自讀同步
	// read + IsFromMe / played + IsFromMe: 已讀回條開啟時的自讀同步
	switch payload.ReceiptType {
	case "read-self", "played-self":
		h.updateOwnerActivity(event.AccountID)
	case "read", "played":
		if payload.IsFromMe {
			h.updateOwnerActivity(event.AccountID)
		}
	}

	return nil
}

// OnConnected 處理帳號已連線事件
func (h *WhatsAppEventHandler) OnConnected(ctx context.Context, event *protocol.Event, payload *protocol.ConnectedPayload) error {
	logger.Ctx(ctx).Infow("帳號已連線", "phone", payload.PhoneNumber, "device_id", payload.DeviceID)

	// 如果 accountID 是 0，表示是新登入尚未綁定帳號，跳過後續處理
	// 等 BindAccount 完成後會重新發送 Connected 事件
	if event.AccountID == 0 {
		logger.Ctx(ctx).Infow("帳號 ID 為 0，跳過 OnConnected 處理（等待 BindAccount）")
		return nil
	}

	updates := map[string]interface{}{
		"status":            "connected",
		"logged_out_reason": "",
		"logged_out_at":     nil,
		"last_connected":    time.Now(),
		"last_seen":         time.Now(),
		"connector_id":      event.ConnectorID,
	}

	if payload.PushName != "" {
		updates["push_name"] = payload.PushName
		h.pushNameCache.Delete(event.AccountID)
	}
	if payload.DeviceID != "" {
		updates["device_id"] = payload.DeviceID
	}
	if payload.Platform != "" {
		updates["platform"] = payload.Platform
	}
	if payload.BusinessName != "" {
		updates["business_name"] = payload.BusinessName
	}

	if err := h.db.Model(&model.WhatsAppAccount{}).Where("id = ?", event.AccountID).Updates(updates).Error; err != nil {
		logger.Ctx(ctx).Errorw("更新帳號連線狀態失敗", "account_id", event.AccountID, "error", err)
		return err
	}

	// 觸發 chat 同步和帳號資料更新（延遲執行，等待 App State 同步）
	// debounce：10 秒內重複 Connected 不再 spawn goroutine，避免快速重連時堆積
	if h.gateway != nil {
		now := time.Now()
		if last, ok := h.syncDebounce.Load(event.AccountID); ok {
			if now.Sub(last.(time.Time)) < 10*time.Second {
				logger.Ctx(ctx).Infow("跳過 post-connect sync（debounce）", "since_last", now.Sub(last.(time.Time)).Round(time.Millisecond))
				return nil
			}
		}
		h.syncDebounce.Store(event.AccountID, now)
		go h.triggerPostConnectSync(context.Background(), event.AccountID)
	}

	return nil
}

// syncCooldown 同步冷卻時間（短時間重啟不重複同步）
const syncCooldown = 3 * time.Minute

// triggerPostConnectSync 延遲觸發連線後的同步任務
// 使用非阻塞命令發送，避免大量帳號同時連線時命令排隊超時
func (h *WhatsAppEventHandler) triggerPostConnectSync(ctx context.Context, accountID uint) {
	// 延遲 10 秒等待 WhatsApp App State 同步完成（包括 PUSH_NAME 事件）
	time.Sleep(10 * time.Second)

	// 檢查上次同步時間，若不到冷卻時間則跳過
	var account model.WhatsAppAccount
	if err := h.db.Select("last_sync_at").Where("id = ?", accountID).First(&account).Error; err != nil {
		logger.Ctx(ctx).Warnw("查詢帳號同步時間失敗", "error", err)
	} else if account.LastSyncAt != nil && time.Since(*account.LastSyncAt) < syncCooldown {
		logger.Ctx(ctx).Infow("跳過同步", "since_last_sync", time.Since(*account.LastSyncAt).Round(time.Second))
		return
	}

	// 1. 觸發帳號資料更新（獲取主帳號頭像）- 非阻塞
	logger.Ctx(ctx).Infow("觸發帳號資料更新")
	if err := h.gateway.UpdateAccountProfileAsync(ctx, accountID); err != nil {
		logger.Ctx(ctx).Warnw("觸發帳號資料更新失敗", "error", err)
	}

	// 2. 觸發 chat 同步（獲取聯絡人和群組名稱）- 非阻塞
	logger.Ctx(ctx).Infow("觸發 chat 同步")
	if err := h.gateway.SyncChatsAsync(ctx, accountID); err != nil {
		logger.Ctx(ctx).Warnw("觸發 chat 同步失敗", "error", err)
	}

	// 更新同步時間
	now := time.Now()
	if err := h.db.Model(&model.WhatsAppAccount{}).Where("id = ?", accountID).Update("last_sync_at", now).Error; err != nil {
		logger.Ctx(ctx).Warnw("更新同步時間失敗", "error", err)
	}
}

// OnDisconnected 處理帳號已斷線事件
func (h *WhatsAppEventHandler) OnDisconnected(ctx context.Context, event *protocol.Event, payload *protocol.DisconnectedPayload) error {
	logger.Ctx(ctx).Warnw("帳號已斷線", "reason", payload.Reason)

	now := time.Now()
	if err := h.db.Model(&model.WhatsAppAccount{}).Where("id = ?", event.AccountID).Updates(map[string]interface{}{
		"status":            "disconnected",
		"logged_out_reason": payload.Reason,
		"logged_out_at":     now,
		"last_seen":         now,
	}).Error; err != nil {
		logger.Ctx(ctx).Errorw("更新帳號斷線狀態失敗", "account_id", event.AccountID, "error", err)
		return err
	}

	return nil
}

// OnLoggedOut 處理帳號已登出事件
func (h *WhatsAppEventHandler) OnLoggedOut(ctx context.Context, event *protocol.Event, payload *protocol.LoggedOutPayload) error {
	logger.Ctx(ctx).Warnw("帳號已登出", "reason", payload.Reason)

	now := time.Now()
	if err := h.db.Model(&model.WhatsAppAccount{}).Where("id = ?", event.AccountID).Updates(map[string]interface{}{
		"status":            "logged_out",
		"logged_out_reason": payload.Reason,
		"logged_out_at":     now,
		"last_seen":         now,
	}).Error; err != nil {
		logger.Ctx(ctx).Errorw("更新帳號登出狀態失敗", "account_id", event.AccountID, "error", err)
		return err
	}

	return nil
}

// OnQRCode 處理 QR Code 事件
func (h *WhatsAppEventHandler) OnQRCode(ctx context.Context, event *protocol.Event, payload *protocol.QRCodePayload) error {
	logger.Ctx(ctx).Infow("QR Code 已生成", "session_id", payload.SessionID)

	if h.qrCodeCallback != nil {
		h.qrCodeCallback(event.AccountID, payload.SessionID, payload.QRCode)
	}

	return nil
}

// OnPairingCode 處理配對碼事件
func (h *WhatsAppEventHandler) OnPairingCode(ctx context.Context, event *protocol.Event, payload *protocol.PairingCodePayload) error {
	logger.Ctx(ctx).Infow("配對碼已生成", "session_id", payload.SessionID, "pairing_code", payload.PairingCode)

	if h.pairingCallback != nil {
		h.pairingCallback(event.AccountID, payload.SessionID, payload.PairingCode)
	}

	return nil
}

// OnSyncProgress 處理同步進度事件
func (h *WhatsAppEventHandler) OnSyncProgress(ctx context.Context, event *protocol.Event, payload *protocol.SyncProgressPayload) error {
	logger.Ctx(ctx).Infow("同步進度", "sync_type", payload.SyncType, "current", payload.Current, "total", payload.Total)
	return nil
}

// OnSyncComplete 處理同步完成事件
func (h *WhatsAppEventHandler) OnSyncComplete(ctx context.Context, event *protocol.Event, payload *protocol.SyncCompletePayload) error {
	logger.Ctx(ctx).Infow("同步完成", "sync_type", payload.SyncType, "count", payload.Count)
	return nil
}

// OnLoginSuccess 處理登入成功事件
func (h *WhatsAppEventHandler) OnLoginSuccess(ctx context.Context, event *protocol.Event, payload *protocol.LoginSuccessPayload) error {
	logger.Ctx(ctx).Infow("登入成功", "session_id", payload.SessionID, "jid", payload.JID, "phone", payload.PhoneNumber)

	var accountID uint

	if event.AccountID == 0 {
		// 從 login session 取得渠道碼
		var channelID *uint
		var channelCode string
		if h.loginSessionReader != nil && payload.SessionID != "" {
			if session, err := h.loginSessionReader.GetLoginSession(ctx, payload.SessionID); err == nil && session != nil {
				channelCode = session.ChannelCode
			}
		}
		if channelCode != "" {
			var channel model.Channel
			if err := h.db.Where("channel_code = ? AND status = ? AND deleted_at IS NULL",
				channelCode, "enabled").First(&channel).Error; err == nil {
				channelID = &channel.ID
				logger.Ctx(ctx).Infow("渠道碼綁定渠道", "channel_code", channelCode, "channel_id", channel.ID)
			} else {
				logger.Ctx(ctx).Warnw("渠道碼無效或未啟用", "channel_code", channelCode, "error", err)
			}
		}

		// 新登入流程：創建或更新帳號
		// 先嘗試根據電話號碼查找現有帳號
		var existingAccount model.WhatsAppAccount
		result := h.db.Where("phone_number = ?", payload.PhoneNumber).First(&existingAccount)

		var isNewAccount bool
		if result.Error == nil {
			// 帳號已存在，更新狀態
			accountID = existingAccount.ID
			wasDeleted := existingAccount.AdminStatus == "deleted"
			updates := map[string]interface{}{
				"status":         "connected",
				"last_connected": time.Now(),
				"last_seen":      time.Now(),
			}
			if payload.PushName != "" {
				updates["push_name"] = payload.PushName
			}
			if payload.Platform != "" {
				updates["platform"] = payload.Platform
			}
			if payload.BusinessName != "" {
				updates["business_name"] = payload.BusinessName
			}
			// 補綁渠道（帳號尚無渠道且本次 session 有渠道碼時）
			if existingAccount.ChannelID == nil && channelID != nil {
				updates["channel_id"] = *channelID
				updates["channel_source"] = "link"
				logger.Ctx(ctx).Infow("現有帳號補綁渠道", "account_id", accountID, "channel_id", *channelID)
			}
			if err := h.db.Model(&existingAccount).Updates(updates).Error; err != nil {
				logger.Ctx(ctx).Errorw("更新現有帳號狀態失敗", "account_id", existingAccount.ID, "error", err)
				return err
			}
			logger.Ctx(ctx).Infow("更新現有帳號", "account_id", accountID, "phone", payload.PhoneNumber)

			// 恢復已刪除帳號的管理狀態
			if wasDeleted {
				h.db.Model(&existingAccount).Update("admin_status", "active")
				logger.Ctx(ctx).Infow("已恢復帳號管理狀態", "account_id", accountID)
			}
		} else {
			// 創建新帳號
			isNewAccount = true
			newAccount := &model.WhatsAppAccount{
				PhoneNumber:   payload.PhoneNumber,
				Status:        "connected",
				AdminStatus:   "active",
				LastConnected: time.Now(),
				LastSeen:      time.Now(),
				PushName:      payload.PushName,
				Platform:      payload.Platform,
				BusinessName:  payload.BusinessName,
				ChannelID:     channelID,
			}
			if channelCode != "" {
				newAccount.ChannelSource = "link"
			}
			if err := h.db.Create(newAccount).Error; err != nil {
				logger.Ctx(ctx).Errorw("創建帳號失敗", "error", err)
				return err
			}
			accountID = newAccount.ID
			logger.Ctx(ctx).Infow("創建新帳號", "account_id", accountID, "phone", payload.PhoneNumber, "channel_id", channelID)
		}

		// 处理推荐码逻辑（新帳號和現有帳號皆適用）
		var sourceAgentID *uint
		if h.referralSessionService != nil && payload.SessionID != "" {
			referralInfo, err := h.referralSessionService.GetReferralSession(ctx, payload.SessionID)
			if err != nil {
				logger.Ctx(ctx).Warnw("获取推荐码会话失败", "session_id", payload.SessionID, "error", err)
			} else if referralInfo != nil {
				sourceAgentID = referralInfo.SourceAgentID
				logger.Ctx(ctx).Infow("取得推薦碼會話", "account_id", accountID, "is_new_account", isNewAccount, "referral_code", referralInfo.ReferralCode, "source_account_id", referralInfo.SourceAccountID)
				// 检查是否已有裂变记录（防止重复）
				var existingReferral model.ReferralRegistration
				err := h.db.Where("new_account_id = ?", accountID).First(&existingReferral).Error
				if err != nil && err != gorm.ErrRecordNotFound {
					logger.Ctx(ctx).Warnw("检查裂变记录失败", "account_id", accountID, "error", err)
				} else if err == gorm.ErrRecordNotFound {
					// 使用事务记录裂变关系
					err = h.db.Transaction(func(tx *gorm.DB) error {
						now := time.Now()
						if err := tx.Model(&model.WhatsAppAccount{}).Where("id = ?", accountID).Updates(map[string]interface{}{
							"referred_by_account_id": referralInfo.SourceAccountID,
							"referral_registered_at": now,
						}).Error; err != nil {
							return fmt.Errorf("更新账号推荐信息失败: %w", err)
						}

						// 创建裂变记录
						registration := model.ReferralRegistration{
							ReferralCode:      referralInfo.ReferralCode,
							SourceAccountID:   referralInfo.SourceAccountID,
							NewAccountID:      accountID,
							SourceAgentID:     referralInfo.SourceAgentID,
							PromotionDomainID: referralInfo.PromotionDomainID,
							RegisteredAt:      now,
						}
						if err := tx.Create(&registration).Error; err != nil {
							return fmt.Errorf("创建裂变记录失败: %w", err)
						}

						return nil
					})

					if err != nil {
						logger.Ctx(ctx).Errorw("记录裂变关系失败", "account_id", accountID, "error", err)
					} else {
						logger.Ctx(ctx).Infow("裂变关系记录成功", "account_id", accountID, "referral_code", referralInfo.ReferralCode, "source_account_id", referralInfo.SourceAccountID)
						// 成功记录后删除 Redis 中的临时数据
						if err := h.referralSessionService.DeleteReferralSession(ctx, payload.SessionID); err != nil {
							logger.Ctx(ctx).Warnw("删除推荐码会话失败", "session_id", payload.SessionID, "error", err)
						}
					}
				} else {
					logger.Ctx(ctx).Infow("帳號已有裂變記錄，跳過推薦碼綁定", "account_id", accountID, "existing_referral_code", existingReferral.ReferralCode)
				}

				// 处理来源代码（source_key）自动打标签
				if referralInfo.SourceKey != "" {
					var tag model.AccountTag
					err := h.db.Where("source_key = ? AND deleted_at IS NULL", referralInfo.SourceKey).First(&tag).Error
					if err != nil && err != gorm.ErrRecordNotFound {
						logger.Ctx(ctx).Warnw("查询来源标签失败", "source_key", referralInfo.SourceKey, "error", err)
					} else if err == nil {
						// 检查是否已有该标签（防止重复）
						var existingTag model.WhatsAppAccountTag
						err := h.db.Where("account_id = ? AND tag_id = ?", accountID, tag.ID).First(&existingTag).Error
						if err != nil && err != gorm.ErrRecordNotFound {
							logger.Ctx(ctx).Warnw("检查账号标签关系失败", "account_id", accountID, "tag_id", tag.ID, "error", err)
						} else if err == gorm.ErrRecordNotFound {
							// 创建标签关系
							accountTag := model.WhatsAppAccountTag{
								AccountID: accountID,
								TagID:     tag.ID,
							}
							if err := h.db.Create(&accountTag).Error; err != nil {
								logger.Ctx(ctx).Errorw("自动分配来源标签失败", "account_id", accountID, "tag_id", tag.ID, "source_key", referralInfo.SourceKey, "error", err)
							} else {
								logger.Ctx(ctx).Infow("来源标签自动分配成功", "account_id", accountID, "tag_id", tag.ID, "tag_name", tag.Name, "source_key", referralInfo.SourceKey)
							}
						} else {
							logger.Ctx(ctx).Debugw("账号已有该标签，跳过", "account_id", accountID, "tag_id", tag.ID)
						}
					} else {
						logger.Ctx(ctx).Warnw("未找到匹配的来源标签", "source_key", referralInfo.SourceKey)
					}
				}
			} else {
				logger.Ctx(ctx).Debugw("無推薦碼會話", "account_id", accountID, "session_id", payload.SessionID)
			}
		}

		// 自動分配工作組（best-effort，失敗只 log）
		if h.workgroupAutoAssigner != nil {
			if err := h.workgroupAutoAssigner.AutoAssignAccount(accountID, channelID, sourceAgentID); err != nil {
				logger.Ctx(ctx).Warnw("自動分配工作組失敗", "account_id", accountID, "channel_id", channelID, "error", err)
			}
		} else {
			logger.Ctx(ctx).Warnw("workgroupAutoAssigner 未注入，跳過自動分配", "account_id", accountID)
		}
	} else {
		// 現有帳號重新連接
		accountID = event.AccountID
		updates := map[string]interface{}{
			"status":         "connected",
			"phone_number":   payload.PhoneNumber,
			"last_connected": time.Now(),
			"last_seen":      time.Now(),
		}
		if payload.PushName != "" {
			updates["push_name"] = payload.PushName
		}
		if payload.Platform != "" {
			updates["platform"] = payload.Platform
		}
		if payload.BusinessName != "" {
			updates["business_name"] = payload.BusinessName
		}
		if err := h.db.Model(&model.WhatsAppAccount{}).Where("id = ?", accountID).Updates(updates).Error; err != nil {
			logger.Ctx(ctx).Errorw("更新帳號重連狀態失敗", "account_id", accountID, "error", err)
			return err
		}
	}

	// 如果是新登入（原本 accountID=0），通知 Connector 更新 accountID 映射
	if event.AccountID == 0 && h.bindAccountCallback != nil {
		h.bindAccountCallback(payload.SessionID, accountID)
	}

	// 呼叫回調（使用實際的 accountID）
	if h.loginSuccessCallback != nil {
		h.loginSuccessCallback(accountID, payload.SessionID, payload.JID, payload.PhoneNumber)
	}

	return nil
}

// OnLoginFailed 處理登入失敗事件
func (h *WhatsAppEventHandler) OnLoginFailed(ctx context.Context, event *protocol.Event, payload *protocol.LoginFailedPayload) error {
	logger.Ctx(ctx).Warnw("登入失敗", "session_id", payload.SessionID, "reason", payload.Reason)

	// 呼叫回調
	if h.loginFailedCallback != nil {
		h.loginFailedCallback(event.AccountID, payload.SessionID, payload.Reason)
	}

	return nil
}

// OnLoginCancelled 處理登入已取消事件
func (h *WhatsAppEventHandler) OnLoginCancelled(ctx context.Context, event *protocol.Event, payload *protocol.LoginCancelledPayload) error {
	logger.Ctx(ctx).Infow("登入已取消", "session_id", payload.SessionID)
	return nil
}

// OnProfileUpdated 處理帳號資料已更新事件
func (h *WhatsAppEventHandler) OnProfileUpdated(ctx context.Context, event *protocol.Event, payload *protocol.ProfileUpdatedPayload) error {
	logger.Ctx(ctx).Infow("帳號資料已更新", "push_name", payload.PushName, "has_avatar", payload.Avatar != "")

	// 更新帳號資料
	updates := make(map[string]interface{})

	if payload.Avatar != "" {
		updates["avatar"] = payload.Avatar
	}
	if payload.AvatarID != "" {
		updates["avatar_id"] = payload.AvatarID
	}
	if payload.PushName != "" {
		updates["push_name"] = payload.PushName
	}
	if payload.FullName != "" {
		updates["full_name"] = payload.FullName
	}
	if payload.FirstName != "" {
		updates["first_name"] = payload.FirstName
	}
	if payload.KeepChatsArchived != nil {
		updates["keep_chats_archived"] = *payload.KeepChatsArchived
	}

	if len(updates) > 0 {
		if err := h.db.Model(&model.WhatsAppAccount{}).Where("id = ?", event.AccountID).Updates(updates).Error; err != nil {
			logger.Ctx(ctx).Errorw("更新帳號資料失敗", "error", err)
			return err
		}
		// logger.Infof("帳號資料更新成功: account=%d, fields=%v", event.AccountID, updates)
	}

	return nil
}

// OnGroupsSync 處理群組同步事件
func (h *WhatsAppEventHandler) OnGroupsSync(ctx context.Context, event *protocol.Event, payload *protocol.GroupsSyncPayload) error {
	logger.Ctx(ctx).Infow("收到群組同步", "count", len(payload.Groups))

	for _, group := range payload.Groups {
		if h.jidMappingService != nil {
			if _, err := h.jidMappingService.GetOrCreateChat(h.db, event.AccountID, group.JID, group.Name, true); err != nil {
				logger.Ctx(ctx).Warnw("建立群組聊天記錄失敗", "jid", group.JID, "error", err)
			}
		} else {
			result := h.db.Model(&model.WhatsAppChat{}).
				Where("account_id = ? AND jid = ?", event.AccountID, group.JID).
				Updates(map[string]interface{}{
					"name":     group.Name,
					"is_group": true,
				})
			if result.RowsAffected == 0 {
				chat := model.WhatsAppChat{
					AccountID: event.AccountID,
					JID:       group.JID,
					Name:      group.Name,
					IsGroup:   true,
				}
				if err := h.db.Create(&chat).Error; err != nil {
					logger.Ctx(ctx).Warnw("建立群組聊天記錄失敗", "jid", group.JID, "error", err)
				}
			}
		}
	}

	logger.Ctx(ctx).Infow("群組同步完成", "count", len(payload.Groups))
	return nil
}

// OnMessageRevoked 處理訊息被撤回事件
func (h *WhatsAppEventHandler) OnMessageRevoked(ctx context.Context, event *protocol.Event, payload *protocol.MessageRevokedPayload) error {
	logger.Ctx(ctx).Debugw("收到訊息撤回事件", "message_id", payload.MessageID, "is_from_me", payload.IsFromMe)

	// 更新訊息為已撤回狀態
	now := time.Now()
	result := h.db.Model(&model.WhatsAppMessage{}).
		Where("account_id = ? AND message_id = ?", event.AccountID, payload.MessageID).
		Updates(map[string]interface{}{
			"is_revoked": true,
			"revoked_at": now,
		})

	if result.Error != nil {
		logger.Ctx(ctx).Errorw("更新撤回訊息失敗", "message_id", payload.MessageID, "error", result.Error)
		return result.Error
	}

	if result.RowsAffected == 0 {
		logger.Ctx(ctx).Debugw("撤回訊息未找到", "message_id", payload.MessageID)
	} else {
		logger.Ctx(ctx).Debugw("訊息已標記為撤回", "message_id", payload.MessageID)

		// 廣播撤回事件
		if h.eventBroadcast != nil {
			h.eventBroadcast(event.AccountID, "message_revoked", map[string]interface{}{
				"message_id": payload.MessageID,
				"chat_jid":   payload.ChatJID,
				"is_from_me": payload.IsFromMe,
				"revoked_at": now.Format(time.RFC3339),
			})
		}
	}

	return nil
}

// OnMessageEdited 處理訊息被編輯事件
func (h *WhatsAppEventHandler) OnMessageEdited(ctx context.Context, event *protocol.Event, payload *protocol.MessageEditedPayload) error {
	logger.Ctx(ctx).Debugw("收到訊息編輯事件", "message_id", payload.MessageID, "is_from_me", payload.IsFromMe)

	// 更新訊息內容
	now := time.Now()
	result := h.db.Model(&model.WhatsAppMessage{}).
		Where("account_id = ? AND message_id = ?", event.AccountID, payload.MessageID).
		Updates(map[string]interface{}{
			"content":   payload.NewContent,
			"is_edited": true,
			"edited_at": now,
		})

	if result.Error != nil {
		logger.Ctx(ctx).Errorw("更新編輯訊息失敗", "message_id", payload.MessageID, "error", result.Error)
		return result.Error
	}

	if result.RowsAffected == 0 {
		logger.Ctx(ctx).Debugw("編輯訊息未找到", "message_id", payload.MessageID)
	} else {
		logger.Ctx(ctx).Debugw("訊息已更新為編輯後內容", "message_id", payload.MessageID)

		// 廣播編輯事件
		if h.eventBroadcast != nil {
			h.eventBroadcast(event.AccountID, "message_edited", map[string]interface{}{
				"message_id":  payload.MessageID,
				"chat_jid":    payload.ChatJID,
				"new_content": payload.NewContent,
				"is_from_me":  payload.IsFromMe,
				"edited_at":   now.Format(time.RFC3339),
			})
		}
	}

	return nil
}

// OnMessageDeletedForMe 處理訊息被刪除（僅自己）事件
func (h *WhatsAppEventHandler) OnMessageDeletedForMe(ctx context.Context, event *protocol.Event, payload *protocol.MessageDeletedForMePayload) error {
	logger.Ctx(ctx).Debugw("收到 DeleteForMe 事件", "message_id", payload.MessageID, "is_from_me", payload.IsFromMe)

	now := time.Now()
	result := h.db.Model(&model.WhatsAppMessage{}).
		Where("account_id = ? AND message_id = ?", event.AccountID, payload.MessageID).
		Updates(map[string]interface{}{
			"deleted_at": now,
			"deleted_by": "phone",
		})

	if result.Error != nil {
		logger.Ctx(ctx).Errorw("更新 DeleteForMe 訊息失敗", "message_id", payload.MessageID, "error", result.Error)
		return result.Error
	}

	if result.RowsAffected == 0 {
		logger.Ctx(ctx).Debugw("DeleteForMe 訊息未找到", "message_id", payload.MessageID)
	} else {
		logger.Ctx(ctx).Debugw("訊息已標記為刪除", "message_id", payload.MessageID)

		if h.eventBroadcast != nil {
			h.eventBroadcast(event.AccountID, "message_deleted_for_me", map[string]interface{}{
				"message_id": payload.MessageID,
				"chat_jid":   payload.ChatJID,
				"is_from_me": payload.IsFromMe,
				"deleted_at": now.Format(time.RFC3339),
			})
		}
	}

	return nil
}

// OnChatArchiveChanged 處理聊天歸檔狀態變更事件（來自其他裝置或 WhatsApp 自動取消歸檔）
func (h *WhatsAppEventHandler) OnChatArchiveChanged(ctx context.Context, event *protocol.Event, payload *protocol.ChatArchiveChangedPayload) error {
	// 嘗試用 JID 找到對應的 chat（含 LID 映射）
	var chat model.WhatsAppChat
	jidsToTry := []string{payload.ChatJID}
	if h.jidMappingService != nil {
		jidsToTry = append(jidsToTry, h.jidMappingService.GetAlternativeJIDs(event.AccountID, payload.ChatJID)...)
	}
	var found bool
	for _, jid := range jidsToTry {
		if err := h.db.Where("account_id = ? AND jid = ?", event.AccountID, jid).First(&chat).Error; err == nil {
			found = true
			break
		}
	}
	if !found {
		logger.Ctx(ctx).Debugw("聊天歸檔狀態變更但找不到對應 chat", "chat_jid", payload.ChatJID)
		return nil
	}

	if chat.Archived == payload.Archived {
		return nil
	}

	updates := map[string]interface{}{"archived": payload.Archived}
	if payload.Archived {
		ts := time.Unix(payload.Timestamp, 0)
		updates["archived_at"] = &ts
	} else {
		updates["archived_at"] = nil
	}

	if err := h.db.Model(&chat).Updates(updates).Error; err != nil {
		logger.Ctx(ctx).Errorw("更新聊天歸檔狀態失敗", "chat_id", chat.ID, "error", err)
		return err
	}

	logger.Ctx(ctx).Infow("聊天歸檔狀態已同步", "chat_jid", payload.ChatJID, "archived", payload.Archived)

	if h.eventBroadcast != nil {
		h.eventBroadcast(event.AccountID, "chat_archive_changed", map[string]interface{}{
			"chat_id":  chat.ID,
			"chat_jid": payload.ChatJID,
			"archived": payload.Archived,
		})
	}

	return nil
}

// OnChatArchiveBatch 批次處理重連時的歸檔狀態同步
func (h *WhatsAppEventHandler) OnChatArchiveBatch(ctx context.Context, event *protocol.Event, payload *protocol.ChatArchiveBatchPayload) error {
	if len(payload.Items) == 0 {
		return nil
	}

	logger.Ctx(ctx).Infow("收到歸檔狀態批次同步", "count", len(payload.Items))

	// 1. 收集所有 JID
	allJIDs := make([]string, 0, len(payload.Items))
	for _, item := range payload.Items {
		allJIDs = append(allJIDs, item.ChatJID)
	}

	// 2. 批量獲取 LID 映射
	jidMapping := h.batchGetAlternativeJIDs(allJIDs)

	// 3. 構建所有可能的 JID 列表
	allPossibleJIDs := make([]string, 0, len(allJIDs)*2)
	for _, jid := range allJIDs {
		allPossibleJIDs = append(allPossibleJIDs, jid)
		if alt, ok := jidMapping[jid]; ok && alt != jid {
			allPossibleJIDs = append(allPossibleJIDs, alt)
		}
	}

	// 4. 批量查詢現有 chat
	var existingChats []model.WhatsAppChat
	if err := h.db.Where("account_id = ? AND jid IN ?", event.AccountID, allPossibleJIDs).
		Select("id, jid, archived").Find(&existingChats).Error; err != nil {
		logger.Ctx(ctx).Errorw("批量查詢 chat 失敗", "error", err)
		return err
	}

	chatByJID := make(map[string]*model.WhatsAppChat, len(existingChats))
	for i := range existingChats {
		chatByJID[existingChats[i].JID] = &existingChats[i]
	}

	// 5. 收集需要更新的 chat（只更新狀態有變化的）
	now := time.Now()
	var toArchive, toUnarchive []uint
	for _, item := range payload.Items {
		targetJID := item.ChatJID
		chat := chatByJID[targetJID]
		if chat == nil {
			if alt, ok := jidMapping[item.ChatJID]; ok {
				chat = chatByJID[alt]
			}
		}
		if chat == nil || chat.Archived == item.Archived {
			continue
		}
		if item.Archived {
			toArchive = append(toArchive, chat.ID)
		} else {
			toUnarchive = append(toUnarchive, chat.ID)
		}
	}

	// 6. 批量更新
	if len(toArchive) > 0 {
		if err := h.db.Model(&model.WhatsAppChat{}).Where("id IN ?", toArchive).
			Updates(map[string]interface{}{"archived": true, "archived_at": &now}).Error; err != nil {
			logger.Ctx(ctx).Errorw("批量歸檔更新失敗", "error", err)
			return err
		}
	}
	if len(toUnarchive) > 0 {
		if err := h.db.Model(&model.WhatsAppChat{}).Where("id IN ?", toUnarchive).
			Updates(map[string]interface{}{"archived": false, "archived_at": nil}).Error; err != nil {
			logger.Ctx(ctx).Errorw("批量取消歸檔更新失敗", "error", err)
			return err
		}
	}

	logger.Ctx(ctx).Infow("歸檔狀態批次同步完成",
		"archived", len(toArchive), "unarchived", len(toUnarchive), "total", len(payload.Items))

	return nil
}

// OnChatsUpdated 處理 Chat 列表更新事件（同步名稱和頭像）
// 使用批量 UPDATE 優化，將 N 次 UPDATE 合併為 1 次
func (h *WhatsAppEventHandler) OnChatsUpdated(ctx context.Context, event *protocol.Event, payload *protocol.ChatsUpdatedPayload) error {
	if len(payload.Chats) == 0 {
		return nil
	}

	logger.Ctx(ctx).Infow("收到 Chat 更新", "count", len(payload.Chats))

	// 1. 收集所有 JID
	allJIDs := make([]string, 0, len(payload.Chats))
	for _, chat := range payload.Chats {
		allJIDs = append(allJIDs, chat.JID)
	}

	// 2. 批量獲取 LID 映射（單次查詢）
	jidMapping := h.batchGetAlternativeJIDs(allJIDs)

	// 3. 構建所有可能的 JID 列表（原始 + 替代）
	allPossibleJIDs := make([]string, 0, len(allJIDs)*2)
	for _, jid := range allJIDs {
		allPossibleJIDs = append(allPossibleJIDs, jid)
		if alt, ok := jidMapping[jid]; ok && alt != jid {
			allPossibleJIDs = append(allPossibleJIDs, alt)
		}
	}

	// 4. 批量查詢現有 chat（單次查詢）
	var existingChats []model.WhatsAppChat
	if err := h.db.Where("account_id = ? AND jid IN ?", event.AccountID, allPossibleJIDs).
		Find(&existingChats).Error; err != nil {
		logger.Ctx(ctx).Errorw("批量查詢 chat 失敗", "error", err)
		return err
	}

	existingByJID := make(map[string]bool)
	for _, c := range existingChats {
		existingByJID[c.JID] = true
	}

	// 5. 準備批量更新資料
	var updates []chatUpdateInfo

	for _, chat := range payload.Chats {
		if chat.Name == "" && chat.Avatar == "" {
			continue
		}

		// 找到對應的現有 chat JID
		targetJID := chat.JID
		if !existingByJID[chat.JID] {
			if alt, ok := jidMapping[chat.JID]; ok && existingByJID[alt] {
				targetJID = alt
			} else {
				// 不存在的 chat，跳過
				continue
			}
		}

		updates = append(updates, chatUpdateInfo{
			JID:    targetJID,
			Name:   chat.Name,
			Avatar: chat.Avatar,
		})
	}

	if len(updates) == 0 {
		logger.Ctx(ctx).Infow("Chat 更新完成，沒有需要更新的 chat")
		return nil
	}

	// 6. 執行批量 UPDATE（使用 PostgreSQL 的 UPDATE FROM VALUES 語法）
	// 將 N 次 UPDATE 合併為 1 次 SQL
	updatedCount, err := h.batchUpdateChats(event.AccountID, updates)
	if err != nil {
		logger.Ctx(ctx).Errorw("批量更新 chat 失敗", "error", err)
		return err
	}

	logger.Ctx(ctx).Infow("Chat 更新完成", "updated_count", updatedCount)
	return nil
}

// chatUpdateInfo 用於批量更新 chat 的資料結構
type chatUpdateInfo struct {
	JID    string
	Name   string
	Avatar string
}

// batchUpdateChats 使用原生 SQL 批量更新 chat
// 使用 PostgreSQL 的 UPDATE FROM VALUES 語法，將 N 次 UPDATE 合併為 1 次
func (h *WhatsAppEventHandler) batchUpdateChats(accountID uint, updates []chatUpdateInfo) (int64, error) {
	if len(updates) == 0 {
		return 0, nil
	}

	// 構建 VALUES 子句和參數
	// UPDATE whatsapp_chats AS c
	// SET name = COALESCE(NULLIF(v.name, ''), c.name),
	//     avatar = COALESCE(NULLIF(v.avatar, ''), c.avatar)
	// FROM (VALUES ($1, $2, $3), ($4, $5, $6), ...) AS v(jid, name, avatar)
	// WHERE c.account_id = $N AND c.jid = v.jid

	var valuesClauses []string
	var args []interface{}
	paramIdx := 1

	for _, u := range updates {
		valuesClauses = append(valuesClauses, fmt.Sprintf("($%d, $%d, $%d)", paramIdx, paramIdx+1, paramIdx+2))
		name := u.Name
		if len(name) > 255 {
			name = name[:255]
		}
		args = append(args, u.JID, name, u.Avatar)
		paramIdx += 3
	}

	// 加入 account_id 參數
	args = append(args, accountID)

	sql := fmt.Sprintf(`
		UPDATE whatsapp_chats AS c
		SET
			name = COALESCE(NULLIF(v.name, ''), c.name),
			avatar = COALESCE(NULLIF(v.avatar, ''), c.avatar),
			updated_at = NOW()
		FROM (VALUES %s) AS v(jid, name, avatar)
		WHERE c.account_id = $%d AND c.jid = v.jid
	`, strings.Join(valuesClauses, ", "), paramIdx)

	result := h.db.Exec(sql, args...)
	if result.Error != nil {
		return 0, result.Error
	}

	return result.RowsAffected, nil
}

// whatsmeowLIDMap 對應 whatsmeow 的 lid_map 表（用於批量查詢）
type whatsmeowLIDMap struct {
	LID string `gorm:"column:lid;primaryKey"`
	PN  string `gorm:"column:pn"`
}

func (whatsmeowLIDMap) TableName() string {
	return "whatsmeow_lid_map"
}

// batchGetAlternativeJIDs 批量獲取 LID ↔ PhoneJID 映射
func (h *WhatsAppEventHandler) batchGetAlternativeJIDs(jids []string) map[string]string {
	result := make(map[string]string)

	// 分離 LID 和 PhoneJID
	var lids, pns []string
	lidToJID := make(map[string]string)
	pnToJID := make(map[string]string)

	for _, jid := range jids {
		if strings.HasSuffix(jid, "@lid") {
			lid := strings.TrimSuffix(jid, "@lid")
			lids = append(lids, lid)
			lidToJID[lid] = jid
		} else if strings.HasSuffix(jid, "@s.whatsapp.net") {
			pn := strings.TrimSuffix(jid, "@s.whatsapp.net")
			pns = append(pns, pn)
			pnToJID[pn] = jid
		}
	}

	// 批量查詢 LID → PN
	if len(lids) > 0 {
		var mappings []whatsmeowLIDMap
		if err := h.db.Where("lid IN ?", lids).Find(&mappings).Error; err == nil {
			for _, m := range mappings {
				if m.PN != "" {
					origJID := lidToJID[m.LID]
					result[origJID] = m.PN + "@s.whatsapp.net"
				}
			}
		}
	}

	// 批量查詢 PN → LID
	if len(pns) > 0 {
		var mappings []whatsmeowLIDMap
		if err := h.db.Where("pn IN ?", pns).Find(&mappings).Error; err == nil {
			for _, m := range mappings {
				if m.LID != "" {
					origJID := pnToJID[m.PN]
					result[origJID] = m.LID + "@lid"
				}
			}
		}
	}

	return result
}

// getOrCreateChat 取得或建立聊天
func (h *WhatsAppEventHandler) getOrCreateChat(accountID uint, jid string, name string, isGroup bool) (*model.WhatsAppChat, error) {
	var chat model.WhatsAppChat

	err := h.db.Where("account_id = ? AND jid = ?", accountID, jid).First(&chat).Error
	if err == nil {
		return &chat, nil
	}

	if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	// 建立新聊天
	chat = model.WhatsAppChat{
		AccountID: accountID,
		JID:       jid,
		Name:      name,
		IsGroup:   isGroup,
		LastTime:  time.Now(),
	}

	if err := h.db.Create(&chat).Error; err != nil {
		return nil, err
	}

	return &chat, nil
}

// getOrCreateChatWithMapping 取得或建立聊天（考慮 LID ↔ PhoneJID 映射）
func (h *WhatsAppEventHandler) getOrCreateChatWithMapping(accountID uint, jid string, name string, isGroup bool) (*model.WhatsAppChat, error) {
	if h.jidMappingService != nil {
		return h.jidMappingService.GetOrCreateChat(h.db, accountID, jid, name, isGroup)
	}
	return h.getOrCreateChat(accountID, jid, name, isGroup)
}

// ownerOnlineThreshold 判斷主人是否在線的閾值
const ownerOnlineThreshold = 5 * time.Minute

// updateOwnerActivity 更新帳號主人的最後活躍時間（60 秒 debounce）
func (h *WhatsAppEventHandler) updateOwnerActivity(accountID uint) {
	h.ownerActivityMu.Lock()
	last := h.ownerActivityLastUpdate[accountID]
	now := time.Now()
	if now.Sub(last) < 60*time.Second {
		h.ownerActivityMu.Unlock()
		return
	}
	h.ownerActivityLastUpdate[accountID] = now
	h.ownerActivityMu.Unlock()

	h.db.Model(&model.WhatsAppAccount{}).
		Where("id = ?", accountID).
		Updates(map[string]interface{}{
			"owner_last_active": now,
			"is_online":         true,
		})

	if h.eventBroadcast != nil {
		h.eventBroadcast(accountID, "owner_activity", map[string]interface{}{
			"owner_last_active": now.Format(time.RFC3339),
			"is_online":         true,
		})
	}
}

// StartOwnerOnlineExpiry 啟動定時清理過期在線狀態的 goroutine
func (h *WhatsAppEventHandler) StartOwnerOnlineExpiry(ctx context.Context) {
	// 啟動時先跑一次，清理上次停機前的殘留
	h.expireStaleOnlineAccounts()

	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.expireStaleOnlineAccounts()
			}
		}
	}()
}

// expireStaleOnlineAccounts 將超過閾值未活躍的帳號標記為離線
func (h *WhatsAppEventHandler) expireStaleOnlineAccounts() {
	cutoff := time.Now().Add(-ownerOnlineThreshold)
	h.db.Model(&model.WhatsAppAccount{}).
		Where("is_online = ? AND (owner_last_active IS NULL OR owner_last_active < ?)", true, cutoff).
		Update("is_online", false)
}

// pushNameEntry push_name 快取條目
type pushNameEntry struct {
	name      string
	fetchedAt time.Time
}

const pushNameCacheTTL = 5 * time.Minute

// getAccountPushName 取得帳號 push_name（帶快取）
func (h *WhatsAppEventHandler) getAccountPushName(accountID uint) string {
	if v, ok := h.pushNameCache.Load(accountID); ok {
		entry := v.(pushNameEntry)
		if time.Since(entry.fetchedAt) < pushNameCacheTTL {
			return entry.name
		}
	}
	var account model.WhatsAppAccount
	name := ""
	if err := h.db.Select("push_name").Where("id = ?", accountID).First(&account).Error; err == nil {
		name = account.PushName
	}
	h.pushNameCache.Store(accountID, pushNameEntry{name: name, fetchedAt: time.Now()})
	return name
}

// OnMediaDownloaded 處理媒體下載完成事件（更新歷史訊息的 media_url）
// 因為 MediaDownloaded 和 MessageReceived 走不同 stream，可能先於訊息記錄建立，
// 所以 RowsAffected==0 時 retry 等待訊息寫入。
func (h *WhatsAppEventHandler) OnMediaDownloaded(ctx context.Context, event *protocol.Event, payload *protocol.MediaDownloadedPayload) error {
	const maxRetries = 3
	const retryInterval = 2 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		result := h.db.WithContext(ctx).
			Model(&model.WhatsAppMessage{}).
			Where("message_id = ? AND account_id = ? AND (media_url IS NULL OR media_url = '')", payload.MessageID, event.AccountID).
			Update("media_url", payload.MediaURL)

		if result.Error != nil {
			logger.Ctx(ctx).Warnw("更新媒體 URL 失敗", "message_id", payload.MessageID, "error", result.Error)
			return result.Error
		}

		if result.RowsAffected > 0 {
			logger.Ctx(ctx).Debugw("媒體 URL 已更新", "message_id", payload.MessageID, "media_url", payload.MediaURL)
			return nil
		}

		// 訊息記錄可能還沒建立，等待後重試
		if attempt < maxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
			}
		}
	}

	logger.Ctx(ctx).Warnw("媒體 URL 更新失敗：訊息記錄不存在", "message_id", payload.MessageID, "account_id", event.AccountID)
	return nil
}

// truncateString 截斷字串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
