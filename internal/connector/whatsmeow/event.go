package whatsmeow

import (
	"context"
	"fmt"
	"time"

	"whatsapp_golang/internal/protocol"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// resolveLIDToPN attempts to resolve a LID JID to its phone number JID using the client's LID store
func (m *Manager) resolveLIDToPN(ctx context.Context, client *whatsmeow.Client, jid types.JID) (types.JID, bool) {
	if jid.Server != types.HiddenUserServer {
		return jid, false // Not a LID, return as-is
	}

	pn, err := client.Store.LIDs.GetPNForLID(ctx, jid)
	if err != nil || pn.IsEmpty() {
		m.log.Debugw("無法解析 LID 到電話號碼", "lid", jid.String(), "error", err)
		return jid, false
	}

	m.log.Debugw("LID 解析成功", "lid", jid.String(), "phone", pn.String())
	return pn, true
}

// parseMessageContent parses message content and returns content, contentType, and whether to skip
func parseMessageContent(msg *waProto.Message) (content string, contentType string, skip bool) {
	// Skip protocol messages (handled separately for revoke detection)
	if msg.GetProtocolMessage() != nil {
		return "", "", true
	}
	if msg.GetSenderKeyDistributionMessage() != nil {
		return "", "", true
	}

	contentType = "text"

	switch {
	case msg.GetConversation() != "":
		content = msg.GetConversation()
	case msg.GetExtendedTextMessage() != nil:
		content = msg.GetExtendedTextMessage().GetText()
	case msg.GetImageMessage() != nil:
		content = msg.GetImageMessage().GetCaption()
		contentType = "image"
	case msg.GetVideoMessage() != nil:
		content = msg.GetVideoMessage().GetCaption()
		contentType = "video"
	case msg.GetAudioMessage() != nil:
		contentType = "audio"
	case msg.GetDocumentMessage() != nil:
		content = msg.GetDocumentMessage().GetFileName()
		contentType = "document"
	case msg.GetStickerMessage() != nil:
		contentType = "sticker"
	case msg.GetReactionMessage() != nil:
		content = msg.GetReactionMessage().GetText()
		contentType = "reaction"
	case msg.GetLocationMessage() != nil:
		loc := msg.GetLocationMessage()
		content = fmt.Sprintf("%.6f,%.6f", loc.GetDegreesLatitude(), loc.GetDegreesLongitude())
		contentType = "location"
	case msg.GetContactMessage() != nil:
		content = msg.GetContactMessage().GetDisplayName()
		contentType = "contact"
	case msg.GetLiveLocationMessage() != nil:
		loc := msg.GetLiveLocationMessage()
		content = fmt.Sprintf("%.6f,%.6f", loc.GetDegreesLatitude(), loc.GetDegreesLongitude())
		contentType = "live_location"
	case msg.GetPollCreationMessage() != nil:
		content = msg.GetPollCreationMessage().GetName()
		contentType = "poll"
	default:
		// Unknown message type
		return "", "", true
	}

	return content, contentType, false
}

// buildMessagePayload builds a message payload from an event
func buildMessagePayload(msgEvt *events.Message, content, contentType string, isHistory bool) *protocol.MessageReceivedPayload {
	payload := &protocol.MessageReceivedPayload{
		MessageID:   msgEvt.Info.ID,
		ChatJID:     msgEvt.Info.Chat.String(),
		SenderJID:   msgEvt.Info.Sender.String(),
		SenderName:  msgEvt.Info.PushName,
		Content:     content,
		ContentType: contentType,
		Timestamp:   msgEvt.Info.Timestamp.UnixMilli(),
		IsGroup:     msgEvt.Info.IsGroup,
		IsFromMe:    msgEvt.Info.IsFromMe,
		IsHistory:   isHistory,
	}

	// LID ↔ PhoneJID mapping support
	if !msgEvt.Info.SenderAlt.IsEmpty() {
		payload.SenderAltJID = msgEvt.Info.SenderAlt.String()
	}
	if !msgEvt.Info.RecipientAlt.IsEmpty() {
		payload.RecipientAltJID = msgEvt.Info.RecipientAlt.String()
	}
	if msgEvt.Info.AddressingMode != "" {
		payload.AddressingMode = string(msgEvt.Info.AddressingMode)
	}

	return payload
}

// createEventHandler creates a whatsmeow event handler for the given account
func (m *Manager) createEventHandler(accountID uint) func(interface{}) {
	return func(evt interface{}) {
		// 攔截 full sync 的歸檔事件，存入 buffer 由 HandleSyncChats 批次處理，
		// 避免數百個無用事件灌爆 worker queue
		if archiveEvt, ok := evt.(*events.Archive); ok && archiveEvt.FromFullSync {
			m.archiveBufMu.Lock()
			m.archiveBuf[accountID] = append(m.archiveBuf[accountID], protocol.ChatArchiveItem{
				ChatJID:  archiveEvt.JID.String(),
				Archived: archiveEvt.Action.GetArchived(),
			})
			m.archiveBufMu.Unlock()
			return
		}

		start := time.Now()
		m.mu.RLock()
		w := m.eventWorkers[accountID]
		m.mu.RUnlock()

		if w != nil {
			w.send(evt)
			m.log.Debugw("event handler callback 完成 (worker)",
				"account_id", accountID, "event_type", fmt.Sprintf("%T", evt), "elapsed", time.Since(start))
		} else {
			m.log.Warnw("event handler 走同步路徑（worker 未啟動）",
				"account_id", accountID, "event_type", fmt.Sprintf("%T", evt))
			m.dispatchEvent(context.Background(), accountID, evt)
		}
	}
}

// dispatchEvent 實際的事件分發邏輯，由 event worker goroutine 呼叫
func (m *Manager) dispatchEvent(ctx context.Context, accountID uint, evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		m.handleMessage(ctx, accountID, v)

	case *events.Receipt:
		m.handleReceipt(ctx, accountID, v)

	case *events.Connected:
		m.log.Infow("帳號已連線", "account_id", accountID)
		m.mu.Lock()
		if info, exists := m.accountInfo[accountID]; exists {
			info.Connected = true
			info.LastSeen = time.Now()
		}
		// Get client to obtain device_id
		client := m.clients[accountID]
		m.mu.Unlock()

		// Publish Connected event with device_id
		deviceID := ""
		phoneNumber := ""
		platform := ""
		businessName := ""
		if client != nil && client.Store.ID != nil {
			deviceID = client.Store.ID.String()
			phoneNumber = client.Store.ID.User
		}
		if client != nil {
			platform = client.Store.Platform
			businessName = client.Store.BusinessName
		}
		m.publisher.PublishConnected(ctx, accountID, &protocol.ConnectedPayload{
			PhoneNumber:  phoneNumber,
			Platform:     platform,
			BusinessName: businessName,
			DeviceID:     deviceID,
		})

	case *events.Disconnected:
		m.log.Warnw("帳號已斷線", "account_id", accountID)
		m.mu.Lock()
		if info, exists := m.accountInfo[accountID]; exists {
			info.Connected = false
			info.LastSeen = time.Now()
		}
		m.mu.Unlock()

		// Publish Disconnected event
		m.publisher.PublishDisconnected(ctx, accountID, &protocol.DisconnectedPayload{
			Reason: "connection_lost",
		})

	case *events.LoggedOut:
		m.log.Warnw("帳號已登出", "account_id", accountID, "reason", v.Reason)

		// Publish LoggedOut event（在 removeAccount 之前，確保事件送出）
		m.publisher.PublishLoggedOut(ctx, accountID, &protocol.LoggedOutPayload{
			Reason: fmt.Sprintf("%v", v.Reason),
		})

		// 異步移除帳號 + 停止 worker，避免在 worker goroutine 內死鎖
		go m.removeAccount(accountID)

	case *events.HistorySync:
		m.log.Debugw("收到 HistorySync 事件", "account_id", accountID)
		m.handleHistorySync(ctx, accountID, v)

	case *events.Archive:
		// FromFullSync 事件已在 createEventHandler 攔截，這裡只處理即時變更
		archived := v.Action.GetArchived()
		m.log.Infow("聊天歸檔狀態變更", "account_id", accountID, "jid", v.JID.String(), "archived", archived)
		m.publisher.PublishChatArchiveChanged(ctx, accountID, &protocol.ChatArchiveChangedPayload{
			ChatJID:   v.JID.String(),
			Archived:  archived,
			Timestamp: v.Timestamp.Unix(),
		})

	case *events.DeleteForMe:
		if v.FromFullSync {
			return
		}
		m.log.Infow("收到 DeleteForMe 事件",
			"account_id", accountID,
			"message_id", v.MessageID,
			"chat_jid", v.ChatJID.String(),
			"is_from_me", v.IsFromMe)
		m.publisher.PublishMessageDeletedForMe(ctx, accountID, &protocol.MessageDeletedForMePayload{
			MessageID: v.MessageID,
			ChatJID:   v.ChatJID.String(),
			SenderJID: v.SenderJID.String(),
			IsFromMe:  v.IsFromMe,
			Timestamp: v.Timestamp.UnixMilli(),
		})

	case *events.UnarchiveChatsSetting:
		keepArchived := !v.Action.GetUnarchiveChats()
		m.log.Infow("保持對話封存設定變更", "account_id", accountID, "keep_archived", keepArchived)
		m.publisher.PublishProfileUpdated(ctx, accountID, &protocol.ProfileUpdatedPayload{
			KeepChatsArchived: &keepArchived,
		})
	}
}

