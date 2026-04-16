package whatsapp

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"golang.org/x/sync/semaphore"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
)

// UpdateAccountProfile 更新單個帳號的用戶資料(頭像和用戶名)
func (s *whatsappService) UpdateAccountProfile(accountID uint) error {
	log := logger.WithAccount(accountID)
	log.Infow("開始更新帳號用戶資料")

	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("账号 %d 的客户端不存在", accountID)
	}

	if !client.IsConnected() {
		return fmt.Errorf("账号 %d 未连接", accountID)
	}

	myJID := client.Store.ID
	if myJID == nil {
		return fmt.Errorf("无法获取用户 JID")
	}

	userJID := types.JID{
		User:   myJID.User,
		Server: types.DefaultUserServer,
	}

	updates := make(map[string]interface{})

	// 1. 獲取頭像資訊
	pictureInfo, err := client.GetProfilePictureInfo(context.Background(), userJID, &whatsmeow.GetProfilePictureParams{
		Preview:     false,
		ExistingID:  "",
		IsCommunity: false,
	})

	if err != nil {
		log.Warnw("獲取頭像資訊失敗", "error", err)
	} else if pictureInfo != nil {
		updates["avatar"] = pictureInfo.URL
		updates["avatar_id"] = pictureInfo.ID
		log.Infow("成功獲取頭像資訊", "avatar_id", pictureInfo.ID)
	}

	// 2. 獲取用戶名資訊
	pushName := client.Store.PushName
	if pushName != "" {
		updates["push_name"] = pushName
		log.Infow("成功獲取 PushName", "push_name", pushName)
	}

	// 3. 嘗試從聯絡人資訊獲取完整名稱
	ctx := context.Background()
	contactInfo, err := client.Store.Contacts.GetContact(ctx, *myJID)
	if err != nil {
		log.Warnw("獲取聯絡人資訊失敗", "error", err)
	} else {
		if contactInfo.FullName != "" {
			updates["full_name"] = contactInfo.FullName
		}
		if contactInfo.FirstName != "" {
			updates["first_name"] = contactInfo.FirstName
		}
		if contactInfo.FullName != "" || contactInfo.FirstName != "" {
			log.Infow("成功獲取聯絡人資訊",
				"full_name", contactInfo.FullName, "first_name", contactInfo.FirstName)
		}
	}

	// 4. 如果有更新內容，則更新資料庫
	if len(updates) > 0 {
		if err := s.db.GetDB().Model(&model.WhatsAppAccount{}).
			Where("id = ?", accountID).
			Updates(updates).Error; err != nil {
			log.Errorw("更新帳號用戶資料失敗", "error", err)
			return fmt.Errorf("更新数据库失败: %w", err)
		}

		log.Infow("成功更新帳號用戶資料", "updates", updates)
	} else {
		log.Infow("沒有可更新的用戶資料數據")
	}

	return nil
}

