package whatsapp

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/types/events"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
)

// MessageBroadcastCallback 消息广播回调函数类型
type MessageBroadcastCallback func(accountID uint, message *model.WhatsAppMessage)

// messageBroadcastCallback 全局消息广播回调
var messageBroadcastCallback MessageBroadcastCallback

// SetMessageBroadcastCallback 设置消息广播回调
func SetMessageBroadcastCallback(callback MessageBroadcastCallback) {
	messageBroadcastCallback = callback
}

// createEventHandler 创建事件处理器
func (s *whatsappService) createEventHandler(accountID uint) func(interface{}) {
	logger.WithAccount(accountID).Debugw("建立事件處理器")
	return func(evt interface{}) {
		log := logger.WithAccount(accountID)
		switch v := evt.(type) {
		case *events.Message:
			s.handleMessage(accountID, v)
		case *events.Receipt:
			s.handleReceipt(accountID, v)
		case *events.HistorySync:
			log.Debugw("收到 HistorySync 事件")
			s.handleHistorySync(accountID, v)
		case *events.Connected:
			log.Infow("收到 Connected 事件，已連接到 WhatsApp")
			log.Debugw("Connected: 開始更新資料庫狀態")
			err := s.updateAccountStatus(accountID, map[string]interface{}{
				"status":         "connected",
				"last_connected": time.Now(),
				"last_seen":      time.Now(),
			})
			if err != nil {
				log.Errorw("更新連接狀態失敗", "error", err)
			} else {
				log.Debugw("資料庫狀態已更新為 connected")
			}
			// 重置重連計數
			s.reconnectMu.Lock()
			delete(s.reconnectAttempts, accountID)
			s.reconnectMu.Unlock()
			log.Debugw("重連計數已重置")

			// 獲取帳號基本資訊
			log.Debugw("開始異步獲取基本資訊")
			go s.fetchAccountInfo(accountID)

			// 通過 SyncController 控制頭像同步
			if s.syncController != nil && s.syncController.ShouldSyncAvatar() {
				s.mu.RLock()
				client, exists := s.clients[accountID]
				s.mu.RUnlock()
				if exists && client.IsLoggedIn() {
					log.Debugw("客戶端已就緒，檢查頭像狀態")
					var account model.WhatsAppAccount
					if err := s.db.GetDB().Select("avatar").Where("id = ?", accountID).First(&account).Error; err == nil {
						if account.Avatar == "" {
							log.Debugw("沒有頭像，開始異步獲取")
							go s.updateAccountAvatar(accountID, client)
						}
					}
				}
			} else {
				log.Debugw("頭像同步已停用（由 SyncController 控制）")
			}
		case *events.Disconnected:
			log.Warnw("收到 Disconnected 事件，已斷開連接")
			err := s.updateAccountStatus(accountID, map[string]interface{}{
				"status":   "disconnected",
				"last_seen": time.Now(),
			})
			if err != nil {
				log.Errorw("更新斷開連接狀態失敗", "error", err)
			}

			if s.syncQueue != nil {
				s.syncQueue.MarkAccountDisconnected(context.Background(), accountID)
			}

			if s.connectionManager != nil {
				log.Debugw("通知連接管理器進行重連")
				s.connectionManager.requestAccountConnection(accountID)
			}
		case *events.LoggedOut:
			log.Warnw("收到 LoggedOut 事件，已被強制登出")

			s.logDisconnectDiagnostics(accountID, "LoggedOut", v)

			var account model.WhatsAppAccount
			if err := s.db.GetDB().Select("phone_number").Where("id = ?", accountID).First(&account).Error; err == nil {
				log.Warnw("檢測到 LoggedOut 事件，更新狀態為離線", "phone", account.PhoneNumber)
			} else {
				log.Warnw("檢測到 LoggedOut 事件，但無法獲取帳號詳情", "error", err)
			}

			err := s.updateAccountStatus(accountID, map[string]interface{}{
				"status":   "logged_out",
				"last_seen": time.Now(),
			})
			if err != nil {
				log.Errorw("更新 LoggedOut 狀態失敗", "error", err)
			}

			s.mu.Lock()
			if client, exists := s.clients[accountID]; exists {
				log.Infow("清理客戶端實例 (LoggedOut)")
				client.Disconnect()
				delete(s.clients, accountID)
			}
			s.mu.Unlock()

			s.reconnectMu.Lock()
			delete(s.reconnectAttempts, accountID)
			s.reconnectMu.Unlock()

			if s.syncQueue != nil {
				s.syncQueue.MarkAccountDisconnected(context.Background(), accountID)
				cleared, err := s.syncQueue.ClearAccountTasks(context.Background(), accountID)
				if err != nil {
					log.Warnw("清理同步任務失敗", "error", err)
				} else if cleared > 0 {
					log.Infow("已清理待處理同步任務", "cleared_count", cleared)
				}
			}

		case *events.Picture:
			log.Infow("收到 Picture 事件",
				"jid", v.JID.String(), "remove", v.Remove, "picture_id", v.PictureID)
			go s.handlePictureEvent(accountID, v)

		case *events.StreamReplaced:
			log.Warnw("收到 StreamReplaced 事件，連接已被其他設備替換")

			s.logDisconnectDiagnostics(accountID, "StreamReplaced", nil)

			var account model.WhatsAppAccount
			if err := s.db.GetDB().Select("phone_number").Where("id = ?", accountID).First(&account).Error; err == nil {
				log.Warnw("StreamReplaced - 可能原因: 多實例競爭/手機端重新掃碼/其他設備登入",
					"phone", account.PhoneNumber)
			} else {
				log.Warnw("StreamReplaced 但無法獲取詳情", "error", err)
			}

			err := s.updateAccountStatus(accountID, map[string]interface{}{
				"status":   "disconnected",
				"last_seen": time.Now(),
			})
			if err != nil {
				log.Errorw("更新 StreamReplaced 狀態失敗", "error", err)
			}

			s.mu.Lock()
			if client, exists := s.clients[accountID]; exists {
				log.Infow("清理客戶端實例 (StreamReplaced)")
				client.Disconnect()
				delete(s.clients, accountID)
			}
			s.mu.Unlock()

			s.reconnectMu.Lock()
			delete(s.reconnectAttempts, accountID)
			s.reconnectMu.Unlock()

			if s.syncQueue != nil {
				s.syncQueue.MarkAccountDisconnected(context.Background(), accountID)
				cleared, err := s.syncQueue.ClearAccountTasks(context.Background(), accountID)
				if err != nil {
					log.Warnw("清理同步任務失敗", "error", err)
				} else if cleared > 0 {
					log.Infow("已清理待處理同步任務", "cleared_count", cleared)
				}
			}
		}
	}
}

