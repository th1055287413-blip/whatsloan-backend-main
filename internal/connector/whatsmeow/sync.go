package whatsmeow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"whatsapp_golang/internal/protocol"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// HandleSyncChats handles the sync chats command
func (m *Manager) HandleSyncChats(ctx context.Context, cmd *protocol.Command) error {
	client, err := m.getConnectedClient(ctx, cmd.AccountID)
	if err != nil {
		return err
	}

	m.log.Debugw("同步聊天列表", "account_id", cmd.AccountID)

	// Prepare ChatsUpdated payload
	var chats []protocol.ChatInfo

	// 1. Get contacts list with names
	contacts, err := client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		m.log.Warnw("取得聯絡人列表失敗", "account_id", cmd.AccountID, "error", err)
	} else {
		for jid, contact := range contacts {
			// Prefer PushName, then FullName
			name := contact.PushName
			if name == "" {
				name = contact.FullName
			}
			if name == "" {
				name = contact.FirstName
			}
			if name != "" {
				chats = append(chats, protocol.ChatInfo{
					JID:     jid.String(),
					Name:    name,
					IsGroup: false,
				})
			}
		}
		m.log.Debugw("取得聯絡人", "account_id", cmd.AccountID, "count", len(contacts))
	}

	// 2. Get groups list
	groups, err := client.GetJoinedGroups(ctx)
	if err != nil {
		m.log.Warnw("取得群組列表失敗", "account_id", cmd.AccountID, "error", err)
	} else if len(groups) > 0 {
		for _, g := range groups {
			chats = append(chats, protocol.ChatInfo{
				JID:     g.JID.String(),
				Name:    g.Name,
				IsGroup: true,
			})
		}
	}

	// Publish 用獨立 context：WhatsApp API 可能已耗盡大部分 timeout，
	// 確保取回的資料一定能送出
	pubCtx, pubCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer pubCancel()

	// Publish groups sync event (keep old event for compatibility)
	if len(groups) > 0 {
		groupInfos := make([]protocol.GroupInfo, 0, len(groups))
		for _, g := range groups {
			groupInfos = append(groupInfos, protocol.GroupInfo{
				JID:  g.JID.String(),
				Name: g.Name,
			})
		}
		if err := m.publisher.PublishGroupsSync(pubCtx, cmd.AccountID, &protocol.GroupsSyncPayload{
			Groups: groupInfos,
		}); err != nil {
			m.log.Warnw("發送 GroupsSync 事件失敗", "account_id", cmd.AccountID, "error", err)
		} else {
			m.log.Debugw("已發送群組同步事件", "account_id", cmd.AccountID, "count", len(groupInfos))
		}
	}

	// 3. Publish ChatsUpdated event (includes all contacts and groups names)
	if len(chats) > 0 {
		if err := m.publisher.PublishChatsUpdated(pubCtx, cmd.AccountID, &protocol.ChatsUpdatedPayload{
			Chats: chats,
		}); err != nil {
			m.log.Warnw("發送 ChatsUpdated 事件失敗", "account_id", cmd.AccountID, "error", err)
		} else {
			m.log.Debugw("已發送 ChatsUpdated 事件", "account_id", cmd.AccountID, "chat_count", len(chats))
		}
	}

	// 4. Flush buffered archive states from full sync
	m.archiveBufMu.Lock()
	archiveItems := m.archiveBuf[cmd.AccountID]
	delete(m.archiveBuf, cmd.AccountID)
	m.archiveBufMu.Unlock()

	if len(archiveItems) > 0 {
		if err := m.publisher.PublishChatArchiveBatch(pubCtx, cmd.AccountID, &protocol.ChatArchiveBatchPayload{
			Items: archiveItems,
		}); err != nil {
			m.log.Warnw("發送 ChatArchiveBatch 事件失敗", "account_id", cmd.AccountID, "error", err)
		} else {
			m.log.Infow("已發送歸檔狀態批次同步", "account_id", cmd.AccountID, "count", len(archiveItems))
		}
	}

	// Publish sync complete event
	if err := m.publisher.PublishSyncComplete(pubCtx, cmd.AccountID, &protocol.SyncCompletePayload{
		SyncType: "chats",
		Count:    len(chats),
	}); err != nil {
		m.log.Warnw("發送 SyncComplete 事件失敗", "account_id", cmd.AccountID, "error", err)
	}

	// Async trigger avatar sync (delay 30 seconds to avoid conflict with other syncs)
	go m.syncRecentChatsAvatars(cmd.AccountID, 20)

	return nil
}