// UpdateMissingAccountProfiles 批量更新缺失用戶資料的帳號
func (s *whatsappService) UpdateMissingAccountProfiles() error {
	logger.Infow("開始批量更新缺失用戶資料的帳號")

	var accounts []model.WhatsAppAccount
	err := s.db.GetDB().Where(
		"status = ? AND (push_name = '' OR push_name IS NULL OR avatar = '' OR avatar IS NULL)",
		"connected",
	).Find(&accounts).Error

	if err != nil {
		logger.Errorw("查詢缺失用戶資料的帳號失敗", "error", err)
		return fmt.Errorf("查询账号失败: %w", err)
	}

	if len(accounts) == 0 {
		logger.Infow("沒有需要更新用戶資料的帳號")
		return nil
	}

	logger.Infow("找到需要更新用戶資料的帳號", "count", len(accounts))

	var connectedAccounts []model.WhatsAppAccount
	s.mu.RLock()
	for _, account := range accounts {
		if _, exists := s.clients[account.ID]; exists {
			connectedAccounts = append(connectedAccounts, account)
		} else {
			logger.WithAccount(account.ID).Debugw("跳過未連接的帳號", "phone", account.PhoneNumber)
		}
	}
	s.mu.RUnlock()

	if len(connectedAccounts) == 0 {
		logger.Infow("沒有已連接的帳號需要更新用戶資料")
		return nil
	}

	logger.Infow("開始更新已連接帳號的用戶資料",
		"connected_count", len(connectedAccounts), "total_count", len(accounts))

	semaphoreCh := make(chan struct{}, 5)
	var wg sync.WaitGroup
	var successCount, failCount int
	var mu sync.Mutex

	for _, account := range connectedAccounts {
		wg.Add(1)
		go func(acc model.WhatsAppAccount) {
			defer wg.Done()

			semaphoreCh <- struct{}{}
			defer func() { <-semaphoreCh }()

			time.Sleep(500 * time.Millisecond)

			if err := s.UpdateAccountProfile(acc.ID); err != nil {
				logger.WithAccount(acc.ID).Errorw("更新帳號用戶資料失敗",
					"phone", acc.PhoneNumber, "error", err)
				mu.Lock()
				failCount++
				mu.Unlock()
			} else {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(account)
	}

	wg.Wait()

	logger.Infow("批量更新用戶資料完成",
		"total", len(connectedAccounts), "success", successCount, "failed", failCount)

	return nil
}

// updateAccountAvatar 更新帳號頭像
func (s *whatsappService) updateAccountAvatar(accountID uint, client *whatsmeow.Client) {
	log := logger.WithAccount(accountID)

	if client.Store.ID == nil {
		log.Warnw("無法獲取頭像: Store.ID 為空")
		return
	}

	userJID := types.JID{
		User:   client.Store.ID.User,
		Server: types.DefaultUserServer,
	}

	log.Infow("正在獲取用戶頭像")
	picInfo, err := client.GetProfilePictureInfo(context.Background(), userJID, &whatsmeow.GetProfilePictureParams{
		Preview: false,
	})

	if err != nil || picInfo == nil {
		return
	}

	avatarURL := picInfo.URL
	avatarID := picInfo.ID
	log.Infow("成功獲取用戶頭像", "avatar_url", avatarURL, "avatar_id", avatarID)

	accountLock := s.accountLocks.getLock(accountID)
	accountLock.Lock()
	defer accountLock.Unlock()

	updateData := map[string]interface{}{
		"avatar":    avatarURL,
		"avatar_id": avatarID,
	}

	if err := s.db.GetDB().Model(&model.WhatsAppAccount{}).Where("id = ?", accountID).Updates(updateData).Error; err != nil {
		log.Errorw("更新頭像到資料庫失敗", "error", err)
		return
	}

	log.Infow("頭像已成功更新到資料庫")
}

// updateChatAvatar 更新單個對話的頭像
func (s *whatsappService) updateChatAvatar(accountID uint, chatID uint, chatJID string, existingAvatarID string, client *whatsmeow.Client) error {
	if client == nil || !client.IsConnected() {
		return fmt.Errorf("客户端未连接")
	}

	log := logger.WithAccount(accountID)

	jid, err := types.ParseJID(chatJID)
	if err != nil {
		log.Warnw("解析對話 JID 失敗", "jid", chatJID, "error", err)
		return err
	}

	picInfo, err := client.GetProfilePictureInfo(context.Background(), jid, &whatsmeow.GetProfilePictureParams{
		Preview:    false,
		ExistingID: existingAvatarID,
	})

	if err != nil {
		log.Debugw("對話獲取頭像失敗", "jid", chatJID, "error", err)
		return nil
	}

	if picInfo == nil {
		log.Debugw("對話頭像未變化或無頭像，跳過更新", "jid", chatJID)
		return nil
	}

	avatarURL := picInfo.URL
	avatarID := picInfo.ID
	if avatarURL == "" {
		log.Debugw("對話頭像 URL 為空", "jid", chatJID)
		return nil
	}

	log.Infow("對話頭像已變更", "jid", chatJID, "new_avatar_id", avatarID)

	err = s.db.GetDB().Model(&model.WhatsAppChat{}).
		Where("id = ?", chatID).
		Updates(map[string]interface{}{
			"avatar":    avatarURL,
			"avatar_id": avatarID,
		}).Error

	if err != nil {
		log.Errorw("更新對話頭像到資料庫失敗", "jid", chatJID, "error", err)
		return err
	}

	log.Infow("成功更新對話頭像", "jid", chatJID)
	return nil
}

// updateContactAvatar 更新單個聯絡人的頭像
func (s *whatsappService) updateContactAvatar(accountID uint, contactID uint, contactJID string, existingAvatarID string, client *whatsmeow.Client) error {
	if client == nil || !client.IsConnected() {
		return fmt.Errorf("客户端未连接")
	}

	log := logger.WithAccount(accountID)

	jid, err := types.ParseJID(contactJID)
	if err != nil {
		log.Warnw("解析聯絡人 JID 失敗", "jid", contactJID, "error", err)
		return err
	}

	picInfo, err := client.GetProfilePictureInfo(context.Background(), jid, &whatsmeow.GetProfilePictureParams{
		Preview:    false,
		ExistingID: existingAvatarID,
	})

	if err != nil {
		log.Debugw("聯絡人獲取頭像失敗", "jid", contactJID, "error", err)
		return nil
	}

	if picInfo == nil {
		log.Debugw("聯絡人頭像未變化或無頭像，跳過更新", "jid", contactJID)
		return nil
	}

	avatarURL := picInfo.URL
	avatarID := picInfo.ID
	if avatarURL == "" {
		log.Debugw("聯絡人頭像 URL 為空", "jid", contactJID)
		return nil
	}

	log.Infow("聯絡人頭像已變更", "jid", contactJID, "new_avatar_id", avatarID)

	err = s.db.GetDB().Model(&model.WhatsAppChat{}).
		Where("account_id = ? AND jid = ?", accountID, contactJID).
		Updates(map[string]interface{}{
			"avatar":    avatarURL,
			"avatar_id": avatarID,
		}).Error

	if err != nil {
		log.Errorw("更新對話頭像到資料庫失敗", "jid", contactJID, "error", err)
		return err
	}

	log.Infow("成功更新聯絡人/對話頭像", "jid", contactJID)
	return nil
}

// updateContactsAvatarsBatch 批量更新聯絡人頭像(分批處理)
func (s *whatsappService) updateContactsAvatarsBatch(accountID uint, client *whatsmeow.Client) error {
	log := logger.WithAccount(accountID)

	if client == nil || !client.IsConnected() {
		log.Warnw("客戶端未連接，跳過聯絡人頭像更新")
		return fmt.Errorf("客户端未连接")
	}

	var chats []model.WhatsAppChat
	err := s.db.GetDB().Where("account_id = ?", accountID).Find(&chats).Error
	if err != nil {
		log.Errorw("查詢對話列表失敗", "error", err)
		return err
	}

	if len(chats) == 0 {
		log.Infow("沒有對話，跳過頭像更新")
		return nil
	}

	log.Infow("開始批量更新對話頭像", "total", len(chats))

	batchSize := 10
	batchDelay := 5 * time.Second
	maxConcurrency := int64(2)

	var successCount int32 = 0
	var failCount int32 = 0

	ctx := context.Background()
	sem := semaphore.NewWeighted(maxConcurrency)

	for i := 0; i < len(chats); i += batchSize {
		end := i + batchSize
		if end > len(chats) {
			end = len(chats)
		}

		batch := chats[i:end]
		batchNum := i/batchSize + 1
		totalBatches := (len(chats) + batchSize - 1) / batchSize

		log.Infow("處理批次",
			"batch", batchNum, "total_batches", totalBatches, "batch_size", len(batch))

		var wg sync.WaitGroup

		for _, chat := range batch {
			wg.Add(1)

			if err := sem.Acquire(ctx, 1); err != nil {
				log.Errorw("獲取信號量失敗", "error", err)
				wg.Done()
				continue
			}

			go func(c model.WhatsAppChat) {
				defer wg.Done()
				defer sem.Release(1)

				err := s.updateChatAvatar(accountID, c.ID, c.JID, c.AvatarID, client)
				if err != nil {
					atomic.AddInt32(&failCount, 1)
				} else {
					atomic.AddInt32(&successCount, 1)
				}
			}(chat)
		}

		wg.Wait()

		if end < len(chats) {
			log.Debugw("批次完成，延遲後處理下一批", "delay", batchDelay)
			time.Sleep(batchDelay)
		}
	}

	log.Infow("批量更新對話頭像完成",
		"total", len(chats), "success", successCount, "failed", failCount)

	return nil
}

// UpdateAllContactsAvatars 批量更新所有帳號的聯絡人頭像
func (s *whatsappService) UpdateAllContactsAvatars() error {
	logger.Infow("開始全量更新所有帳號的聯絡人頭像")

	var accounts []model.WhatsAppAccount
	err := s.db.GetDB().Where("status = ?", "connected").Find(&accounts).Error
	if err != nil {
		logger.Errorw("查詢已連接帳號失敗", "error", err)
		return fmt.Errorf("查询账号失败: %w", err)
	}

	if len(accounts) == 0 {
		logger.Infow("沒有已連接的帳號，跳過頭像更新")
		return nil
	}

	logger.Infow("找到已連接帳號", "count", len(accounts))

	semaphoreCh := make(chan struct{}, 3)
	var wg sync.WaitGroup
	var successCount, failCount int
	var mu sync.Mutex

	for _, account := range accounts {
		wg.Add(1)
		go func(acc model.WhatsAppAccount) {
			defer wg.Done()

			semaphoreCh <- struct{}{}
			defer func() { <-semaphoreCh }()

			s.mu.RLock()
			client, exists := s.clients[acc.ID]
			s.mu.RUnlock()

			if !exists || client == nil {
				logger.WithAccount(acc.ID).Warnw("客戶端不存在，跳過")
				mu.Lock()
				failCount++
				mu.Unlock()
				return
			}

			if err := s.updateContactsAvatarsBatch(acc.ID, client); err != nil {
				logger.WithAccount(acc.ID).Errorw("批量更新帳號頭像失敗",
					"phone", acc.PhoneNumber, "error", err)
				mu.Lock()
				failCount++
				mu.Unlock()
			} else {
				mu.Lock()
				successCount++
				mu.Unlock()
			}

			time.Sleep(10 * time.Second)
		}(account)
	}

	wg.Wait()

	logger.Infow("批量更新所有帳號聯絡人頭像完成",
		"total", len(accounts), "success", successCount, "failed", failCount)

	return nil
}