// handleMessage 處理訊息事件
func (s *whatsappService) handleMessage(accountID uint, msg *events.Message) {
	// 提取並儲存 LID <-> PhoneJID 映射
	if s.jidMappingService != nil && !msg.Info.SenderAlt.IsEmpty() {
		go s.jidMappingService.SaveMapping(
			accountID,
			msg.Info.Sender.String(),
			msg.Info.SenderAlt.String(),
		)
	}

	parsed := s.parseMessageContent(msg, accountID)
	if parsed.Skip {
		return
	}

	content := parsed.Content
	msgType := parsed.Type
	metadata := parsed.Metadata
	var mediaURL string

	s.mu.RLock()
	client := s.clients[accountID]
	s.mu.RUnlock()

	if parsed.NeedsMedia && client != nil {
		go func() {
			if url, err := s.downloadMediaMessage(client, msg); err == nil {
				s.updateMessageMediaURL(accountID, msg.Info.ID, url)
			} else {
				logger.Errorw("下載媒體失敗", "type", msgType, "error", err)
			}
		}()
	}

	go s.saveMessageWithMetadata(
		accountID,
		msg.Info.ID,
		msg.Info.MessageSource.Sender.String(),
		msg.Info.MessageSource.Chat.String(),
		content,
		msgType,
		msg.Info.Timestamp,
		msg.Info.MessageSource.IsFromMe,
		"",
		metadata,
		mediaURL,
	)

	if s.interceptor != nil {
		senderName := msg.Info.PushName
		if senderName == "" {
			senderName = msg.Info.MessageSource.Sender.String()
		}

		var account model.WhatsAppAccount
		receiverName := ""
		if err := s.db.GetDB().First(&account, accountID).Error; err == nil {
			if account.PushName != "" {
				receiverName = account.PushName
			} else if account.FullName != "" {
				receiverName = account.FullName
			} else {
				receiverName = account.PhoneNumber
			}
		}

		logger.WithAccount(accountID).Debugw("開始調用敏感詞檢測",
			"is_from_me", msg.Info.MessageSource.IsFromMe, "content", content)
		go s.interceptor.CheckMessage(
			accountID,
			msg.Info.ID,
			msg.Info.MessageSource.Chat.String(),
			msg.Info.MessageSource.Sender.String(),
			senderName,
			receiverName,
			content,
			msg.Info.MessageSource.IsFromMe,
			msg.Info.Timestamp.UnixMilli(),
			msg.Info.MessageSource.Chat.String(),
			nil,
		)
	} else {
		logger.Debugw("跳過敏感詞檢測: interceptor 未初始化")
	}
}

