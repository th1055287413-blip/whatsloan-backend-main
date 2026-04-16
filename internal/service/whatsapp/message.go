package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
)

// SendMessage 发送消息
func (s *whatsappService) SendMessage(accountID uint, toJID, content string, adminID *uint) error {
	return s.SendMessageWithOriginal(accountID, toJID, content, "", adminID)
}

// SendMessageWithOriginal 发送消息（带原文）
// 注意：此方法使用舊的 whatsmeow 直連方式，adminID 參數暫不使用
// 新的發送流程應使用 Gateway
func (s *whatsappService) SendMessageWithOriginal(accountID uint, toJID, content string, originalText string, adminID *uint) error {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || !client.IsLoggedIn() {
		return fmt.Errorf("账号未连接")
	}

	jid, err := types.ParseJID(toJID)
	if err != nil {
		return fmt.Errorf("无效的JID: %v", err)
	}

	msg := &waE2E.Message{
		Conversation: &content,
	}

	resp, err := client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return fmt.Errorf("发送消息失败: %v", err)
	}

	// 保存消息到数据库，包含原文
	go s.saveMessage(accountID, resp.ID, client.Store.ID.String(), toJID, content, "text", time.Now(), true, originalText)

	// 敏感词检测 - 检测发送的消息
	if s.interceptor != nil {
		// 获取发送者账号名称
		var account model.WhatsAppAccount
		senderName := ""
		if err := s.db.GetDB().First(&account, accountID).Error; err == nil {
			if account.PushName != "" {
				senderName = account.PushName
			} else if account.FullName != "" {
				senderName = account.FullName
			} else {
				senderName = account.PhoneNumber
			}
		}

		// 获取接收者名称
		receiverName := toJID

		logger.WithAccount(accountID).Debugw("開始呼叫敏感詞檢測（發送訊息）",
			"content", content)
		// 舊流程不支援 admin tracking，傳入 nil
		go s.interceptor.CheckMessage(
			accountID,
			resp.ID,
			toJID,
			client.Store.ID.String(),
			senderName,
			receiverName,
			content,
			true,                   // isFromMe
			time.Now().UnixMilli(), // messageTimestamp
			toJID,                  // toJID
			nil,                    // sentByAdminID (舊流程不支援)
		)
	}

	return nil
}

// saveMessage 保存消息到数据库
func (s *whatsappService) saveMessage(accountID uint, messageID, fromJID, toJID, content, msgType string, timestamp time.Time, isFromMe bool, originalText string, mediaURL ...string) {
	// 调用新方法，不传 metadata
	s.saveMessageWithMetadata(accountID, messageID, fromJID, toJID, content, msgType, timestamp, isFromMe, originalText, nil, mediaURL...)
}