// handleMessage handles incoming messages
func (m *Manager) handleMessage(ctx context.Context, accountID uint, msg *events.Message) {
	// 跳過 status broadcast 訊息（Status 動態）
	if msg.Info.Chat == types.StatusBroadcastJID {
		m.log.Debugw("跳過 status broadcast 訊息", "msg_id", msg.Info.ID)
		return
	}

	// Debug: log all JID info for LID investigation
	m.log.Debugw("handleMessage JID 詳情",
		"account_id", accountID,
		"msg_id", msg.Info.ID,
		"chat", msg.Info.Chat.String(),
		"sender", msg.Info.Sender.String(),
		"sender_alt", msg.Info.SenderAlt.String(),
		"recipient_alt", msg.Info.RecipientAlt.String(),
		"addressing_mode", msg.Info.AddressingMode)

	// Check for protocol messages in Message (e.g., revoke)
	if pm := msg.Message.GetProtocolMessage(); pm != nil {
		m.log.Infow("handleMessage: ProtocolMessage (from Message)", "type", pm.GetType())
		m.handleProtocolMessage(ctx, accountID, msg, pm)
		return
	}

	// Check for protocol messages in RawMessage (whatsmeow might have unwrapped Message for edits)
	// This is necessary because whatsmeow replaces msg.Message with edited content for MESSAGE_EDIT
	if msg.RawMessage != nil {
		// RawMessage might be wrapped in DeviceSentMessage
		rawMsg := msg.RawMessage
		if rawMsg.GetDeviceSentMessage().GetMessage() != nil {
			rawMsg = rawMsg.GetDeviceSentMessage().GetMessage()
		}
		if pm := rawMsg.GetProtocolMessage(); pm != nil {
			m.log.Infow("handleMessage: ProtocolMessage (from RawMessage)", "type", pm.GetType())
			m.handleProtocolMessage(ctx, accountID, msg, pm)
			return
		}
	}

	content, contentType, skip := parseMessageContent(msg.Message)
	if skip {
		m.log.Debugw("跳過訊息", "msg_id", msg.Info.ID)
		return
	}

	// Check if this is an edited message
	if msg.IsEdit {
		m.log.Infow("偵測到編輯訊息", "account_id", accountID, "msg_id", msg.Info.ID, "content", content)
		m.handleEditedMessage(ctx, accountID, msg, content)
		return
	}

	// Download and upload media if applicable
	m.mu.RLock()
	client := m.clients[accountID]
	m.mu.RUnlock()

	var mediaURL string
	switch contentType {
	case "image", "video", "audio", "document", "sticker":
		if client != nil {
			mediaURL = m.downloadAndUploadMedia(ctx, client, msg, contentType)
		}
	}

	payload := buildMessagePayload(msg, content, contentType, false)
	payload.MediaURL = mediaURL

	// Resolve LID JIDs to phone number JIDs if possible

	if client != nil {
		// Resolve Chat JID if it's a LID
		chatJID := msg.Info.Chat
		if resolvedChat, resolved := m.resolveLIDToPN(ctx, client, chatJID); resolved {
			payload.ChatAltJID = payload.ChatJID // Keep original LID as alt
			payload.ChatJID = resolvedChat.String()
		}

		// Resolve Sender JID if it's a LID (for non-group chats)
		if !msg.Info.IsGroup {
			senderJID := msg.Info.Sender
			if resolvedSender, resolved := m.resolveLIDToPN(ctx, client, senderJID); resolved {
				if payload.SenderAltJID == "" {
					payload.SenderAltJID = payload.SenderJID
				}
				payload.SenderJID = resolvedSender.String()
			}
		}
	}

	if err := m.publisher.PublishMessageReceived(ctx, accountID, payload); err != nil {
		m.log.Warnw("發送 MessageReceived 事件失敗", "account_id", accountID, "error", err)
	}
}

