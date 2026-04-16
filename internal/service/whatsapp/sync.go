package whatsapp

import (
	"context"
	"fmt"
	"time"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// syncChatsFromWhatsApp 主動從WhatsApp同步聊天列表
func (s *whatsappService) syncChatsFromWhatsApp(accountID uint) {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || !client.IsConnected() || !client.IsLoggedIn() {
		logger.WithAccount(accountID).Warnw("帳號未連接，無法同步聊天列表")
		return
	}

	ctx := context.Background()

	// 1. 同步應用狀態 - 聊天列表 (使用重試機制)
	log := logger.WithAccount(accountID)
	log.Infow("正在同步應用狀態")
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := client.FetchAppState(ctx, "regular_high", true, false)
		if err != nil {
			if attempt < maxRetries {
				log.Warnw("同步應用狀態失敗，將重試",
					"attempt", attempt, "max_retries", maxRetries, "error", err)
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
				continue
			} else {
				log.Errorw("同步應用狀態最終失敗",
					"attempt", attempt, "max_retries", maxRetries, "error", err)
			}
		} else {
			log.Infow("應用狀態同步完成")
			break
		}
	}

	// 2. 同步聊天狀態 (使用重試機制)
	log.Infow("正在同步聊天狀態")
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := client.FetchAppState(ctx, "regular_low", true, false)
		if err != nil {
			if attempt < maxRetries {
				log.Warnw("同步聊天狀態失敗，將重試",
					"attempt", attempt, "max_retries", maxRetries, "error", err)
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
				continue
			} else {
				log.Errorw("同步聊天狀態最終失敗",
					"attempt", attempt, "max_retries", maxRetries, "error", err)
			}
		} else {
			log.Infow("聊天狀態同步完成")
			break
		}
	}

	// 3. 同步關鍵狀態 (使用重試機制)
	log.Infow("正在同步關鍵狀態")
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := client.FetchAppState(ctx, "critical_block", true, false)
		if err != nil {
			if attempt < maxRetries {
				log.Warnw("同步關鍵狀態失敗，將重試",
					"attempt", attempt, "max_retries", maxRetries, "error", err)
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
				continue
			} else {
				log.Errorw("同步關鍵狀態最終失敗",
					"attempt", attempt, "max_retries", maxRetries, "error", err)
			}
		} else {
			log.Infow("關鍵狀態同步完成")
			break
		}
	}

	// 4. 從本地存儲獲取聊天資訊並保存到資料庫（同步執行以確保計數正確）
	s.saveChatsFromStore(accountID)

	// 5. 背景修復 phone_jid 為空的 chat 並合併殘留重複
	if s.jidMappingService != nil {
		fixed := s.jidMappingService.ReconcileDuplicateChats(s.db.GetDB(), accountID)
		if fixed > 0 {
			log.Infow("背景修復重複 chat 完成", "fixed_count", fixed)
		}
	}

	log.Infow("聊天列表同步任務完成")
}