// handleReceipt 處理訊息回執事件(送達和已讀狀態)
func (s *whatsappService) handleReceipt(accountID uint, receipt *events.Receipt) {
	log := logger.WithAccount(accountID)
	var newStatus string
	if receipt.Type == events.ReceiptTypeRead || receipt.Type == events.ReceiptTypeReadSelf {
		newStatus = "read"
		log.Debugw("收到已讀回執",
			"message_ids", receipt.MessageIDs, "sender", receipt.MessageSource.Sender)
	} else if receipt.Type == events.ReceiptTypeDelivered {
		newStatus = "delivered"
		log.Debugw("收到送達回執",
			"message_ids", receipt.MessageIDs, "sender", receipt.MessageSource.Sender)
	} else {
		return
	}

	for _, messageID := range receipt.MessageIDs {
		go s.updateMessageStatus(accountID, messageID, newStatus)
	}
}

// broadcastNewMessage 通过 WebSocket 广播新消息
func (s *whatsappService) broadcastNewMessage(accountID uint, message *model.WhatsAppMessage) {
	if messageBroadcastCallback != nil {
		messageBroadcastCallback(accountID, message)
	}
}

// logDisconnectDiagnostics 記錄斷線診斷資訊
func (s *whatsappService) logDisconnectDiagnostics(accountID uint, eventType string, evt interface{}) {
	log := logger.WithAccount(accountID)
	log.Warnw("斷線診斷報告開始", "event_type", eventType)

	// 1. 帳號基本資訊
	var account model.WhatsAppAccount
	if err := s.db.GetDB().Where("id = ?", accountID).First(&account).Error; err == nil {
		connectionDuration := "未知"
		if !account.LastConnected.IsZero() {
			connectionDuration = time.Since(account.LastConnected).String()
		}
		log.Warnw("帳號資訊",
			"phone", account.PhoneNumber, "status", account.Status,
			"connection_duration", connectionDuration)
	}

	// 2. 同步狀態
	if s.syncStatusService != nil {
		if status, err := s.syncStatusService.GetByAccountID(accountID); err == nil && status != nil {
			historyProgress := status.HistorySyncProgress
			if historyProgress == "" {
				historyProgress = "N/A"
			}
			log.Warnw("同步狀態",
				"connect", status.ConnectStatus,
				"chat", status.ChatSyncStatus,
				"history", status.HistorySyncStatus,
				"history_progress", historyProgress,
				"contact", status.ContactSyncStatus)

			if status.HistorySyncStatus == model.SyncStateRunning || status.HistorySyncStatus == model.SyncStateQueued {
				log.Warnw("歷史同步正在進行中，可能因 API 請求過多被 WhatsApp 踢出")
			}
		}
	}

	// 3. 統計
	var chatCount, messageCount int64
	s.db.GetDB().Model(&model.WhatsAppChat{}).Where("account_id = ?", accountID).Count(&chatCount)
	s.db.GetDB().Model(&model.WhatsAppMessage{}).Where("account_id = ?", accountID).Count(&messageCount)
	log.Warnw("資料統計", "chat_count", chatCount, "message_count", messageCount)

	// 4. LoggedOut 事件的額外資訊
	if loggedOut, ok := evt.(*events.LoggedOut); ok && loggedOut != nil {
		log.Warnw("LoggedOut 原因碼", "reason", loggedOut.Reason)
	}

	// 5. 推測可能原因
	if eventType == "LoggedOut" {
		log.Warnw("可能原因分析: 1)用戶移除連結設備 2)WhatsApp 檢測異常活動 3)帳號重新登入 4)官方限制或封禁")
	} else if eventType == "StreamReplaced" {
		log.Warnw("可能原因分析: 1)同一帳號多服務器實例 2)手機端重新掃碼 3)其他設備登入 WhatsApp Web")
	}

	// 6. 同步隊列資訊
	log.Warnw("同步隊列限速設定: 每 5 秒 1 個任務")
	log.Warnw("斷線診斷報告結束")
}