// handleEditedMessage handles edited messages
func (m *Manager) handleEditedMessage(ctx context.Context, accountID uint, msg *events.Message, newContent string) {
	m.log.Infow("收到編輯訊息事件",
		"account_id", accountID,
		"message_id", msg.Info.ID,
		"chat_jid", msg.Info.Chat.String())

	payload := &protocol.MessageEditedPayload{
		MessageID:  msg.Info.ID,
		ChatJID:    msg.Info.Chat.String(),
		NewContent: newContent,
		SenderJID:  msg.Info.Sender.String(),
		IsFromMe:   msg.Info.IsFromMe,
		Timestamp:  msg.Info.Timestamp.UnixMilli(),
	}

	if err := m.publisher.PublishMessageEdited(ctx, accountID, payload); err != nil {
		m.log.Warnw("發送 MessageEdited 事件失敗", "account_id", accountID, "error", err)
	}
}

// handleReceipt handles message receipts and forwards to Gateway via Redis Stream
func (m *Manager) handleReceipt(ctx context.Context, accountID uint, receipt *events.Receipt) {
	switch receipt.Type {
	case types.ReceiptTypeRead, types.ReceiptTypeReadSelf,
		types.ReceiptTypeDelivered, types.ReceiptTypePlayed, types.ReceiptTypePlayedSelf:
	default:
		return
	}

	if len(receipt.MessageIDs) == 0 {
		return
	}

	payload := &protocol.ReceiptPayload{
		MessageID:   string(receipt.MessageIDs[0]),
		ChatJID:     receipt.Chat.String(),
		ReceiptType: string(receipt.Type),
		IsFromMe:    receipt.IsFromMe,
		Timestamp:   receipt.Timestamp.UnixMilli(),
	}

	if err := m.publisher.PublishReceipt(ctx, accountID, payload); err != nil {
		m.log.Warnw("發送 Receipt 事件失敗", "account_id", accountID, "error", err)
	}
}

