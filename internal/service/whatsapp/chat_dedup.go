package whatsapp

import (
	"fmt"

	"gorm.io/gorm"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
)

// ResolvePhoneJID 從 whatsmeow_lid_map 解析 phone_jid
// 如果 jid 是 @s.whatsapp.net → 直接回傳
// 如果 jid 是 @lid → 查 mapping → phone@s.whatsapp.net
// 群組或無法解析 → 回傳 ""
func (s *jidMappingService) ResolvePhoneJID(jid string) string {
	if IsGroupJID(jid) {
		return ""
	}

	if IsPhoneJID(jid) {
		return jid
	}

	if IsLID(jid) {
		lid := ExtractJIDUser(jid)
		var mapping WhatsmeowLIDMap
		if err := s.db.Where("lid = ?", lid).First(&mapping).Error; err == nil && mapping.PN != "" {
			return mapping.PN + "@s.whatsapp.net"
		}
	}

	return ""
}

// GetOrCreateChat 統一的 chat 建立/查找函式
// 確保同一聯絡人（不同 JID 格式）只有一筆 chat 記錄
func (s *jidMappingService) GetOrCreateChat(db *gorm.DB, accountID uint, jid, name string, isGroup bool) (*model.WhatsAppChat, error) {
	// 群組：直接 findOrCreate by (account_id, jid)
	if isGroup || IsGroupJID(jid) {
		return s.getOrCreateGroupChat(db, accountID, jid, name)
	}

	// 個人聊天：透過 phone_jid 去重
	phoneJID := s.ResolvePhoneJID(jid)

	// phone_jid 非空時，以 phone_jid 為 canonical key 查找
	if phoneJID != "" {
		chat, err := s.findOrCreateByPhoneJID(db, accountID, jid, phoneJID, name)
		if err != nil {
			return nil, err
		}
		if chat != nil {
			return chat, nil
		}
	}

	// phone_jid 為空（mapping 尚不存在）：fallback 到 FindExistingChat
	existingChat := s.FindExistingChat(db, accountID, jid)
	if existingChat != nil {
		return existingChat, nil
	}

	// 完全沒找到 → 建立新 chat
	canonicalJID := s.GetCanonicalJID(accountID, jid)
	chat := model.WhatsAppChat{
		AccountID: accountID,
		JID:       canonicalJID,
		PhoneJID:  phoneJID,
		Name:      name,
		IsGroup:   false,
	}

	result := db.Where("account_id = ? AND jid = ?", accountID, canonicalJID).FirstOrCreate(&chat)
	if result.Error != nil {
		// 併發 FirstOrCreate 衝突 → 再查一次
		if err := db.Where("account_id = ? AND jid = ?", accountID, canonicalJID).First(&chat).Error; err == nil {
			return &chat, nil
		}
		return nil, fmt.Errorf("建立 chat 失敗: %w", result.Error)
	}

	// 如果是已存在的記錄但 phone_jid 為空，嘗試回填
	if phoneJID != "" && chat.PhoneJID == "" {
		db.Model(&chat).Update("phone_jid", phoneJID)
		chat.PhoneJID = phoneJID
	}

	return &chat, nil
}

// findOrCreateByPhoneJID 以 phone_jid 為 key 查找或建立 chat
func (s *jidMappingService) findOrCreateByPhoneJID(db *gorm.DB, accountID uint, jid, phoneJID, name string) (*model.WhatsAppChat, error) {
	var chat model.WhatsAppChat

	// 先用 phone_jid 查找
	err := db.Where("account_id = ? AND phone_jid = ? AND phone_jid != ''", accountID, phoneJID).First(&chat).Error
	if err == nil {
		// 找到了：如果新的 jid 是 LID 格式，更新 jid 欄位
		if IsLID(jid) && !IsLID(chat.JID) {
			db.Model(&chat).Update("jid", jid)
			chat.JID = jid
		}
		// 更新名稱（如果有更好的名稱）
		if name != "" && (chat.Name == "" || chat.Name == chat.JID || chat.Name == chat.PhoneJID) {
			db.Model(&chat).Update("name", name)
			chat.Name = name
		}
		return &chat, nil
	}

	if err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("查詢 chat by phone_jid 失敗: %w", err)
	}

	// phone_jid 沒找到，用 ON CONFLICT 插入
	canonicalJID := s.GetCanonicalJID(accountID, jid)
	if name == "" {
		name = canonicalJID
	}

	chat = model.WhatsAppChat{
		AccountID: accountID,
		JID:       canonicalJID,
		PhoneJID:  phoneJID,
		Name:      name,
		IsGroup:   false,
	}

	// ON CONFLICT 處理兩種衝突：
	// 1. (account_id, jid) — 無條件唯一索引，併發插入相同 jid 時觸發
	// 2. (account_id, phone_jid) — 部分唯一索引，同一聯絡人不同 jid 格式時觸發
	// PostgreSQL 不支援多個 ON CONFLICT，所以用 (account_id, jid) 作為主要衝突處理，
	// phone_jid 衝突則透過 error fallback 再查詢處理。
	err = db.Exec(`
		INSERT INTO whatsapp_chats (account_id, jid, phone_jid, name, is_group, created_at, updated_at)
		VALUES (?, ?, ?, ?, false, NOW(), NOW())
		ON CONFLICT (account_id, jid) DO UPDATE SET
			phone_jid = COALESCE(NULLIF(whatsapp_chats.phone_jid, ''), EXCLUDED.phone_jid),
			name = COALESCE(NULLIF(EXCLUDED.name, ''), whatsapp_chats.name),
			updated_at = NOW()
	`, accountID, canonicalJID, phoneJID, name).Error

	if err != nil {
		// phone_jid 唯一索引衝突 → 已有同 phone_jid 的 chat，直接查詢返回
		var existing model.WhatsAppChat
		if dbErr := db.Where("account_id = ? AND phone_jid = ?", accountID, phoneJID).First(&existing).Error; dbErr == nil {
			// 如果新 jid 是 LID，更新舊記錄的 jid
			if IsLID(canonicalJID) && !IsLID(existing.JID) {
				db.Model(&existing).Update("jid", canonicalJID)
				existing.JID = canonicalJID
			}
			return &existing, nil
		}
		return nil, fmt.Errorf("upsert chat 失敗: %w", err)
	}

	// 重新查詢取得完整記錄（含 DB 生成的 id）
	if err := db.Where("account_id = ? AND jid = ?", accountID, canonicalJID).First(&chat).Error; err != nil {
		return nil, fmt.Errorf("upsert 後查詢 chat 失敗: %w", err)
	}

	return &chat, nil
}

