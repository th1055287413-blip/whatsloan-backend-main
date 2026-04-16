package whatsapp

import (
	"context"
	"fmt"
	"time"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"gorm.io/gorm"
)

// GetChats 獲取聊天列表（含 phone_jid 去重保險層）
func (s *whatsappService) GetChats(accountID uint) ([]*model.WhatsAppChat, error) {
	var chats []*model.WhatsAppChat

	// 使用 DISTINCT ON 確保同一 phone_jid 只回傳一筆
	err := s.db.GetDB().Raw(`
		SELECT DISTINCT ON (account_id, COALESCE(NULLIF(phone_jid, ''), jid)) *
		FROM whatsapp_chats
		WHERE account_id = ?
		ORDER BY account_id, COALESCE(NULLIF(phone_jid, ''), jid), last_time DESC
	`, accountID).Scan(&chats).Error

	logger.WithAccount(accountID).Infow("獲取聊天列表", "chat_count", len(chats))

	for _, chat := range chats {
		chat.Name = s.getDisplayName(chat.JID, chat.Name)
	}

	return chats, err
}

// GetContacts 獲取帳號的聯絡人列表（從 whatsmeow_contacts 表讀取）
func (s *whatsappService) GetContacts(accountID uint, page, pageSize int) ([]*model.WhatsAppContact, int64, error) {
	var account model.WhatsAppAccount
	if err := s.db.GetDB().Select("device_id").Where("id = ?", accountID).First(&account).Error; err != nil {
		return nil, 0, err
	}

	if account.DeviceID == "" {
		logger.WithAccount(accountID).Warnw("帳號尚未關聯設備，無法取得聯絡人")
		return []*model.WhatsAppContact{}, 0, nil
	}

	type wmContact struct {
		TheirJID string `gorm:"column:their_jid"`
		PushName string `gorm:"column:push_name"`
		FullName string `gorm:"column:full_name"`
	}

	// 使用 DISTINCT ON 去重：LID 和 PhoneJID 可能指向同一人
	var total int64
	countSQL := `
		SELECT COUNT(*) FROM (
			SELECT DISTINCT ON (COALESCE(m.pn || '@s.whatsapp.net', c.their_jid)) c.their_jid
			FROM whatsmeow_contacts c
			LEFT JOIN whatsmeow_lid_map m ON c.their_jid LIKE '%@lid' AND m.lid = REPLACE(c.their_jid, '@lid', '')
			WHERE c.our_jid = ?
		) sub
	`
	if err := s.db.GetDB().Raw(countSQL, account.DeviceID).Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	var wmContacts []wmContact
	offset := (page - 1) * pageSize
	querySQL := `
		SELECT DISTINCT ON (COALESCE(m.pn || '@s.whatsapp.net', c.their_jid))
			c.their_jid, c.push_name, c.full_name
		FROM whatsmeow_contacts c
		LEFT JOIN whatsmeow_lid_map m ON c.their_jid LIKE '%@lid' AND m.lid = REPLACE(c.their_jid, '@lid', '')
		WHERE c.our_jid = ?
		ORDER BY COALESCE(m.pn || '@s.whatsapp.net', c.their_jid), c.push_name ASC
		OFFSET ? LIMIT ?
	`
	if err := s.db.GetDB().Raw(querySQL, account.DeviceID, offset, pageSize).Scan(&wmContacts).Error; err != nil {
		return nil, 0, err
	}

	contacts := make([]*model.WhatsAppContact, 0, len(wmContacts))
	for _, wc := range wmContacts {
		phone := ""
		if len(wc.TheirJID) > 0 {
			if idx := len(wc.TheirJID) - len("@s.whatsapp.net"); idx > 0 && wc.TheirJID[idx:] == "@s.whatsapp.net" {
				phone = wc.TheirJID[:idx]
			}
		}

		name := wc.PushName
		if name == "" {
			name = wc.FullName
		}

		contacts = append(contacts, &model.WhatsAppContact{
			AccountID: accountID,
			JID:       wc.TheirJID,
			Phone:     phone,
			PushName:  name,
			FullName:  wc.FullName,
		})
	}

	logger.WithAccount(accountID).Infow("獲取聯絡人列表",
		"total", total, "page_count", len(contacts))
	return contacts, total, nil
}

// getDisplayName 获取更友好的显示名称
func (s *whatsappService) getDisplayName(jid, currentName string) string {
	if currentName != "" && currentName != jid {
		return currentName
	}

	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		return jid
	}

	if parsedJID.Server == "g.us" {
		return jid
	}

	phoneNumber := parsedJID.User
	if phoneNumber != "" {
		if len(phoneNumber) > 10 {
			return "+" + phoneNumber
		}
		return phoneNumber
	}

	return jid
}