// handleHistorySync handles history sync events
func (m *Manager) handleHistorySync(ctx context.Context, accountID uint, evt *events.HistorySync) {
	m.log.Infow("收到歷史同步事件",
		"account_id", accountID,
		"type", evt.Data.GetSyncType(),
		"conversations", len(evt.Data.Conversations))

	totalMessages := 0
	for _, conv := range evt.Data.Conversations {
		totalMessages += len(conv.GetMessages())
	}

	if totalMessages == 0 {
		m.log.Debugw("歷史同步不包含任何訊息", "account_id", accountID)
		return
	}

	// Get client for parsing messages
	m.mu.RLock()
	client, exists := m.clients[accountID]
	m.mu.RUnlock()

	if !exists {
		m.log.Warnw("client 不存在，無法處理歷史同步", "account_id", accountID)
		return
	}

	// 建立媒體下載 worker pool（非同步下載，不阻塞歷史同步）
	mediaPool := newMediaDownloadPool(ctx, m, client, accountID)
	defer mediaPool.stop()

	const historySyncBatchSize = 100

	publishedCount := 0
	batch := make([]*protocol.MessageReceivedPayload, 0, historySyncBatchSize)

	flushBatch := func() {
		if len(batch) == 0 {
			return
		}
		count, err := m.publisher.PublishMessageReceivedBatch(ctx, accountID, batch)
		publishedCount += count
		if err != nil {
			m.log.Warnw("批次發送歷史訊息部分失敗", "account_id", accountID, "error", err)
		}
		batch = batch[:0]
	}

	// Process history messages for each conversation
	for _, conversation := range evt.Data.Conversations {
		if ctx.Err() != nil {
			m.log.Infow("歷史同步中斷，帳號已移除", "account_id", accountID, "published_count", publishedCount)
			return
		}

		chatJID := conversation.GetID()
		msgCount := len(conversation.GetMessages())

		if msgCount == 0 {
			continue
		}

		// Parse chat JID
		jid, err := types.ParseJID(chatJID)
		if err != nil {
			m.log.Errorw("解析聊天 JID 失敗", "jid", chatJID, "error", err)
			continue
		}

		// Process each history message
		for _, historyMsg := range conversation.GetMessages() {
			if ctx.Err() != nil {
				flushBatch()
				m.log.Infow("歷史同步中斷，帳號已移除", "account_id", accountID, "published_count", publishedCount)
				return
			}

			// Use ParseWebMessage to parse history messages
			msgEvt, err := client.ParseWebMessage(jid, historyMsg.GetMessage())
			if err != nil {
				m.log.Debugw("解析歷史訊息失敗", "error", err)
				continue
			}

			// Use shared function to parse message content
			content, contentType, skip := parseMessageContent(msgEvt.Message)
			if skip {
				continue
			}

			// Use shared function to build payload
			batch = append(batch, buildMessagePayload(msgEvt, content, contentType, true))

			// 只對近 7 天的媒體訊息嘗試下載（更舊的 URL 大概率已過期）
			// 用 Redis SET 去重，避免帳號重連時重複下載
			switch contentType {
			case "image", "video", "audio", "document", "sticker":
				if time.Since(msgEvt.Info.Timestamp) <= 7*24*time.Hour {
					dedupeKey := fmt.Sprintf("wa:media:done:%d:%s", accountID, msgEvt.Info.ID)
					if added, _ := m.redis.SetNX(ctx, dedupeKey, 1, 8*24*time.Hour).Result(); added {
						mediaPool.enqueue(&mediaDownloadTask{
							msg:         msgEvt,
							contentType: contentType,
							messageID:   msgEvt.Info.ID,
							chatJID:     msgEvt.Info.Chat.String(),
							dedupeKey:   dedupeKey,
						})
					}
				}
			}

			if len(batch) >= historySyncBatchSize {
				flushBatch()
			}
		}
	}

	flushBatch()

	m.log.Infow("歷史同步完成", "account_id", accountID, "published_count", publishedCount)

	// Publish sync complete event
	if err := m.publisher.PublishSyncComplete(ctx, accountID, &protocol.SyncCompletePayload{
		SyncType: "history",
		Count:    publishedCount,
	}); err != nil {
		m.log.Warnw("發送 SyncComplete 事件失敗", "account_id", accountID, "error", err)
	}
}

