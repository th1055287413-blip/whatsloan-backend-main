package whatsmeow

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"whatsapp_golang/internal/protocol"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// HandleSendMessage handles the send message command
func (m *Manager) HandleSendMessage(ctx context.Context, cmd *protocol.Command, payload *protocol.SendMessagePayload) error {
	client, err := m.getConnectedClient(ctx, cmd.AccountID)
	if err != nil {
		return err
	}

	// 格式化為完整的 JID (如果 ToJID 不包含 @, 則添加 @s.whatsapp.net)
	toJID := payload.ToJID
	if !strings.Contains(toJID, "@") {
		toJID = toJID + "@s.whatsapp.net"
	}

	// Parse JID
	jid, err := types.ParseJID(toJID)
	if err != nil {
		return fmt.Errorf("無效的 JID: %w", err)
	}

	// Build message
	msg := &waProto.Message{
		Conversation: proto.String(payload.Content),
	}

	// Send message
	resp, err := client.SendMessage(ctx, jid, msg)
	if err != nil {
		return fmt.Errorf("發送訊息失敗: %w", err)
	}

	m.log.Debugw("訊息已發送", "account_id", cmd.AccountID, "to", toJID, "message_id", resp.ID)

	// Get sender JID
	senderJID := ""
	if client.Store.ID != nil {
		senderJID = client.Store.ID.String()
	}

	// Publish MessageReceived event (for saving message and broadcasting to WebSocket)
	if err := m.publisher.PublishMessageReceived(ctx, cmd.AccountID, &protocol.MessageReceivedPayload{
		MessageID:     resp.ID,
		ChatJID:       toJID,
		SenderJID:     senderJID,
		Content:       payload.Content,
		ContentType:   "text",
		Timestamp:     resp.Timestamp.UnixMilli(),
		IsFromMe:      true,
		SentByAdminID: payload.SentByAdminID,
		SenderType:    payload.SenderType,
	}); err != nil {
		m.log.Warnw("發送 MessageReceived 事件失敗", "account_id", cmd.AccountID, "error", err)
	}

	// Publish MessageSent event (for status update)
	if err := m.publisher.PublishMessageSent(ctx, cmd.AccountID, &protocol.MessageSentPayload{
		CommandID: cmd.ID,
		MessageID: resp.ID,
		ChatJID:   toJID,
		Timestamp: resp.Timestamp.UnixMilli(),
	}); err != nil {
		m.log.Warnw("發送 MessageSent 事件失敗", "account_id", cmd.AccountID, "error", err)
	}

	return nil
}

// HandleSendMedia handles the send media command
func (m *Manager) HandleSendMedia(ctx context.Context, cmd *protocol.Command, payload *protocol.SendMediaPayload) error {
	client, err := m.getConnectedClient(ctx, cmd.AccountID)
	if err != nil {
		return err
	}

	// 格式化為完整的 JID (如果 ToJID 不包含 @, 則添加 @s.whatsapp.net)
	toJID := payload.ToJID
	if !strings.Contains(toJID, "@") {
		toJID = toJID + "@s.whatsapp.net"
	}

	// Parse JID
	jid, err := types.ParseJID(toJID)
	if err != nil {
		return fmt.Errorf("無效的 JID: %w", err)
	}

	// Download media from URL
	mediaData, err := m.downloadMediaFromURL(ctx, payload.MediaURL)
	if err != nil {
		return fmt.Errorf("下載媒體失敗: %w", err)
	}

	// Build and upload media message
	msg, err := m.buildMediaMessage(ctx, client, payload.MediaType, mediaData, payload.Caption, payload.FileName)
	if err != nil {
		return err
	}

	// Send message
	resp, err := client.SendMessage(ctx, jid, msg)
	if err != nil {
		return fmt.Errorf("發送媒體訊息失敗: %w", err)
	}

	m.log.Debugw("媒體訊息已發送", "account_id", cmd.AccountID, "to", toJID, "media_type", payload.MediaType, "message_id", resp.ID)

	// Get sender JID
	senderJID := ""
	if client.Store.ID != nil {
		senderJID = client.Store.ID.String()
	}

	// 將本地路徑轉為前端可存取的 URL（uploads/image/x.jpg → /media/image/x.jpg）
	mediaURL := payload.MediaURL
	if strings.HasPrefix(mediaURL, "uploads/") {
		mediaURL = "/" + strings.Replace(mediaURL, "uploads/", "media/", 1)
	}

	// Publish MessageReceived event (for saving message and broadcasting to WebSocket)
	if err := m.publisher.PublishMessageReceived(ctx, cmd.AccountID, &protocol.MessageReceivedPayload{
		MessageID:     resp.ID,
		ChatJID:       toJID,
		SenderJID:     senderJID,
		Content:       payload.Caption,
		ContentType:   payload.MediaType,
		MediaURL:      mediaURL,
		Timestamp:     resp.Timestamp.UnixMilli(),
		IsFromMe:      true,
		SentByAdminID: payload.SentByAdminID,
		SenderType:    payload.SenderType,
	}); err != nil {
		m.log.Warnw("發送 MessageReceived 事件失敗", "account_id", cmd.AccountID, "error", err)
	}

	// Publish MessageSent event (for status update)
	if err := m.publisher.PublishMessageSent(ctx, cmd.AccountID, &protocol.MessageSentPayload{
		CommandID: cmd.ID,
		MessageID: resp.ID,
		ChatJID:   toJID,
		Timestamp: resp.Timestamp.UnixMilli(),
	}); err != nil {
		m.log.Warnw("發送 MessageSent 事件失敗", "account_id", cmd.AccountID, "error", err)
	}

	return nil
}