// saveChatsFromStore 从whatsmeow存储中获取聊天信息并保存到数据库
func (s *whatsappService) saveChatsFromStore(accountID uint) {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists {
		return
	}

	log := logger.WithAccount(accountID)
	ctx := context.Background()

	// 獲取聯絡人列表
	contacts, err := client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		log.Errorw("獲取聯絡人失敗", "error", err)
		return
	}

	log.Infow("開始處理聯絡人聊天", "contact_count", len(contacts))

	// 為每個聯絡人建立或更新聊天記錄（使用統一函式去重）
	for jid, contact := range contacts {
		chatName := contact.PushName
		if chatName == "" {
			chatName = jid.User
		}

		if s.jidMappingService != nil {
			if _, err := s.jidMappingService.GetOrCreateChat(s.db.GetDB(), accountID, jid.String(), chatName, false); err != nil {
				log.Errorw("建立聊天記錄失敗", "jid", jid.String(), "error", err)
			}
		} else {
			var existingChat model.WhatsAppChat
			result := s.db.GetDB().Where("account_id = ? AND jid = ?", accountID, jid.String()).First(&existingChat)
			if result.Error == gorm.ErrRecordNotFound {
				chat := model.WhatsAppChat{
					AccountID: accountID,
					JID:       jid.String(),
					Name:      chatName,
				}
				if err := s.db.GetDB().Create(&chat).Error; err != nil {
					log.Errorw("建立聊天記錄失敗", "jid", jid.String(), "error", err)
				}
			}
		}
	}

	// 獲取群組資訊
	groups, err := client.GetJoinedGroups(context.Background())
	if err != nil {
		log.Errorw("獲取群組失敗", "error", err)
	} else {
		log.Infow("開始處理群組聊天", "group_count", len(groups))

		for _, group := range groups {
			if s.jidMappingService != nil {
				if _, err := s.jidMappingService.GetOrCreateChat(s.db.GetDB(), accountID, group.JID.String(), group.Name, true); err != nil {
					log.Errorw("建立群組聊天記錄失敗", "jid", group.JID.String(), "error", err)
				}
			} else {
				var existingChat model.WhatsAppChat
				result := s.db.GetDB().Where("account_id = ? AND jid = ?", accountID, group.JID.String()).First(&existingChat)
				if result.Error == gorm.ErrRecordNotFound {
					chat := model.WhatsAppChat{
						AccountID: accountID,
						JID:       group.JID.String(),
						Name:      group.Name,
						IsGroup:   true,
					}
					if err := s.db.GetDB().Create(&chat).Error; err != nil {
						log.Errorw("建立群組聊天記錄失敗", "jid", group.JID.String(), "error", err)
					}
				} else if result.Error == nil && group.Name != "" && existingChat.Name != group.Name {
					s.db.GetDB().Model(&existingChat).Update("name", group.Name)
				}
			}
		}
	}

	log.Infow("聊天記錄保存完成")

	// 聊天記錄保存完成後，異步觸發頭像更新 (已暫停 - 避免觸發 WhatsApp 速率限制)
	// go func(accID uint) {
	// 	time.Sleep(2 * time.Second)
	// 	...
	// }(accountID)
	log.Infow("頭像自動同步已暫停")
}

// SyncChatsManually 手動同步聊天列表（API接口）
func (s *whatsappService) SyncChatsManually(accountID uint) error {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || !client.IsConnected() || !client.IsLoggedIn() {
		return fmt.Errorf("账号 %d 未连接", accountID)
	}

	logger.WithAccount(accountID).Infow("手動觸發聊天列表同步")
	go func() {
		s.syncChatsFromWhatsApp(accountID)
		if s.connectionManager != nil {
			s.connectionManager.updateLastSyncTime(accountID)
		}
	}()
	return nil
}