// syncRecentChatsAvatars syncs avatars for recently active chats
// Delayed execution to avoid conflict with App State sync, only syncs avatars for recent N active chats
func (m *Manager) syncRecentChatsAvatars(accountID uint, limit int) {
	// Check if sync task is already running (prevent duplicates)
	m.avatarSyncMu.Lock()
	if m.avatarSyncRunning[accountID] {
		m.avatarSyncMu.Unlock()
		m.log.Debugw("頭像同步跳過，已有同步任務在執行", "account_id", accountID)
		return
	}
	m.avatarSyncRunning[accountID] = true
	m.avatarSyncMu.Unlock()

	// Ensure cleanup on exit
	defer func() {
		m.avatarSyncMu.Lock()
		delete(m.avatarSyncRunning, accountID)
		m.avatarSyncMu.Unlock()
	}()

	// Delay 30 seconds, wait for App State sync to complete
	m.log.Debugw("頭像同步已排程", "account_id", accountID, "limit", limit)
	time.Sleep(30 * time.Second)

	// Acquire global semaphore (limit concurrent accounts)
	m.log.Debugw("頭像同步等待信號量", "account_id", accountID)
	m.avatarSyncSem <- struct{}{}
	defer func() { <-m.avatarSyncSem }()
	m.log.Debugw("頭像同步獲得信號量", "account_id", accountID)

	// Check if account is still connected
	m.mu.RLock()
	client, exists := m.clients[accountID]
	m.mu.RUnlock()

	if !exists {
		m.log.Debugw("頭像同步取消，帳號不存在", "account_id", accountID)
		return
	}
	if !client.IsConnected() {
		m.log.Debugw("頭像同步取消，帳號未連線", "account_id", accountID)
		return
	}

	// Get recently active chats missing avatars from DB
	type chatRecord struct {
		ID       uint   `gorm:"column:id"`
		JID      string `gorm:"column:jid"`
		AvatarID string `gorm:"column:avatar_id"`
	}
	var chats []chatRecord

	err := m.db.Table("whatsapp_chats").
		Select("id, jid, avatar_id").
		Where("account_id = ? AND (avatar = '' OR avatar IS NULL)", accountID).
		Order("last_time DESC").
		Limit(limit).
		Scan(&chats).Error

	if err != nil {
		m.log.Debugw("頭像同步失敗，查詢聊天列表錯誤", "account_id", accountID, "error", err)
		return
	}

	if len(chats) == 0 {
		m.log.Debugw("頭像同步完成，無缺頭像的聊天", "account_id", accountID)
		return
	}

	m.log.Debugw("開始頭像同步", "account_id", accountID, "chat_count", len(chats))

	ctx := context.Background()
	var updatedChats []protocol.ChatInfo
	successCount := 0
	failCount := 0
	skippedCount := 0

	for i, chat := range chats {
		// Delay between requests (no delay for first request)
		if i > 0 {
			time.Sleep(500 * time.Millisecond)
		}

		// Extra delay every 10 requests (batch delay)
		if i > 0 && i%10 == 0 {
			m.log.Debugw("頭像同步批次延遲", "account_id", accountID, "processed", i, "total", len(chats))
			time.Sleep(5 * time.Second)
		}

		// Check connection status
		if !client.IsConnected() {
			m.log.Warnw("頭像同步中斷，帳號已斷線", "account_id", accountID)
			break
		}

		// Parse JID
		jid, err := types.ParseJID(chat.JID)
		if err != nil {
			m.log.Debugw("頭像同步跳過，無效 JID", "account_id", accountID, "jid", chat.JID)
			failCount++
			continue
		}

		m.log.Debugw("頭像同步正在獲取", "account_id", accountID, "progress", fmt.Sprintf("%d/%d", i+1, len(chats)), "chat_jid", chat.JID)

		// Get avatar (use ExistingID for differential check)
		picInfo, err := client.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{
			Preview:    false,
			ExistingID: chat.AvatarID,
		})

		if err != nil {
			if strings.Contains(err.Error(), "does not have a profile picture") {
				skippedCount++
			} else {
				m.log.Debugw("頭像同步獲取失敗", "account_id", accountID, "progress", fmt.Sprintf("%d/%d", i+1, len(chats)), "chat_jid", chat.JID, "error", err)
				failCount++
			}
			continue
		}

		if picInfo == nil {
			// Avatar unchanged (ExistingID matches) or user has no avatar
			m.log.Debugw("頭像同步無頭像或未變化", "account_id", accountID, "progress", fmt.Sprintf("%d/%d", i+1, len(chats)), "chat_jid", chat.JID)
			skippedCount++
			continue
		}

		if picInfo.URL == "" {
			m.log.Debugw("頭像同步頭像 URL 為空", "account_id", accountID, "progress", fmt.Sprintf("%d/%d", i+1, len(chats)), "chat_jid", chat.JID)
			skippedCount++
			continue
		}

		// Update DB
		err = m.db.Table("whatsapp_chats").
			Where("id = ?", chat.ID).
			Updates(map[string]interface{}{
				"avatar":    picInfo.URL,
				"avatar_id": picInfo.ID,
			}).Error

		if err != nil {
			m.log.Errorw("頭像同步更新 DB 失敗", "account_id", accountID, "progress", fmt.Sprintf("%d/%d", i+1, len(chats)), "chat_jid", chat.JID, "error", err)
			failCount++
			continue
		}

		m.log.Debugw("頭像同步已更新", "account_id", accountID, "progress", fmt.Sprintf("%d/%d", i+1, len(chats)), "chat_jid", chat.JID, "avatar_id", picInfo.ID)
		successCount++

		// Collect updated chat info for frontend notification
		updatedChats = append(updatedChats, protocol.ChatInfo{
			JID:    chat.JID,
			Avatar: picInfo.URL,
		})
	}

	// Publish ChatsUpdated event to notify frontend
	if len(updatedChats) > 0 {
		if err := m.publisher.PublishChatsUpdated(ctx, accountID, &protocol.ChatsUpdatedPayload{
			Chats: updatedChats,
		}); err != nil {
			m.log.Debugw("發送頭像更新事件失敗", "account_id", accountID, "error", err)
		} else {
			m.log.Infow("已發送頭像更新事件", "account_id", accountID, "updated_count", len(updatedChats))
		}
	}

	m.log.Debugw("頭像同步完成", "account_id", accountID, "success_count", successCount, "skipped_count", skippedCount, "fail_count", failCount, "total", len(chats))
}

