package whatsapp

import (
	"encoding/json"
	"time"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"gorm.io/gorm"
)

// SyncChatHistory 同步指定聊天的历史消息 - 修复后的版本
func (s *whatsappService) SyncChatHistory(accountID uint, chatJID string, count int) error {
	// 直接使用fetchChatMessages方法 (该方法在 chat.go 中定义)
	return s.fetchChatMessages(accountID, chatJID, count)
}

// handleHistorySync 处理历史消息同步事件
func (s *whatsappService) handleHistorySync(accountID uint, evt *events.HistorySync) {
	log := logger.WithAccount(accountID)
	log.Infow("收到歷史訊息同步事件",
		"sync_type", evt.Data.SyncType,
		"conversation_count", len(evt.Data.Conversations))

	totalMessages := 0
	for _, conv := range evt.Data.Conversations {
		totalMessages += len(conv.GetMessages())
	}
	log.Infow("歷史同步事件訊息統計", "total_messages", totalMessages)

	if totalMessages == 0 {
		log.Warnw("歷史同步事件不包含任何訊息")
		return
	}

	savedCount := 0
	skippedCount := 0

	// 处理每个对话的历史消息
	for _, conversation := range evt.Data.Conversations {
		chatJID := conversation.GetID()
		msgCount := len(conversation.GetMessages())
		log.Infow("處理聊天的歷史訊息", "chat_jid", chatJID, "msg_count", msgCount)

		if msgCount == 0 {
			continue
		}

		// 解析聊天JID
		jid, err := types.ParseJID(chatJID)
		if err != nil {
			log.Errorw("解析聊天 JID 失敗", "chat_jid", chatJID, "error", err)
			continue
		}

		// 获取或创建聊天记录（使用統一函式去重）
		var chat model.WhatsAppChat
		isGroup := IsGroupJID(chatJID)

		if s.jidMappingService != nil {
			chatPtr, chatErr := s.jidMappingService.GetOrCreateChat(s.db.GetDB(), accountID, chatJID, chatJID, isGroup)
			if chatErr != nil {
				log.Errorw("取得或建立聊天記錄失敗", "chat_jid", chatJID, "error", chatErr)
				continue
			}
			chat = *chatPtr
		} else {
			result := s.db.GetDB().Where("account_id = ? AND jid = ?", accountID, chatJID).First(&chat)
			if result.Error == gorm.ErrRecordNotFound {
				chat = model.WhatsAppChat{
					AccountID: accountID,
					JID:       chatJID,
					Name:      chatJID,
				}
				if err := s.db.GetDB().Create(&chat).Error; err != nil {
					log.Errorw("建立聊天記錄失敗", "error", err)
					continue
				}
			} else if result.Error != nil {
				log.Errorw("查詢聊天記錄失敗", "chat_jid", chatJID, "error", result.Error)
				continue
			}
		}

		// 确保 chat.ID 有效
		if chat.ID == 0 {
			log.Errorw("聊天記錄 ID 無效，跳過處理", "chat_jid", chatJID)
			continue
		}

		// 获取 client
		s.mu.RLock()
		client, exists := s.clients[accountID]
		s.mu.RUnlock()

		if !exists {
			log.Errorw("客戶端不存在，跳過處理聊天", "chat_jid", chatJID)
			continue
		}

		// 处理历史消息（同步处理以确保消息被保存）
		for _, historyMsg := range conversation.GetMessages() {
			// 使用ParseWebMessage解析历史消息
			msgEvt, err := client.ParseWebMessage(jid, historyMsg.GetMessage())
			if err != nil {
				log.Errorw("解析歷史訊息失敗", "error", err)
				continue
			}

			// 提取並儲存 LID ↔ PhoneJID 映射
			if s.jidMappingService != nil && !msgEvt.Info.SenderAlt.IsEmpty() {
				s.jidMappingService.SaveMapping(
					accountID,
					msgEvt.Info.Sender.String(),
					msgEvt.Info.SenderAlt.String(),
				)
			}

			// 检查消息是否已存在
			var existingMsg model.WhatsAppMessage
			result := s.db.GetDB().Where("account_id = ? AND message_id = ?", accountID, msgEvt.Info.ID).First(&existingMsg)
			if result.Error == nil {
				// 消息已存在，跳过
				skippedCount++
				continue
			}

			// 同步保存历史消息（不使用 goroutine，确保消息被保存）
			s.saveHistoryMessage(accountID, chat.ID, msgEvt)
			savedCount++
		}
	}

	log.Infow("歷史訊息同步處理完成",
		"saved", savedCount, "skipped", skippedCount)
}