// updateContactNames 更新聯絡人名稱
// 支持 LID <-> PhoneJID 映射，正確處理 @lid 格式的 chat
func (s *whatsappService) updateContactNames(accountID uint) {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	log := logger.WithAccount(accountID)

	if !exists || !client.IsConnected() || !client.IsLoggedIn() {
		log.Warnw("帳號未連接，跳過聯絡人名稱更新")
		return
	}

	log.Infow("開始更新聯絡人名稱（支持 LID 映射）")

	ctx := context.Background()

	contacts, err := client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		log.Errorw("獲取聯絡人失敗", "error", err)
		return
	}

	contactMap := make(map[string]string)
	for jid, contact := range contacts {
		if contact.PushName != "" {
			phone := ExtractJIDUser(jid.String())
			contactMap[phone] = contact.PushName
		}
	}

	var chats []model.WhatsAppChat
	if err := s.db.GetDB().Where("account_id = ? AND (name = '' OR name IS NULL)", accountID).Find(&chats).Error; err != nil {
		log.Errorw("獲取聊天列表失敗", "error", err)
		return
	}

	updateCount := 0
	for _, chat := range chats {
		var newName string

		if IsGroupJID(chat.JID) {
			continue
		}

		chatUser := ExtractJIDUser(chat.JID)
		if name, ok := contactMap[chatUser]; ok {
			newName = name
		} else if IsLID(chat.JID) {
			if s.jidMappingService != nil {
				phoneJID := s.jidMappingService.GetPhoneJID(accountID, chat.JID)
				if phoneJID != chat.JID {
					phone := ExtractJIDUser(phoneJID)
					if name, ok := contactMap[phone]; ok {
						newName = name
					}
				}
			}
		}

		if newName != "" && newName != chat.Name {
			oldName := chat.Name
			if err := s.db.GetDB().Model(&chat).Update("name", newName).Error; err != nil {
				log.Errorw("更新聊天名稱失敗", "jid", chat.JID, "error", err)
			} else {
				log.Infow("聯絡人名稱已更新",
					"old_name", oldName, "new_name", newName, "jid", chat.JID)
				updateCount++
			}
		}
	}

	log.Infow("聯絡人名稱更新完成", "update_count", updateCount)

	s.updateGroupNames(accountID)
}

// updateGroupNames 更新群組名稱
func (s *whatsappService) updateGroupNames(accountID uint) {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || !client.IsConnected() || !client.IsLoggedIn() {
		return
	}

	log := logger.WithAccount(accountID)

	var chats []model.WhatsAppChat
	if err := s.db.GetDB().Where("account_id = ? AND jid LIKE '%@g.us' AND (name = '' OR name IS NULL)", accountID).Find(&chats).Error; err != nil {
		log.Errorw("獲取群組列表失敗", "error", err)
		return
	}

	if len(chats) == 0 {
		return
	}

	log.Infow("開始更新群組名稱", "group_count", len(chats))

	groups, err := client.GetJoinedGroups(context.Background())
	if err != nil {
		log.Errorw("獲取群組資訊失敗", "error", err)
		return
	}

	groupMap := make(map[string]string)
	for _, group := range groups {
		if group.Name != "" {
			groupMap[group.JID.String()] = group.Name
		}
	}

	updateCount := 0
	for _, chat := range chats {
		if name, ok := groupMap[chat.JID]; ok && name != "" {
			if err := s.db.GetDB().Model(&chat).Update("name", name).Error; err != nil {
				log.Errorw("更新群組名稱失敗", "jid", chat.JID, "error", err)
			} else {
				log.Infow("群組名稱已更新", "name", name, "jid", chat.JID)
				updateCount++
			}
		}
	}

	log.Infow("群組名稱更新完成", "update_count", updateCount)
}

// UpdateContactNamesManually 手動更新聯絡人名稱（API接口）
func (s *whatsappService) UpdateContactNamesManually(accountID uint) error {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || !client.IsConnected() || !client.IsLoggedIn() {
		return fmt.Errorf("账号 %d 未连接", accountID)
	}

	logger.WithAccount(accountID).Infow("手動觸發聯絡人名稱更新")
	go s.updateContactNames(accountID)
	return nil
}