// HandleSyncHistory handles the sync history command
func (m *Manager) HandleSyncHistory(ctx context.Context, cmd *protocol.Command, payload *protocol.SyncHistoryPayload) error {
	if _, err := m.getConnectedClient(ctx, cmd.AccountID); err != nil {
		return err
	}

	m.log.Debugw("同步歷史訊息", "account_id", cmd.AccountID, "chat_jid", payload.ChatJID, "count", payload.Count)

	return nil
}

// HandleSyncContacts handles the sync contacts command
func (m *Manager) HandleSyncContacts(ctx context.Context, cmd *protocol.Command) error {
	if _, err := m.getConnectedClient(ctx, cmd.AccountID); err != nil {
		return err
	}

	m.log.Debugw("同步聯絡人", "account_id", cmd.AccountID)

	return nil
}

// HandleUpdateProfile handles the update profile command
func (m *Manager) HandleUpdateProfile(ctx context.Context, cmd *protocol.Command) error {
	client, err := m.getConnectedClient(ctx, cmd.AccountID)
	if err != nil {
		return err
	}

	// Get current user's JID
	myJID := client.Store.ID
	if myJID == nil {
		return fmt.Errorf("無法取得用戶 JID")
	}

	// Create user JID without device ID (avatar is associated with phone number, not specific device)
	userJID := types.JID{
		User:   myJID.User,
		Server: types.DefaultUserServer,
	}

	payload := &protocol.ProfileUpdatedPayload{}

	// 1. Get avatar info
	pictureInfo, err := client.GetProfilePictureInfo(ctx, userJID, &whatsmeow.GetProfilePictureParams{
		Preview:     false,
		ExistingID:  "",
		IsCommunity: false,
	})
	if err != nil {
		m.log.Debugw("取得帳號頭像失敗", "account_id", cmd.AccountID, "error", err)
	} else if pictureInfo != nil {
		payload.Avatar = pictureInfo.URL
		payload.AvatarID = pictureInfo.ID
	}

	// 2. Get PushName
	pushName := client.Store.PushName
	if pushName != "" {
		payload.PushName = pushName
	}

	// 3. Try to get full name from contact info
	contactInfo, err := client.Store.Contacts.GetContact(ctx, *myJID)
	if err != nil {
		m.log.Warnw("取得帳號聯絡人資訊失敗", "account_id", cmd.AccountID, "error", err)
	} else {
		if contactInfo.FullName != "" {
			payload.FullName = contactInfo.FullName
		}
		if contactInfo.FirstName != "" {
			payload.FirstName = contactInfo.FirstName
		}
	}

	// 4. Fetch "keep chats archived" setting from AppState
	keepArchived, err := m.fetchKeepChatsArchived(ctx, client)
	if err != nil {
		m.log.Warnw("取得帳號 keep_chats_archived 設定失敗", "account_id", cmd.AccountID, "error", err)
	} else {
		payload.KeepChatsArchived = &keepArchived
	}

	// Publish ProfileUpdated event
	if err := m.publisher.PublishProfileUpdated(ctx, cmd.AccountID, payload); err != nil {
		m.log.Errorw("發送 ProfileUpdated 事件失敗", "account_id", cmd.AccountID, "error", err)
		return err
	}

	m.log.Debugw("帳號資料更新完成", "account_id", cmd.AccountID, "has_avatar", payload.Avatar != "", "push_name", payload.PushName)

	return nil
}