// saveHistoryMessage 保存历史消息
func (s *whatsappService) saveHistoryMessage(accountID, chatID uint, msgEvt *events.Message) {
	log := logger.WithAccount(accountID)

	// 解析消息内容
	parsed := s.parseMessageContent(msgEvt, accountID)
	if parsed.Skip {
		return
	}

	content := parsed.Content
	msgType := parsed.Type
	metadata := parsed.Metadata
	var mediaURL string

	// 同步下载媒体（历史消息同步处理）
	if parsed.NeedsMedia {
		s.mu.RLock()
		client := s.clients[accountID]
		s.mu.RUnlock()

		if client != nil {
			if url, err := s.downloadMediaMessage(client, msgEvt); err == nil {
				mediaURL = url
			} else {
				log.Errorw("下載歷史媒體失敗", "type", msgType, "error", err)
			}
		}
	}

	// 序列化元数据
	var metadataBytes []byte
	if metadata != nil && len(metadata) > 0 {
		var err error
		metadataBytes, err = json.Marshal(metadata)
		if err != nil {
			log.Errorw("序列化歷史訊息元數據失敗", "error", err)
			metadataBytes = nil
		}
	}

	// 保存消息记录
	message := &model.WhatsAppMessage{
		AccountID:       accountID,
		ChatID:          chatID,
		MessageID:       msgEvt.Info.ID,
		FromJID:         msgEvt.Info.MessageSource.Sender.String(),
		ToJID:           msgEvt.Info.MessageSource.Chat.String(),
		Content:         content,
		Type:            msgType,
		MediaURL:        mediaURL,
		MessageMetadata: metadataBytes,
		Timestamp:       msgEvt.Info.Timestamp,
		IsFromMe:        msgEvt.Info.MessageSource.IsFromMe,
		IsRead:          msgEvt.Info.MessageSource.IsFromMe, // 历史消息默认已读
	}

	if err := s.db.GetDB().Create(message).Error; err != nil {
		log.Errorw("保存歷史訊息失敗", "error", err)
		return
	}

	log.Debugw("保存歷史訊息成功", "message_id", msgEvt.Info.ID, "content", content)

	// 更新用户消息计数


	// 更新聊天记录的 last_message 和 last_time
	// 只有当这条消息的时间戳比当前 last_time 更新时才更新
	var chat model.WhatsAppChat
	if err := s.db.GetDB().First(&chat, chatID).Error; err == nil {
		// 如果聊天记录的 last_time 为空或者这条消息更新，则更新聊天记录
		if chat.LastTime.IsZero() || msgEvt.Info.Timestamp.After(chat.LastTime) {
			updates := map[string]interface{}{
				"last_message": content,
				"last_time":    msgEvt.Info.Timestamp,
			}
			s.db.GetDB().Model(&chat).Updates(updates)
			log.Debugw("更新聊天記錄", "chat_id", chatID, "last_message", content)
		}
	}
}

// saveHistoryMessageFromWebMessage 从 WebMessageInfo 保存历史消息
func (s *whatsappService) saveHistoryMessageFromWebMessage(accountID, chatID uint, webMsg *waWeb.WebMessageInfo) {
	if webMsg == nil || webMsg.Message == nil {
		return
	}

	log := logger.WithAccount(accountID)

	// 检查消息是否已存在
	messageID := webMsg.GetKey().GetID()
	if messageID == "" {
		return
	}

	var existingMsg model.WhatsAppMessage
	result := s.db.GetDB().Where("account_id = ? AND message_id = ?", accountID, messageID).First(&existingMsg)
	if result.Error == nil {
		// 消息已存在，跳过
		return
	}

	// 解析消息内容
	var content string
	var msgType string

	if webMsg.Message.GetConversation() != "" {
		content = webMsg.Message.GetConversation()
		msgType = "text"
	} else if extMsg := webMsg.Message.GetExtendedTextMessage(); extMsg != nil {
		content = extMsg.GetText()
		msgType = "text"
	} else if webMsg.Message.GetImageMessage() != nil {
		content = "[图片消息]"
		msgType = "image"
	} else if webMsg.Message.GetVideoMessage() != nil {
		content = "[视频消息]"
		msgType = "video"
	} else if webMsg.Message.GetAudioMessage() != nil {
		content = "[音频消息]"
		msgType = "audio"
	} else if webMsg.Message.GetDocumentMessage() != nil {
		content = "[文档消息]"
		msgType = "document"
	} else {
		content = "[其他类型消息]"
		msgType = "other"
	}

	// 解析发送者和接收者
	fromJID := ""
	toJID := ""
	isFromMe := false

	if webMsg.Key != nil {
		if webMsg.Key.GetRemoteJID() != "" {
			toJID = webMsg.Key.GetRemoteJID()
		}
		if webMsg.Key.GetParticipant() != "" {
			fromJID = webMsg.Key.GetParticipant()
		} else if webMsg.Key.GetFromMe() {
			isFromMe = true
			fromJID = toJID // 如果是自己发的，fromJID设为toJID
		}
	}

	// 解析时间戳
	timestamp := time.Now()
	if webMsg.GetMessageTimestamp() > 0 {
		timestamp = time.Unix(int64(webMsg.GetMessageTimestamp()), 0)
	}

	// 保存消息记录
	message := &model.WhatsAppMessage{
		AccountID: accountID,
		ChatID:    chatID,
		MessageID: messageID,
		FromJID:   fromJID,
		ToJID:     toJID,
		Content:   content,
		Type:      msgType,
		Timestamp: timestamp,
		IsFromMe:  isFromMe,
		IsRead:    isFromMe,
	}

	if err := s.db.GetDB().Create(message).Error; err != nil {
		log.Errorw("保存歷史訊息失敗", "error", err)
	} else {
		log.Debugw("保存歷史訊息成功", "message_id", messageID, "content", content)
		// 更新用户消息计数

	}
}