// GetMessages 獲取訊息列表
func (s *whatsappService) GetMessages(chatID uint, limit, offset int) ([]*model.WhatsAppMessage, error) {
	var messages []*model.WhatsAppMessage
	err := s.db.GetDB().Where("chat_id = ?", chatID).
		Order("timestamp DESC").
		Limit(limit).
		Offset(offset).
		Find(&messages).Error
	logger.Debugw("獲取聊天訊息列表",
		"chat_id", chatID, "message_count", len(messages), "limit", limit, "offset", offset)
	return messages, err
}

// GetMessagesByJID 根據 JID 獲取訊息列表
func (s *whatsappService) GetMessagesByJID(accountID uint, jid string, limit, offset int) ([]*model.WhatsAppMessage, error) {
	log := logger.WithAccount(accountID)
	log.Debugw("開始獲取訊息", "jid", jid, "limit", limit, "offset", offset)

	// 使用 JID 映射查找 chat（LID ↔ PhoneJID 互通）
	var chat model.WhatsAppChat
	var found bool

	if s.jidMappingService != nil {
		if existing := s.jidMappingService.FindExistingChat(s.db.GetDB(), accountID, jid); existing != nil {
			chat = *existing
			found = true
		}
	}

	if !found {
		err := s.db.GetDB().Where("account_id = ? AND jid = ?", accountID, jid).First(&chat).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				log.Infow("聊天記錄不存在，嘗試建立", "jid", jid)
				if createErr := s.createChatRecord(accountID, jid); createErr != nil {
					log.Errorw("建立聊天記錄失敗", "error", createErr)
				}

				// 建立後再查（考慮映射）
				if s.jidMappingService != nil {
					if existing := s.jidMappingService.FindExistingChat(s.db.GetDB(), accountID, jid); existing != nil {
						chat = *existing
						found = true
					}
				}
				if !found {
					err = s.db.GetDB().Where("account_id = ? AND jid = ?", accountID, jid).First(&chat).Error
					if err != nil {
						log.Errorw("建立聊天記錄後仍然找不到", "error", err)
						go s.forceSyncChat(accountID, jid)
						return []*model.WhatsAppMessage{}, nil
					}
				}
			} else {
				log.Errorw("查詢聊天記錄失敗", "error", err)
				return nil, err
			}
		}
	}

	log.Debugw("找到聊天記錄", "chat_id", chat.ID, "name", chat.Name, "jid", chat.JID)

	var totalMessages int64
	s.db.GetDB().Model(&model.WhatsAppMessage{}).Where("chat_id = ?", chat.ID).Count(&totalMessages)
	log.Debugw("聊天訊息統計", "chat_id", chat.ID, "total_messages", totalMessages)

	if totalMessages == 0 {
		log.Infow("聊天資料庫中沒有訊息，嘗試獲取歷史訊息", "jid", jid)
		go s.fetchChatMessages(accountID, jid, 50)

		time.Sleep(2 * time.Second)
		s.db.GetDB().Model(&model.WhatsAppMessage{}).Where("chat_id = ?", chat.ID).Count(&totalMessages)
		log.Debugw("等待後訊息統計", "chat_id", chat.ID, "total_messages", totalMessages)
	}

	var messages []*model.WhatsAppMessage
	queryErr := s.db.GetDB().Where("chat_id = ?", chat.ID).
		Order("timestamp DESC").
		Limit(limit).
		Offset(offset).
		Find(&messages).Error

	if queryErr != nil {
		log.Errorw("獲取訊息失敗", "jid", jid, "chat_id", chat.ID, "error", queryErr)
		return nil, queryErr
	}

	log.Debugw("訊息獲取成功",
		"jid", jid, "chat_id", chat.ID, "message_count", len(messages), "limit", limit, "offset", offset)

	return messages, nil
}