// sendAppStateSync 先確保本地 app state 與伺服器同步，再發送 patch。
// 若正常同步失敗（LTHash 不一致），會向手機請求 recovery snapshot。
func (m *Manager) sendAppStateSync(ctx context.Context, client *whatsmeow.Client, patch appstate.PatchInfo) error {
	if err := m.ensureAppStateSynced(ctx, client, patch.Type); err != nil {
		return fmt.Errorf("無法同步 %s app state: %w", patch.Type, err)
	}
	return client.SendAppState(ctx, patch)
}

// ensureAppStateSynced 確保 app state 已同步，依序嘗試：增量 → 全量 → 清除後全量 → recovery。
func (m *Manager) ensureAppStateSynced(ctx context.Context, client *whatsmeow.Client, name appstate.WAPatchName) error {
	// 1) 增量同步
	if err := client.FetchAppState(ctx, name, false, false); err == nil {
		return nil
	} else {
		m.log.Warnw("增量同步 app state 失敗", "patch_type", name, "error", err)
	}

	// 2) 全量重建
	if err := client.FetchAppState(ctx, name, true, false); err == nil {
		return nil
	} else {
		m.log.Warnw("全量重建 app state 失敗", "patch_type", name, "error", err)
	}

	// 3) 清除本地資料後重試
	if purgeErr := m.purgeAppState(ctx, client, string(name)); purgeErr != nil {
		m.log.Errorw("清除 app state 失敗", "error", purgeErr)
	} else if err := client.FetchAppState(ctx, name, true, false); err == nil {
		return nil
	} else {
		m.log.Warnw("清除後全量重建仍失敗", "patch_type", name, "error", err)
	}

	// 4) 向手機請求 recovery snapshot（繞過壞掉的 patch chain）
	m.log.Infow("嘗試向手機請求 app state recovery", "patch_type", name)
	if err := m.requestAppStateRecovery(ctx, client, name); err != nil {
		return fmt.Errorf("recovery 也失敗: %w", err)
	}
	return nil
}