// buildMediaMessage uploads media and builds the appropriate message type
func (m *Manager) buildMediaMessage(
	ctx context.Context,
	client *whatsmeow.Client,
	mediaType string,
	data []byte,
	caption, fileName string,
) (*waProto.Message, error) {
	mimeType := detectMimeType(data)
	fileLength := proto.Uint64(uint64(len(data)))

	switch mediaType {
	case "image":
		uploaded, err := client.Upload(ctx, data, whatsmeow.MediaImage)
		if err != nil {
			return nil, fmt.Errorf("上傳圖片失敗: %w", err)
		}
		return &waProto.Message{
			ImageMessage: &waProto.ImageMessage{
				Caption:       proto.String(caption),
				Mimetype:      proto.String(mimeType),
				URL:           &uploaded.URL,
				DirectPath:    &uploaded.DirectPath,
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    fileLength,
			},
		}, nil

	case "video":
		uploaded, err := client.Upload(ctx, data, whatsmeow.MediaVideo)
		if err != nil {
			return nil, fmt.Errorf("上傳影片失敗: %w", err)
		}
		return &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				Caption:       proto.String(caption),
				Mimetype:      proto.String(mimeType),
				URL:           &uploaded.URL,
				DirectPath:    &uploaded.DirectPath,
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    fileLength,
			},
		}, nil

	case "audio":
		uploaded, err := client.Upload(ctx, data, whatsmeow.MediaAudio)
		if err != nil {
			return nil, fmt.Errorf("上傳音訊失敗: %w", err)
		}
		return &waProto.Message{
			AudioMessage: &waProto.AudioMessage{
				Mimetype:      proto.String(mimeType),
				URL:           &uploaded.URL,
				DirectPath:    &uploaded.DirectPath,
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    fileLength,
			},
		}, nil

	case "document":
		uploaded, err := client.Upload(ctx, data, whatsmeow.MediaDocument)
		if err != nil {
			return nil, fmt.Errorf("上傳文件失敗: %w", err)
		}
		if fileName == "" {
			fileName = "document"
		}
		return &waProto.Message{
			DocumentMessage: &waProto.DocumentMessage{
				FileName:      proto.String(fileName),
				Mimetype:      proto.String(mimeType),
				URL:           &uploaded.URL,
				DirectPath:    &uploaded.DirectPath,
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    fileLength,
			},
		}, nil

	default:
		return nil, fmt.Errorf("不支援的媒體類型: %s", mediaType)
	}
}