// handleProtocolMessage handles protocol messages (revoke, edit, etc.)
func (m *Manager) handleProtocolMessage(ctx context.Context, accountID uint, msg *events.Message, pm *waProto.ProtocolMessage) {
	switch pm.GetType() {
	case waProto.ProtocolMessage_REVOKE:
		m.handleRevokeProtocolMessage(ctx, accountID, msg, pm)
	case waProto.ProtocolMessage_MESSAGE_EDIT:
		m.handleEditProtocolMessage(ctx, accountID, msg, pm)
	default:
		m.log.Debugw("未處理的 ProtocolMessage 類型", "type", pm.GetType())
	}
}

// handleRevokeProtocolMessage handles revoke protocol messages
func (m *Manager) handleRevokeProtocolMessage(ctx context.Context, accountID uint, msg *events.Message, pm *waProto.ProtocolMessage) {
	// Get the revoked message ID from the Key
	revokedKey := pm.GetKey()
	if revokedKey == nil {
		m.log.Warnw("撤回訊息缺少 Key", "msg_id", msg.Info.ID)
		return
	}

	revokedMessageID := revokedKey.GetID()
	if revokedMessageID == "" {
		m.log.Warnw("撤回訊息缺少訊息 ID", "msg_id", msg.Info.ID)
		return
	}

	// Determine chat JID and sender info
	chatJID := msg.Info.Chat.String()
	senderJID := ""
	isFromMe := revokedKey.GetFromMe()

	// If not from me, get the sender JID from the key
	if !isFromMe && revokedKey.GetParticipant() != "" {
		senderJID = revokedKey.GetParticipant()
	} else if !isFromMe {
		senderJID = msg.Info.Sender.String()
	}

	m.log.Infow("收到撤回訊息事件",
		"account_id", accountID,
		"revoked_message_id", revokedMessageID,
		"chat_jid", chatJID,
		"is_from_me", isFromMe)

	payload := &protocol.MessageRevokedPayload{
		MessageID: revokedMessageID,
		ChatJID:   chatJID,
		SenderJID: senderJID,
		IsFromMe:  isFromMe,
		Timestamp: msg.Info.Timestamp.UnixMilli(),
	}

	if err := m.publisher.PublishMessageRevoked(ctx, accountID, payload); err != nil {
		m.log.Warnw("發送 MessageRevoked 事件失敗", "account_id", accountID, "error", err)
	}
}