// requestAppStateRecovery 向手機請求 app state recovery snapshot，
// 等待 AppStateSyncComplete（Recovery=true）事件後重試 FetchAppState。
func (m *Manager) requestAppStateRecovery(ctx context.Context, client *whatsmeow.Client, name appstate.WAPatchName) error {
	// 監聽 recovery 完成事件
	done := make(chan struct{}, 1)
	handlerID := client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.AppStateSyncComplete:
			if v.Name == name && v.Recovery {
				select {
				case done <- struct{}{}:
				default:
				}
			}
		}
	})
	defer client.RemoveEventHandler(handlerID)

	// 清除本地 app state（確保 recovery snapshot 可以被接受）
	_ = m.purgeAppState(ctx, client, string(name))

	// 發送 recovery 請求
	msg := whatsmeow.BuildAppStateRecoveryRequest(name)
	if _, err := client.SendPeerMessage(ctx, msg); err != nil {
		return fmt.Errorf("發送 recovery 請求失敗: %w", err)
	}

	// 等待 recovery 完成（手機需要處理並回傳 snapshot）
	select {
	case <-done:
		m.log.Infow("app state recovery 成功", "patch_type", name)
		return nil
	case <-time.After(15 * time.Second):
		return fmt.Errorf("等待 recovery 回應超時")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// purgeAppState 徹底清除指定裝置的 app state 資料（version + mutation MACs）
func (m *Manager) purgeAppState(ctx context.Context, client *whatsmeow.Client, name string) error {
	if client.Store.ID == nil {
		return fmt.Errorf("device JID not available")
	}
	jid := client.Store.ID.String()
	// FK CASCADE 會連帶刪除 whatsmeow_app_state_mutation_macs
	return m.db.WithContext(ctx).Exec(
		"DELETE FROM whatsmeow_app_state_version WHERE jid = ? AND name = ?", jid, name,
	).Error
}

// HandleUpdateSettings 處理裝置設定更新命令
func (m *Manager) HandleUpdateSettings(ctx context.Context, cmd *protocol.Command, payload *protocol.UpdateSettingsPayload) error {
	client, err := m.getConnectedClient(ctx, cmd.AccountID)
	if err != nil {
		return err
	}

	// 同步 push_name 到 WhatsApp
	if payload.PushName != nil {
		patch := appstate.BuildSettingPushName(*payload.PushName)
		if err := m.sendAppStateSync(ctx, client, patch); err != nil {
			return fmt.Errorf("同步 push_name 設定失敗: %w", err)
		}
		m.log.Infow("帳號 push_name 已同步到 WhatsApp", "account_id", cmd.AccountID, "push_name", *payload.PushName)
	}

	return nil
}

// fetchKeepChatsArchived 透過 AppState 取得「保持對話封存」設定
func (m *Manager) fetchKeepChatsArchived(ctx context.Context, client *whatsmeow.Client) (bool, error) {
	ch := make(chan bool, 1)

	handlerID := client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.AppState:
			if len(v.Index) > 0 && v.Index[0] == appstate.IndexSettingUnarchiveChats {
				if setting := v.SyncActionValue.GetUnarchiveChatsSetting(); setting != nil {
					select {
					case ch <- !setting.GetUnarchiveChats():
					default:
					}
				}
			}
		}
	})
	defer client.RemoveEventHandler(handlerID)

	prev := client.EmitAppStateEventsOnFullSync
	client.EmitAppStateEventsOnFullSync = true
	defer func() { client.EmitAppStateEventsOnFullSync = prev }()

	if err := client.FetchAppState(ctx, appstate.WAPatchRegularLow, true, false); err != nil {
		return false, fmt.Errorf("FetchAppState: %w", err)
	}

	select {
	case val := <-ch:
		return val, nil
	case <-time.After(3 * time.Second):
		return false, fmt.Errorf("timeout waiting for setting_unarchiveChats event")
	}
}

// HandleArchiveChat handles the archive chat command
func (m *Manager) HandleArchiveChat(ctx context.Context, cmd *protocol.Command, payload *protocol.ArchiveChatPayload) error {
	client, err := m.getConnectedClient(ctx, cmd.AccountID)
	if err != nil {
		return err
	}

	// Parse chat JID
	jid, err := types.ParseJID(payload.ChatJID)
	if err != nil {
		return fmt.Errorf("解析聊天 JID 失敗: %w", err)
	}

	// Build archive operation AppState patch
	patch := appstate.BuildArchive(jid, payload.Archive, time.Now(), nil)

	// Send AppState update
	if err := m.sendAppStateSync(ctx, client, patch); err != nil {
		return fmt.Errorf("發送歸檔狀態失敗: %w", err)
	}

	action := "歸檔"
	if !payload.Archive {
		action = "取消歸檔"
	}
	m.log.Infow("聊天歸檔操作成功", "account_id", cmd.AccountID, "action", action, "chat_jid", payload.ChatJID)

	return nil
}