// getOrCreateGroupChat 群組 chat 的 findOrCreate
func (s *jidMappingService) getOrCreateGroupChat(db *gorm.DB, accountID uint, jid, name string) (*model.WhatsAppChat, error) {
	var chat model.WhatsAppChat
	result := db.Where("account_id = ? AND jid = ?", accountID, jid).First(&chat)

	if result.Error == nil {
		// 更新群組名稱
		if name != "" && chat.Name != name {
			db.Model(&chat).Update("name", name)
			chat.Name = name
		}
		return &chat, nil
	}

	if result.Error != gorm.ErrRecordNotFound {
		return nil, result.Error
	}

	chat = model.WhatsAppChat{
		AccountID: accountID,
		JID:       jid,
		Name:      name,
		IsGroup:   true,
	}

	if err := db.Create(&chat).Error; err != nil {
		// 併發建立：再查一次
		if err2 := db.Where("account_id = ? AND jid = ?", accountID, jid).First(&chat).Error; err2 == nil {
			return &chat, nil
		}
		return nil, fmt.Errorf("建立群組 chat 失敗: %w", err)
	}

	return &chat, nil
}

// ReconcileDuplicateChats 背景修復 phone_jid 為空的 chat 並合併殘留重複
func (s *jidMappingService) ReconcileDuplicateChats(db *gorm.DB, accountID uint) int {
	log := logger.WithAccount(accountID)
	log.Infow("開始 ReconcileDuplicateChats")

	fixedCount := 0

	// 1. 查找 phone_jid 為空的非群組 chat
	var chats []model.WhatsAppChat
	if err := db.Where("account_id = ? AND (phone_jid IS NULL OR phone_jid = '') AND is_group = false", accountID).Find(&chats).Error; err != nil {
		log.Errorw("查詢待修復 chat 失敗", "error", err)
		return 0
	}

	log.Infow("找到待修復 chat", "count", len(chats))

	for _, chat := range chats {
		phoneJID := s.ResolvePhoneJID(chat.JID)
		if phoneJID == "" {
			continue
		}

		// 檢查是否已有相同 phone_jid 的 chat
		var existingChat model.WhatsAppChat
		err := db.Where("account_id = ? AND phone_jid = ? AND phone_jid != '' AND id != ?", accountID, phoneJID, chat.ID).First(&existingChat).Error

		if err == nil {
			// 已有相同 phone_jid 的 chat → 在事務中合併
			log.Infow("合併重複 chat",
				"keep_id", existingChat.ID, "remove_id", chat.ID,
				"keep_jid", existingChat.JID, "remove_jid", chat.JID)

			mergeErr := db.Transaction(func(tx *gorm.DB) error {
				// 移動 messages
				if err := tx.Model(&model.WhatsAppMessage{}).Where("chat_id = ?", chat.ID).Update("chat_id", existingChat.ID).Error; err != nil {
					return err
				}

				// 移動 tags（排除已存在的避免 unique constraint 衝突）
				tx.Exec(`
					UPDATE chat_tags SET chat_id = ?
					WHERE chat_id = ? AND NOT EXISTS (
						SELECT 1 FROM chat_tags t2
						WHERE t2.chat_id = ? AND t2.account_id = chat_tags.account_id
						  AND t2.tag = chat_tags.tag AND t2.category = chat_tags.category
					)
				`, existingChat.PhoneJID, chat.JID, existingChat.PhoneJID)

				// 刪除無法移動的重複 tags
				tx.Where("chat_id = ?", chat.JID).Delete(&model.ChatTag{})

				// 刪除舊 chat
				return tx.Delete(&chat).Error
			})

			if mergeErr != nil {
				log.Errorw("合併重複 chat 失敗", "error", mergeErr)
				continue
			}
			fixedCount++
		} else {
			// 沒有重複 → 回填 phone_jid
			db.Model(&chat).Update("phone_jid", phoneJID)
			fixedCount++
		}
	}

	log.Infow("ReconcileDuplicateChats 完成", "fixed_count", fixedCount)
	return fixedCount
}
