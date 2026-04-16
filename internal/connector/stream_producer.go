package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
)

// StreamProducer 事件生產者，負責發送事件到主服務
type StreamProducer struct {
	client      *redis.Client
	connectorID string
	stopped     atomic.Bool
}

// NewStreamProducer 建立事件生產者
func NewStreamProducer(client *redis.Client, connectorID string) *StreamProducer {
	return &StreamProducer{
		client:      client,
		connectorID: connectorID,
	}
}

// Stop 標記生產者已停止，後續 SendEvent 將直接返回
func (p *StreamProducer) Stop() {
	p.stopped.Store(true)
}

// SendEvent 發送事件到事件 Stream
func (p *StreamProducer) SendEvent(ctx context.Context, event *protocol.Event) error {
	if p.stopped.Load() {
		return nil
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化事件失敗: %w", err)
	}

	// 根據事件類型選擇 stream 和 MaxLen
	var streamName string
	var maxLen int64
	switch {
	case protocol.IsPriorityEvent(event.Type):
		streamName = protocol.PriorityEventStreamName
		maxLen = 10000
	case protocol.IsMessageEvent(event.Type):
		streamName = protocol.MessageEventStreamName
		maxLen = 50000
	default:
		streamName = protocol.EventStreamName
		maxLen = 30000
	}

	_, err = p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamName,
		MaxLen: maxLen,
		Approx: true,
		Values: map[string]interface{}{
			"event":        string(data),
			"connector_id": p.connectorID,
			"type":         string(event.Type),
		},
	}).Result()

	if err != nil {
		return fmt.Errorf("發送事件失敗: %w", err)
	}

	return nil
}