// syncAllAccountData 為已關聯帳號同步所有資料
func (s *whatsappService) syncAllAccountData(accountID uint) {
	log := logger.WithAccount(accountID)
	log.Infow("開始為已關聯帳號同步所有資料")

	var account model.WhatsAppAccount
	if err := s.db.GetDB().Select("status").Where("id = ?", accountID).First(&account).Error; err != nil {
		log.Errorw("獲取帳號狀態失敗", "error", err)
		return
	}
	if account.Status == "logged_out" {
		log.Infow("帳號狀態為 logged_out，跳過資料同步")
		return
	}

	time.Sleep(10 * time.Second)

	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || !client.IsConnected() || !client.IsLoggedIn() {
		log.Errorw("帳號未準備就緒，無法同步資料")
		time.AfterFunc(5*time.Minute, func() {
			log.Infow("重試同步資料")
			s.syncAllAccountData(accountID)
		})
		return
	}

	log.Infow("開始同步聊天列表")
	s.syncChatsFromWhatsApp(accountID)

	time.Sleep(15 * time.Second)

	chats, err := s.GetChats(accountID)
	if err != nil {
		log.Errorw("獲取聊天列表失敗", "error", err)
		return
	}

	log.Infow("找到聊天，開始同步所有訊息歷史", "chat_count", len(chats))

	for i, chat := range chats {
		log.Infow("正在同步聊天訊息歷史",
			"chat_name", chat.Name, "progress", fmt.Sprintf("%d/%d", i+1, len(chats)))

		var messageCount int64
		s.db.GetDB().Model(&model.WhatsAppMessage{}).Where("account_id = ? AND chat_id = ?", accountID, chat.ID).Count(&messageCount)

		syncCount := 100
		if messageCount == 0 {
			syncCount = 300
			log.Infow("聊天沒有歷史訊息，將同步更多", "chat_name", chat.Name, "sync_count", syncCount)
		} else {
			log.Infow("聊天已有訊息，將同步新訊息",
				"chat_name", chat.Name, "existing_count", messageCount, "sync_count", syncCount)
		}

		if err := s.SyncChatHistory(accountID, chat.JID, syncCount); err != nil {
			log.Warnw("同步聊天歷史訊息失敗", "chat_name", chat.Name, "error", err)
		} else {
			log.Infow("聊天訊息同步請求已發送", "chat_name", chat.Name)
		}

		time.Sleep(3 * time.Second)
	}

	log.Infow("開始更新聯絡人名稱")
	s.updateContactNames(accountID)

	if s.connectionManager != nil {
		s.connectionManager.updateLastSyncTime(accountID)
	}

	log.Infow("完整資料同步完成")
}

// syncNewAccountData 新帳號資料同步
// 使用 SyncController 統一控制，避免多個入口導致重複同步
func (s *whatsappService) syncNewAccountData(accountID uint) {
	log := logger.WithAccount(accountID)
	log.Infow("開始為新關聯帳號排隊同步任務")

	s.syncStatusService.MarkRunning(accountID, model.SyncStepConnect)

	var clientReady bool
	for i := 0; i < 10; i++ {
		s.mu.RLock()
		client, exists := s.clients[accountID]
		s.mu.RUnlock()

		if exists && client.IsConnected() && client.IsLoggedIn() {
			clientReady = true
			break
		}

		log.Debugw("等待客戶端就緒", "attempt", i+1, "max_attempts", 10)
		time.Sleep(1 * time.Second)
	}

	if !clientReady {
		log.Warnw("等待 10 秒後仍未準備就緒，跳過資料同步")
		s.syncStatusService.MarkFailed(accountID, model.SyncStepConnect, "客戶端未準備就緒")
		return
	}

	s.syncStatusService.MarkCompleted(accountID, model.SyncStepConnect, nil)
	log.Infow("連接步驟已標記為完成")

	now := time.Now()
	if err := s.db.GetDB().Model(&model.WhatsAppAccount{}).Where("id = ?", accountID).Update("last_sync_at", now).Error; err != nil {
		log.Warnw("更新同步時間失敗", "error", err)
	}

	if s.syncController != nil {
		s.syncController.RequestSyncForNewAccount(s.syncQueueCtx, accountID)
	} else {
		log.Warnw("SyncController 未初始化，跳過同步")
	}
}

// SyncAllData 手動觸發完整資料同步
func (s *whatsappService) SyncAllData(accountID uint) error {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || !client.IsConnected() || !client.IsLoggedIn() {
		return fmt.Errorf("账号 %d 未连接", accountID)
	}

	logger.WithAccount(accountID).Infow("手動觸發完整資料同步")
	go s.syncAllAccountData(accountID)
	return nil
}