// createChatRecord 建立聊天記錄
func (s *whatsappService) createChatRecord(accountID uint, jid string) error {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	chatName := jid

	if exists && client.IsConnected() && client.IsLoggedIn() {
		ctx := context.Background()
		parsedJID, err := types.ParseJID(jid)
		if err == nil {
			if contact, err := client.Store.Contacts.GetContact(ctx, parsedJID); err == nil {
				if contact.PushName != "" {
					chatName = contact.PushName
				}
			}
		}
	}

	isGroup := IsGroupJID(jid)
	if s.jidMappingService != nil {
		_, err := s.jidMappingService.GetOrCreateChat(s.db.GetDB(), accountID, jid, chatName, isGroup)
		if err != nil {
			logger.WithAccount(accountID).Errorw("建立聊天記錄失敗", "error", err)
			return err
		}
		logger.WithAccount(accountID).Infow("建立聊天記錄成功", "name", chatName, "jid", jid)
		return nil
	}

	chat := model.WhatsAppChat{
		AccountID:   accountID,
		JID:         jid,
		Name:        chatName,
		LastMessage: "",
		LastTime:    time.Now(),
		UnreadCount: 0,
	}

	if err := s.db.GetDB().Create(&chat).Error; err != nil {
		logger.WithAccount(accountID).Errorw("建立聊天記錄失敗", "error", err)
		return err
	}

	logger.WithAccount(accountID).Infow("建立聊天記錄成功", "name", chatName, "jid", jid)
	return nil
}

// forceSyncChat 強制同步指定聊天
func (s *whatsappService) forceSyncChat(accountID uint, jid string) {
	logger.WithAccount(accountID).Infow("強制同步聊天", "jid", jid)

	if err := s.SyncChatsManually(accountID); err != nil {
		logger.Errorw("同步聊天列表失敗", "error", err)
	}

	time.Sleep(3 * time.Second)

	if err := s.SyncChatHistory(accountID, jid, 50); err != nil {
		logger.Errorw("同步聊天歷史失敗", "error", err)
	}
}

// fetchChatMessages 獲取聊天訊息
func (s *whatsappService) fetchChatMessages(accountID uint, jid string, count int) error {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || !client.IsConnected() || !client.IsLoggedIn() {
		return fmt.Errorf("账号 %d 未连接", accountID)
	}

	log := logger.WithAccount(accountID)
	log.Infow("請求聊天歷史訊息", "jid", jid, "count", count)

	chatJID, err := types.ParseJID(jid)
	if err != nil {
		return fmt.Errorf("解析JID失败: %v", err)
	}

	var chat model.WhatsAppChat
	result := s.db.GetDB().Where("account_id = ? AND jid = ?", accountID, jid).First(&chat)
	if result.Error == gorm.ErrRecordNotFound {
		if err := s.createChatRecord(accountID, jid); err != nil {
			log.Errorw("建立聊天記錄失敗", "error", err)
			return err
		}
		log.Infow("為聊天建立了新記錄", "jid", jid)
		s.db.GetDB().Where("account_id = ? AND jid = ?", accountID, jid).First(&chat)
	}

	var lastKnownMsg model.WhatsAppMessage
	var lastKnownMsgInfo *types.MessageInfo

	err = s.db.GetDB().Where("account_id = ? AND chat_id = ?", accountID, chat.ID).
		Order("timestamp ASC").
		First(&lastKnownMsg).Error

	if err == nil && lastKnownMsg.MessageID != "" {
		senderJID, _ := types.ParseJID(lastKnownMsg.FromJID)
		lastKnownMsgInfo = &types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     chatJID,
				Sender:   senderJID,
				IsFromMe: lastKnownMsg.IsFromMe,
			},
			ID:        types.MessageID(lastKnownMsg.MessageID),
			Timestamp: lastKnownMsg.Timestamp,
		}
		log.Infow("使用最早訊息作為歷史同步參考點",
			"message_id", lastKnownMsg.MessageID,
			"timestamp", lastKnownMsg.Timestamp.Format("2006-01-02 15:04:05"))
	} else {
		lastKnownMsgInfo = &types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     chatJID,
				Sender:   client.Store.ID.ToNonAD(),
				IsFromMe: true,
			},
			ID:        types.MessageID(""),
			Timestamp: time.Now(),
		}
		log.Infow("聊天沒有現有訊息，使用當前時間作為參考點請求最近的歷史訊息", "jid", jid)
	}

	historyReqMsg := client.BuildHistorySyncRequest(lastKnownMsgInfo, count)
	if historyReqMsg == nil {
		return fmt.Errorf("构建历史同步请求失败")
	}

	_, err = client.SendMessage(context.Background(), chatJID, historyReqMsg, whatsmeow.SendRequestExtra{Peer: true})
	if err != nil {
		return fmt.Errorf("发送历史同步请求失败: %v", err)
	}

	log.Infow("已發送歷史訊息同步請求，等待伺服器回應", "jid", jid)
	return nil
}