// handleEditProtocolMessage handles edit protocol messages
func (m *Manager) handleEditProtocolMessage(ctx context.Context, accountID uint, msg *events.Message, pm *waProto.ProtocolMessage) {
	// Get the original message ID from the Key
	editKey := pm.GetKey()
	if editKey == nil {
		m.log.Warnw("編輯訊息缺少 Key", "msg_id", msg.Info.ID)
		return
	}

	originalMessageID := editKey.GetID()
	if originalMessageID == "" {
		m.log.Warnw("編輯訊息缺少原始訊息 ID", "msg_id", msg.Info.ID)
		return
	}

	// Get the edited content from ProtocolMessage.EditedMessage
	editedMsg := pm.GetEditedMessage()
	if editedMsg == nil {
		m.log.Warnw("編輯訊息缺少編輯內容", "msg_id", msg.Info.ID)
		return
	}

	// Parse the edited message content
	newContent, _, skip := parseMessageContent(editedMsg)
	if skip || newContent == "" {
		m.log.Warnw("無法解析編輯訊息內容", "msg_id", msg.Info.ID)
		return
	}

	chatJID := msg.Info.Chat.String()
	isFromMe := editKey.GetFromMe()

	m.log.Infow("收到編輯訊息事件 (ProtocolMessage)",
		"account_id", accountID,
		"original_message_id", originalMessageID,
		"chat_jid", chatJID,
		"is_from_me", isFromMe,
		"new_content", newContent)

	payload := &protocol.MessageEditedPayload{
		MessageID:  originalMessageID,
		ChatJID:    chatJID,
		NewContent: newContent,
		SenderJID:  msg.Info.Sender.String(),
		IsFromMe:   isFromMe,
		Timestamp:  msg.Info.Timestamp.UnixMilli(),
	}

	if err := m.publisher.PublishMessageEdited(ctx, accountID, payload); err != nil {
		m.log.Warnw("發送 MessageEdited 事件失敗", "account_id", accountID, "error", err)
	}
}