// saveMessageWithMetadata 保存带元数据的消息
func (s *whatsappService) saveMessageWithMetadata(accountID uint, messageID, fromJID, toJID, content, msgType string, timestamp time.Time, isFromMe bool, originalText string, metadata map[string]interface{}, mediaURL ...string) {
	// 获取或创建聊天记录
	// toJID 实际上就是 msg.Info.MessageSource.Chat.String()，它始终代表聊天对象
	// 不管是发送还是接收消息，聊天对象都是同一个
	chatJID := toJID

	log := logger.WithAccount(accountID)
	log.Debugw("保存訊息",
		"message_id", messageID, "from_jid", fromJID, "to_jid", toJID,
		"chat_jid", chatJID, "content", content, "type", msgType,
		"is_from_me", isFromMe, "timestamp", timestamp)

	var chat model.WhatsAppChat

	// 使用統一函式取得或建立聊天記錄（去重）
	if s.jidMappingService != nil {
		isGroup := IsGroupJID(chatJID)
		chatPtr, chatErr := s.jidMappingService.GetOrCreateChat(s.db.GetDB(), accountID, chatJID, chatJID, isGroup)
		if chatErr != nil {
			log.Errorw("取得或建立聊天記錄失敗", "error", chatErr)
			return
		}
		chat = *chatPtr
	} else {
		// fallback：原始邏輯
		err := s.retryDatabaseOperation(func() error {
			return s.db.GetDB().Where("account_id = ? AND jid = ?", accountID, chatJID).First(&chat).Error
		}, fmt.Sprintf("查找聊天记录 %s", chatJID))

		if err == gorm.ErrRecordNotFound {
			chat = model.WhatsAppChat{
				AccountID:   accountID,
				JID:         chatJID,
				Name:        chatJID,
				LastMessage: content,
				LastTime:    timestamp,
				UnreadCount: 0,
			}
			if !isFromMe {
				chat.UnreadCount = 1
			}

			createErr := s.retryDatabaseOperation(func() error {
				return s.db.GetDB().Create(&chat).Error
			}, fmt.Sprintf("创建聊天记录 %s", chatJID))

			if createErr != nil {
				log.Errorw("建立聊天記錄失敗", "error", createErr)
				return
			}
		} else if err != nil {
			log.Errorw("查找聊天記錄失敗", "error", err)
			return
		}
	}

	if chat.ID > 0 {
		// 更新聊天记录（使用重试机制）
		updates := map[string]interface{}{
			"last_message": content,
			"last_time":    timestamp,
		}
		if !isFromMe {
			updates["unread_count"] = gorm.Expr("unread_count + 1")
		}

		updateErr := s.retryDatabaseOperation(func() error {
			return s.db.GetDB().Model(&chat).Updates(updates).Error
		}, fmt.Sprintf("更新聊天记录 %s", chatJID))

		if updateErr != nil {
			log.Errorw("更新聊天記錄失敗", "error", updateErr)
			return
		}
	}

	// 保存消息记录
	// 根据消息方向设置发送状态
	sendStatus := "delivered" // 接收的消息默认为已送达
	if isFromMe {
		sendStatus = "sent" // 发送的消息默认为已发送
	}

	// 提取MediaURL参数(可选)
	var mediaURLValue string
	if len(mediaURL) > 0 {
		mediaURLValue = mediaURL[0]
	}

	// 序列化 metadata 为 JSON
	var metadataBytes []byte
	if metadata != nil && len(metadata) > 0 {
		var err error
		metadataBytes, err = json.Marshal(metadata)
		if err != nil {
			log.Errorw("序列化訊息元數據失敗", "error", err)
			metadataBytes = nil
		}
	}

	message := &model.WhatsAppMessage{
		AccountID:       accountID,
		ChatID:          chat.ID,
		MessageID:       messageID,
		FromJID:         fromJID,
		ToJID:           toJID,
		Content:         content,
		OriginalText:    originalText,
		Type:            msgType,
		MediaURL:        mediaURLValue,
		MessageMetadata: metadataBytes,
		Timestamp:       timestamp,
		IsFromMe:        isFromMe,
		IsRead:          isFromMe, // 自己发送的消息默认已读
		SendStatus:      sendStatus,
	}

	// 保存消息记录（使用重试机制，ON CONFLICT DO NOTHING 避免重複）
	if err := s.retryDatabaseOperation(func() error {
		return s.db.GetDB().Clauses(clause.OnConflict{DoNothing: true}).Create(message).Error
	}, fmt.Sprintf("保存消息 %s", messageID)); err != nil {
		log.Errorw("保存訊息失敗", "error", err)
		return
	}
	if message.ID == 0 {
		log.Debugw("訊息已存在，跳過", "message_id", messageID)
		return
	}

	// 通过 WebSocket 推送新消息
	go s.broadcastNewMessage(accountID, message)
}

// updateMessageStatus 更新消息状态
func (s *whatsappService) updateMessageStatus(accountID uint, messageID string, newStatus string) {
	log := logger.WithAccount(accountID)

	err := s.retryDatabaseOperation(func() error {
		return s.db.GetDB().Model(&model.WhatsAppMessage{}).
			Where("account_id = ? AND message_id = ?", accountID, messageID).
			Update("send_status", newStatus).Error
	}, fmt.Sprintf("更新消息 %s 状态为 %s", messageID, newStatus))

	if err != nil {
		log.Errorw("更新訊息狀態失敗",
			"message_id", messageID, "status", newStatus, "error", err)
	} else {
		log.Debugw("訊息狀態已更新",
			"message_id", messageID, "status", newStatus)

		// 通过 WebSocket 通知前端状态变化
		s.notifyMessageStatusChange(accountID, messageID, newStatus)
	}
}

// notifyMessageStatusChange 通知前端消息状态变化
func (s *whatsappService) notifyMessageStatusChange(accountID uint, messageID string, newStatus string) {
	log := logger.WithAccount(accountID)

	// 获取完整的消息信息
	var message model.WhatsAppMessage
	err := s.db.GetDB().Where("account_id = ? AND message_id = ?", accountID, messageID).First(&message).Error
	if err != nil {
		log.Errorw("查詢訊息失敗", "error", err)
		return
	}

	// 通过 WebSocket 推送状态更新到前端
	// 需要导入 handler 包
	// handler.WSHandler.BroadcastMessageStatus(accountID, messageID, newStatus)
	log.Debugw("訊息狀態已推送",
		"message_id", messageID, "status", newStatus)
}

// updateMessageMediaURL 更新消息媒体URL
func (s *whatsappService) updateMessageMediaURL(accountID uint, messageID string, mediaURL string) {
	if mediaURL == "" {
		return
	}

	// 更新消息的 media_url 字段
	err := s.retryDatabaseOperation(func() error {
		return s.db.GetDB().Model(&model.WhatsAppMessage{}).
			Where("message_id = ?", messageID).
			Update("media_url", mediaURL).Error
	}, fmt.Sprintf("更新消息媒体URL: %s", messageID))

	if err != nil {
		logger.WithAccount(accountID).Errorw("更新訊息媒體 URL 失敗",
			"message_id", messageID, "error", err)
	} else {
		logger.WithAccount(accountID).Debugw("成功更新訊息媒體 URL",
			"message_id", messageID, "media_url", mediaURL)
	}
}