// handlePictureEvent 處理頭像變更事件
func (s *whatsappService) handlePictureEvent(accountID uint, evt *events.Picture) {
	jidStr := evt.JID.String()
	log := logger.WithAccount(accountID)

	if evt.Remove {
		log.Infow("頭像已被移除", "jid", jidStr)
		s.clearAvatarByJID(accountID, jidStr)
		return
	}

	if evt.PictureID == "" {
		log.Debugw("PictureID 為空，跳過", "jid", jidStr)
		return
	}

	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || client == nil || !client.IsConnected() {
		log.Warnw("客戶端不可用，跳過頭像更新")
		return
	}

	picInfo, err := client.GetProfilePictureInfo(context.Background(), evt.JID, nil)
	if err != nil {
		log.Warnw("獲取頭像失敗", "jid", jidStr, "error", err)
		return
	}

	if picInfo == nil {
		log.Debugw("頭像資訊為空", "jid", jidStr)
		return
	}

	s.updateAvatarByJID(accountID, jidStr, picInfo.URL, picInfo.ID)
	log.Infow("頭像已更新", "jid", jidStr, "picture_id", picInfo.ID)
}

// clearAvatarByJID 清除指定 JID 的頭像
func (s *whatsappService) clearAvatarByJID(accountID uint, jid string) {
	updates := map[string]interface{}{
		"avatar":    "",
		"avatar_id": "",
	}

	s.db.GetDB().Model(&model.WhatsAppChat{}).
		Where("account_id = ? AND jid = ?", accountID, jid).
		Updates(updates)

	var account model.WhatsAppAccount
	if err := s.db.GetDB().Where("id = ?", accountID).First(&account).Error; err == nil {
		myJID := account.PhoneNumber + "@s.whatsapp.net"
		if jid == myJID {
			s.db.GetDB().Model(&model.WhatsAppAccount{}).
				Where("id = ?", accountID).
				Updates(updates)
		}
	}
}

// updateAvatarByJID 更新指定 JID 的頭像
func (s *whatsappService) updateAvatarByJID(accountID uint, jid string, avatarURL string, avatarID string) {
	updates := map[string]interface{}{
		"avatar":    avatarURL,
		"avatar_id": avatarID,
	}

	s.db.GetDB().Model(&model.WhatsAppChat{}).
		Where("account_id = ? AND jid = ?", accountID, jid).
		Updates(updates)

	var account model.WhatsAppAccount
	if err := s.db.GetDB().Where("id = ?", accountID).First(&account).Error; err == nil {
		myJID := account.PhoneNumber + "@s.whatsapp.net"
		if jid == myJID {
			s.db.GetDB().Model(&model.WhatsAppAccount{}).
				Where("id = ?", accountID).
				Updates(updates)
		}
	}
}