// PublishMessageReceived 發送收到訊息事件
func (p *StreamProducer) PublishMessageReceived(ctx context.Context, accountID uint, payload *protocol.MessageReceivedPayload) error {
	event, err := protocol.NewEvent(protocol.EvtMessageReceived, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishMessageReceivedBatch 批次發送收到訊息事件（使用 Redis Pipeline 減少往返次數）
func (p *StreamProducer) PublishMessageReceivedBatch(ctx context.Context, accountID uint, payloads []*protocol.MessageReceivedPayload) (int, error) {
	if p.stopped.Load() || len(payloads) == 0 {
		return 0, nil
	}

	pipe := p.client.Pipeline()

	for _, payload := range payloads {
		event, err := protocol.NewEvent(protocol.EvtMessageReceived, p.connectorID, accountID, payload)
		if err != nil {
			return 0, fmt.Errorf("建立事件失敗: %w", err)
		}

		data, err := json.Marshal(event)
		if err != nil {
			return 0, fmt.Errorf("序列化事件失敗: %w", err)
		}

		pipe.XAdd(ctx, &redis.XAddArgs{
			Stream: protocol.MessageEventStreamName,
			MaxLen: 50000,
			Approx: true,
			Values: map[string]interface{}{
				"event":        string(data),
				"connector_id": p.connectorID,
				"type":         string(protocol.EvtMessageReceived),
			},
		})
	}

	cmds, err := pipe.Exec(ctx)
	if err == nil {
		return len(payloads), nil
	}

	// 統計成功數量
	published := 0
	for _, cmd := range cmds {
		if cmd.Err() == nil {
			published++
		}
	}
	return published, fmt.Errorf("批次發送部分失敗: %d/%d succeeded: %w", published, len(payloads), err)
}

// PublishMessageSent 發送訊息已發送事件
func (p *StreamProducer) PublishMessageSent(ctx context.Context, accountID uint, payload *protocol.MessageSentPayload) error {
	event, err := protocol.NewEvent(protocol.EvtMessageSent, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishReceipt 發送訊息回執事件
func (p *StreamProducer) PublishReceipt(ctx context.Context, accountID uint, payload *protocol.ReceiptPayload) error {
	event, err := protocol.NewEvent(protocol.EvtReceipt, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishConnected 發送帳號已連線事件
func (p *StreamProducer) PublishConnected(ctx context.Context, accountID uint, payload *protocol.ConnectedPayload) error {
	event, err := protocol.NewEvent(protocol.EvtConnected, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishDisconnected 發送帳號已斷線事件
func (p *StreamProducer) PublishDisconnected(ctx context.Context, accountID uint, payload *protocol.DisconnectedPayload) error {
	event, err := protocol.NewEvent(protocol.EvtDisconnected, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishLoggedOut 發送帳號已登出事件
func (p *StreamProducer) PublishLoggedOut(ctx context.Context, accountID uint, payload *protocol.LoggedOutPayload) error {
	event, err := protocol.NewEvent(protocol.EvtLoggedOut, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishQRCode 發送 QR Code 事件
func (p *StreamProducer) PublishQRCode(ctx context.Context, accountID uint, payload *protocol.QRCodePayload) error {
	event, err := protocol.NewEvent(protocol.EvtQRCode, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishPairingCode 發送配對碼事件
func (p *StreamProducer) PublishPairingCode(ctx context.Context, accountID uint, payload *protocol.PairingCodePayload) error {
	event, err := protocol.NewEvent(protocol.EvtPairingCode, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishLoginSuccess 發送登入成功事件
func (p *StreamProducer) PublishLoginSuccess(ctx context.Context, accountID uint, payload *protocol.LoginSuccessPayload) error {
	event, err := protocol.NewEvent(protocol.EvtLoginSuccess, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishLoginFailed 發送登入失敗事件
func (p *StreamProducer) PublishLoginFailed(ctx context.Context, accountID uint, payload *protocol.LoginFailedPayload) error {
	event, err := protocol.NewEvent(protocol.EvtLoginFailed, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishLoginCancelled 發送登入已取消事件
func (p *StreamProducer) PublishLoginCancelled(ctx context.Context, accountID uint, payload *protocol.LoginCancelledPayload) error {
	event, err := protocol.NewEvent(protocol.EvtLoginCancelled, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishSyncProgress 發送同步進度事件
func (p *StreamProducer) PublishSyncProgress(ctx context.Context, accountID uint, payload *protocol.SyncProgressPayload) error {
	event, err := protocol.NewEvent(protocol.EvtSyncProgress, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishSyncComplete 發送同步完成事件
func (p *StreamProducer) PublishSyncComplete(ctx context.Context, accountID uint, payload *protocol.SyncCompletePayload) error {
	event, err := protocol.NewEvent(protocol.EvtSyncComplete, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishGroupsSync 發送群組同步事件
func (p *StreamProducer) PublishGroupsSync(ctx context.Context, accountID uint, payload *protocol.GroupsSyncPayload) error {
	event, err := protocol.NewEvent(protocol.EvtGroupsSync, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishCommandAck 發送命令確認事件
func (p *StreamProducer) PublishCommandAck(ctx context.Context, accountID uint, commandID string) error {
	payload := &protocol.CommandAckPayload{CommandID: commandID}
	event, err := protocol.NewEvent(protocol.EvtCommandAck, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishCommandError 發送命令錯誤事件
func (p *StreamProducer) PublishCommandError(ctx context.Context, accountID uint, commandID string, errMsg string, code string) error {
	payload := &protocol.CommandErrorPayload{
		CommandID: commandID,
		Error:     errMsg,
		Code:      code,
	}
	event, err := protocol.NewEvent(protocol.EvtCommandError, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishHeartbeat 發送心跳事件
func (p *StreamProducer) PublishHeartbeat(ctx context.Context, payload *protocol.HeartbeatPayload) error {
	event, err := protocol.NewEvent(protocol.EvtHeartbeat, p.connectorID, 0, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishProfileUpdated 發送帳號資料已更新事件
func (p *StreamProducer) PublishProfileUpdated(ctx context.Context, accountID uint, payload *protocol.ProfileUpdatedPayload) error {
	event, err := protocol.NewEvent(protocol.EvtProfileUpdated, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishChatsUpdated 發送 Chat 列表更新事件（包含名稱和頭像）
func (p *StreamProducer) PublishChatsUpdated(ctx context.Context, accountID uint, payload *protocol.ChatsUpdatedPayload) error {
	event, err := protocol.NewEvent(protocol.EvtChatsUpdated, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishMessageRevoked 發送訊息被撤回事件
func (p *StreamProducer) PublishMessageRevoked(ctx context.Context, accountID uint, payload *protocol.MessageRevokedPayload) error {
	event, err := protocol.NewEvent(protocol.EvtMessageRevoked, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishMessageEdited 發送訊息被編輯事件
func (p *StreamProducer) PublishMessageEdited(ctx context.Context, accountID uint, payload *protocol.MessageEditedPayload) error {
	event, err := protocol.NewEvent(protocol.EvtMessageEdited, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishMessageDeletedForMe 發送訊息被刪除（僅自己）事件
func (p *StreamProducer) PublishMessageDeletedForMe(ctx context.Context, accountID uint, payload *protocol.MessageDeletedForMePayload) error {
	event, err := protocol.NewEvent(protocol.EvtMessageDeletedForMe, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishChatArchiveChanged 發送聊天歸檔狀態變更事件
func (p *StreamProducer) PublishChatArchiveChanged(ctx context.Context, accountID uint, payload *protocol.ChatArchiveChangedPayload) error {
	event, err := protocol.NewEvent(protocol.EvtChatArchiveChanged, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishChatArchiveBatch 發送批次聊天歸檔狀態同步事件
func (p *StreamProducer) PublishChatArchiveBatch(ctx context.Context, accountID uint, payload *protocol.ChatArchiveBatchPayload) error {
	event, err := protocol.NewEvent(protocol.EvtChatArchiveBatch, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishManageCommandAck 發送管理命令確認事件
func (p *StreamProducer) PublishManageCommandAck(ctx context.Context, commandID string) error {
	payload := &protocol.CommandAckPayload{CommandID: commandID}
	event, err := protocol.NewEvent(protocol.EvtManageCommandAck, p.connectorID, 0, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishManageCommandError 發送管理命令錯誤事件
func (p *StreamProducer) PublishManageCommandError(ctx context.Context, commandID string, errMsg string) error {
	payload := &protocol.CommandErrorPayload{CommandID: commandID, Error: errMsg}
	event, err := protocol.NewEvent(protocol.EvtManageCommandError, p.connectorID, 0, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}

// PublishMediaDownloaded 發送媒體下載完成事件
func (p *StreamProducer) PublishMediaDownloaded(ctx context.Context, accountID uint, payload *protocol.MediaDownloadedPayload) error {
	event, err := protocol.NewEvent(protocol.EvtMediaDownloaded, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}