// HandleRevokeMessage handles the revoke message command
func (m *Manager) HandleRevokeMessage(ctx context.Context, cmd *protocol.Command, payload *protocol.RevokeMessagePayload) error {
	client, err := m.getConnectedClient(ctx, cmd.AccountID)
	if err != nil {
		return err
	}

	// Parse chat JID
	chatJID, err := types.ParseJID(payload.ChatJID)
	if err != nil {
		return fmt.Errorf("無效的聊天 JID: %w", err)
	}

	// Build revoke message (empty sender JID means revoking own message)
	revokeMsg := client.BuildRevoke(chatJID, types.EmptyJID, payload.MessageID)

	// Send revoke request
	_, err = client.SendMessage(ctx, chatJID, revokeMsg)
	if err != nil {
		return fmt.Errorf("發送撤銷訊息失敗: %w", err)
	}

	m.log.Debugw("WhatsApp 訊息已撤銷", "account_id", cmd.AccountID, "chat_jid", payload.ChatJID, "message_id", payload.MessageID)

	return nil
}

// HandleDeleteMessageForMe handles the delete message for me command (syncs across linked devices)
func (m *Manager) HandleDeleteMessageForMe(ctx context.Context, cmd *protocol.Command, payload *protocol.DeleteMessageForMePayload) error {
	client, err := m.getConnectedClient(ctx, cmd.AccountID)
	if err != nil {
		return err
	}

	chatJID, err := types.ParseJID(payload.ChatJID)
	if err != nil {
		return fmt.Errorf("無效的聊天 JID: %w", err)
	}
	// 使用 ToNonAD() 移除 device ID，確保 JID 格式正確
	chatJIDStr := chatJID.ToNonAD().String()

	isFromMe := "0"
	if payload.IsFromMe {
		isFromMe = "1"
	}

	// senderJID 只在群組聊天時需要設定，私聊時應為 "0"
	senderJID := "0"
	isGroup := strings.HasSuffix(chatJIDStr, "@g.us")
	if isGroup && payload.SenderJID != "" {
		// 群組聊天：設定實際發送者 JID（移除 device ID）
		if parsedSender, err := types.ParseJID(payload.SenderJID); err == nil {
			senderJID = parsedSender.ToNonAD().String()
		} else {
			senderJID = payload.SenderJID
		}
	}

	index := []string{appstate.IndexDeleteMessageForMe, chatJIDStr, payload.MessageID, isFromMe, senderJID}
	m.log.Debugw("DeleteForMe 準備發送", "account_id", cmd.AccountID, "index", index, "timestamp", payload.MessageTimestamp)

	patch := appstate.PatchInfo{
		Type: appstate.WAPatchRegularHigh,
		Mutations: []appstate.MutationInfo{{
			Index:   index,
			Version: 2,
			Value: &waSyncAction.SyncActionValue{
				DeleteMessageForMeAction: &waSyncAction.DeleteMessageForMeAction{
					DeleteMedia:      proto.Bool(true),
					MessageTimestamp: proto.Int64(payload.MessageTimestamp),
				},
			},
		}},
	}

	if err := client.SendAppState(ctx, patch); err != nil {
		return fmt.Errorf("發送 DeleteForMe 失敗: %w", err)
	}

	m.log.Debugw("WhatsApp 訊息已 DeleteForMe", "account_id", cmd.AccountID, "chat_jid", payload.ChatJID, "message_id", payload.MessageID, "is_from_me", isFromMe, "sender_jid", senderJID)

	return nil
}

// downloadMediaFromURL downloads media from a URL or reads from local filesystem
func (m *Manager) downloadMediaFromURL(ctx context.Context, mediaURL string) ([]byte, error) {
	// 非 HTTP(S) URL 視為本地檔案路徑（API 與 Connector 共用 volume）
	if !strings.HasPrefix(mediaURL, "http://") && !strings.HasPrefix(mediaURL, "https://") {
		data, err := os.ReadFile(mediaURL)
		if err != nil {
			return nil, fmt.Errorf("讀取本地媒體檔案失敗: %w", err)
		}
		return data, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("建立請求失敗: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("請求失敗: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP 狀態碼: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("讀取回應失敗: %w", err)
	}

	return data, nil
}

// detectMimeType detects the MIME type of media data
func detectMimeType(data []byte) string {
	return http.DetectContentType(data)
}
