package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// CommandHandler 命令處理器介面
type CommandHandler interface {
	// 命令處理方法
	HandleSendMessage(ctx context.Context, cmd *protocol.Command, payload *protocol.SendMessagePayload) error
	HandleSendMedia(ctx context.Context, cmd *protocol.Command, payload *protocol.SendMediaPayload) error
	HandleConnect(ctx context.Context, cmd *protocol.Command) error
	HandleDisconnect(ctx context.Context, cmd *protocol.Command) error
	HandleSyncChats(ctx context.Context, cmd *protocol.Command) error
	HandleSyncHistory(ctx context.Context, cmd *protocol.Command, payload *protocol.SyncHistoryPayload) error
	HandleSyncContacts(ctx context.Context, cmd *protocol.Command) error
	HandleGetQRCode(ctx context.Context, cmd *protocol.Command, payload *protocol.GetQRCodePayload) error
	HandleGetPairingCode(ctx context.Context, cmd *protocol.Command, payload *protocol.GetPairingCodePayload) error
	HandleCancelLogin(ctx context.Context, cmd *protocol.Command, payload *protocol.CancelLoginPayload) error
	HandleRevokeMessage(ctx context.Context, cmd *protocol.Command, payload *protocol.RevokeMessagePayload) error
	HandleUpdateProfile(ctx context.Context, cmd *protocol.Command) error
	HandleUpdateSettings(ctx context.Context, cmd *protocol.Command, payload *protocol.UpdateSettingsPayload) error
	HandleBindAccount(ctx context.Context, cmd *protocol.Command, payload *protocol.BindAccountPayload) error
	HandleArchiveChat(ctx context.Context, cmd *protocol.Command, payload *protocol.ArchiveChatPayload) error
	HandleDeleteMessageForMe(ctx context.Context, cmd *protocol.Command, payload *protocol.DeleteMessageForMePayload) error

	// 帳號管理方法
	GetAccountIDs() []uint
	GetAccountCount() int
	IsAccountManaged(accountID uint) bool
	Shutdown(ctx context.Context)
}

// StreamConsumer 命令消費者，負責接收主服務的命令
type StreamConsumer struct {
	client       *redis.Client
	connectorID  string
	handler      CommandHandler
	producer     *StreamProducer
	consumerName string
	stopCh       chan struct{}
	wg           sync.WaitGroup
	log          *zap.SugaredLogger // 結構化 logger

	// 雙 stream 名稱
	priorityStream string
	bulkStream     string
	// 舊 stream（用於遷移）
	legacyStream string
}

// NewStreamConsumer 建立命令消費者
func NewStreamConsumer(client *redis.Client, connectorID string, handler CommandHandler, producer *StreamProducer, log *zap.SugaredLogger) *StreamConsumer {
	return &StreamConsumer{
		client:         client,
		connectorID:    connectorID,
		handler:        handler,
		producer:       producer,
		consumerName:   fmt.Sprintf("connector-%s", connectorID),
		stopCh:         make(chan struct{}),
		log:            log,
		priorityStream: protocol.GetPriorityCommandStreamName(connectorID),
		bulkStream:     protocol.GetBulkCommandStreamName(connectorID),
		legacyStream:   protocol.GetCommandStreamName(connectorID),
	}
}

// Start 啟動消費者
func (c *StreamConsumer) Start(ctx context.Context) error {
	// 確保消費者群組存在
	c.ensureConsumerGroup(ctx, c.priorityStream, protocol.PriorityCommandConsumerGroup)
	c.ensureConsumerGroup(ctx, c.bulkStream, protocol.BulkCommandConsumerGroup)

	// 遷移舊 stream 中的殘留訊息
	c.drainLegacyStream(ctx)

	c.wg.Add(2)
	go func() {
		defer c.wg.Done()
		c.consumeLoop(ctx, c.priorityStream, protocol.PriorityCommandConsumerGroup, "priority")
	}()
	go func() {
		defer c.wg.Done()
		c.consumeLoop(ctx, c.bulkStream, protocol.BulkCommandConsumerGroup, "bulk")
	}()

	c.log.Infow("命令消費者已啟動", "priority_stream", c.priorityStream, "bulk_stream", c.bulkStream)
	return nil
}

// Stop 停止消費者
func (c *StreamConsumer) Stop() {
	close(c.stopCh)
	c.wg.Wait()
	c.log.Infow("命令消費者已停止")
}

// ensureConsumerGroup 確保消費者群組存在
func (c *StreamConsumer) ensureConsumerGroup(ctx context.Context, stream, group string) {
	err := c.client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		c.log.Warnw("建立消費者群組失敗", "stream", stream, "group", group, "error", err)
	}
}

// drainLegacyStream 清除舊 stream 中的殘留訊息（直接丟棄，不再執行）
func (c *StreamConsumer) drainLegacyStream(ctx context.Context) {
	n, err := c.client.XLen(ctx, c.legacyStream).Result()
	if err != nil || n == 0 {
		return
	}

	c.log.Infow("舊 stream 有殘留訊息，直接清除", "stream", c.legacyStream, "count", n)
	if err := c.client.Del(ctx, c.legacyStream).Err(); err != nil {
		c.log.Warnw("清除舊 stream 失敗", "error", err)
	}
}

// consumeLoop 消費迴圈
func (c *StreamConsumer) consumeLoop(ctx context.Context, stream, group, label string) {
	// 啟動時先 ack 所有殘留的 pending 訊息（上次未正常 ack 的孤兒）
	c.ackStalePending(ctx, stream, group, label)

	for {
		select {
		case <-ctx.Done():
			c.log.Infow("命令消費者收到 context 取消信號", "label", label)
			return
		case <-c.stopCh:
			c.log.Infow("命令消費者收到停止信號", "label", label)
			return
		default:
			c.tryConsume(ctx, stream, group)
		}
	}
}

// ackStalePending 清除殘留的 pending 訊息（直接 ack，不重新執行）
func (c *StreamConsumer) ackStalePending(ctx context.Context, stream, group, label string) {
	pending, err := c.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: stream,
		Group:  group,
		Start:  "-",
		End:    "+",
		Count:  1000,
	}).Result()
	if err != nil || len(pending) == 0 {
		return
	}

	ids := make([]string, len(pending))
	for i, p := range pending {
		ids[i] = p.ID
	}

	if err := c.client.XAck(ctx, stream, group, ids...).Err(); err != nil {
		c.log.Warnw("清除 pending 訊息失敗", "label", label, "error", err)
		return
	}
	c.log.Infow("已清除殘留 pending 訊息", "label", label, "count", len(ids))
}

// tryConsume 嘗試消費一個命令
func (c *StreamConsumer) tryConsume(ctx context.Context, stream, group string) {
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: c.consumerName,
		Streams:  []string{stream, ">"},
		Count:    1,
		Block:    500 * time.Millisecond,
	}).Result()

	if err != nil {
		if err == redis.Nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		c.log.Errorw("讀取命令失敗", "stream", stream, "error", err)
		time.Sleep(100 * time.Millisecond)
		return
	}

	for _, s := range streams {
		for _, msg := range s.Messages {
			c.processMessage(ctx, msg, stream, group)
		}
	}
}

// processMessage 處理訊息
func (c *StreamConsumer) processMessage(ctx context.Context, msg redis.XMessage, stream, group string) {
	cmdData, ok := msg.Values["command"].(string)
	if !ok {
		c.log.Errorw("無效的命令資料格式")
		c.ackMessage(ctx, msg.ID, stream, group)
		return
	}

	var cmd protocol.Command
	if err := json.Unmarshal([]byte(cmdData), &cmd); err != nil {
		c.log.Errorw("解析命令失敗", "error", err)
		c.ackMessage(ctx, msg.ID, stream, group)
		return
	}

	c.log.Infow("處理命令", "type", cmd.Type, "account_id", cmd.AccountID, "cmd_id", cmd.ID)

	// 依命令類型設定不同 timeout，避免單一命令卡住阻塞整個消費迴圈
	// 注意：不覆蓋原始 ctx，ack 和 publish 需要用未 timeout 的 ctx
	cmdCtx, cmdCancel := context.WithTimeout(ctx, commandTimeout(cmd.Type))
	defer cmdCancel()

	// 處理命令
	var err error
	switch cmd.Type {
	case protocol.CmdSendMessage:
		var payload protocol.SendMessagePayload
		if parseErr := cmd.ParsePayload(&payload); parseErr != nil {
			err = parseErr
		} else {
			err = c.handler.HandleSendMessage(cmdCtx, &cmd, &payload)
		}

	case protocol.CmdSendMedia:
		var payload protocol.SendMediaPayload
		if parseErr := cmd.ParsePayload(&payload); parseErr != nil {
			err = parseErr
		} else {
			err = c.handler.HandleSendMedia(cmdCtx, &cmd, &payload)
		}

	case protocol.CmdConnect:
		err = c.handler.HandleConnect(cmdCtx, &cmd)

	case protocol.CmdDisconnect:
		err = c.handler.HandleDisconnect(cmdCtx, &cmd)

	case protocol.CmdSyncChats:
		err = c.handler.HandleSyncChats(cmdCtx, &cmd)

	case protocol.CmdSyncHistory:
		var payload protocol.SyncHistoryPayload
		if parseErr := cmd.ParsePayload(&payload); parseErr != nil {
			err = parseErr
		} else {
			err = c.handler.HandleSyncHistory(cmdCtx, &cmd, &payload)
		}

	case protocol.CmdSyncContacts:
		err = c.handler.HandleSyncContacts(cmdCtx, &cmd)

	case protocol.CmdGetQRCode:
		var payload protocol.GetQRCodePayload
		if parseErr := cmd.ParsePayload(&payload); parseErr != nil {
			err = parseErr
		} else {
			err = c.handler.HandleGetQRCode(cmdCtx, &cmd, &payload)
		}

	case protocol.CmdGetPairingCode:
		var payload protocol.GetPairingCodePayload
		if parseErr := cmd.ParsePayload(&payload); parseErr != nil {
			err = parseErr
		} else {
			err = c.handler.HandleGetPairingCode(cmdCtx, &cmd, &payload)
		}

	case protocol.CmdCancelLogin:
		var payload protocol.CancelLoginPayload
		if parseErr := cmd.ParsePayload(&payload); parseErr != nil {
			err = parseErr
		} else {
			err = c.handler.HandleCancelLogin(cmdCtx, &cmd, &payload)
		}

	case protocol.CmdRevokeMessage:
		var payload protocol.RevokeMessagePayload
		if parseErr := cmd.ParsePayload(&payload); parseErr != nil {
			err = parseErr
		} else {
			err = c.handler.HandleRevokeMessage(cmdCtx, &cmd, &payload)
		}

	case protocol.CmdUpdateProfile:
		err = c.handler.HandleUpdateProfile(cmdCtx, &cmd)

	case protocol.CmdBindAccount:
		var payload protocol.BindAccountPayload
		if parseErr := cmd.ParsePayload(&payload); parseErr != nil {
			err = parseErr
		} else {
			err = c.handler.HandleBindAccount(cmdCtx, &cmd, &payload)
		}

	case protocol.CmdUpdateSettings:
		var payload protocol.UpdateSettingsPayload
		if parseErr := cmd.ParsePayload(&payload); parseErr != nil {
			err = parseErr
		} else {
			err = c.handler.HandleUpdateSettings(cmdCtx, &cmd, &payload)
		}

	case protocol.CmdArchiveChat:
		var payload protocol.ArchiveChatPayload
		if parseErr := cmd.ParsePayload(&payload); parseErr != nil {
			err = parseErr
		} else {
			err = c.handler.HandleArchiveChat(cmdCtx, &cmd, &payload)
		}

	case protocol.CmdDeleteMessageForMe:
		var payload protocol.DeleteMessageForMePayload
		if parseErr := cmd.ParsePayload(&payload); parseErr != nil {
			err = parseErr
		} else {
			err = c.handler.HandleDeleteMessageForMe(cmdCtx, &cmd, &payload)
		}

	default:
		err = fmt.Errorf("未知的命令類型: %s", cmd.Type)
	}

	// 發送 ack 或 error 事件
	if err != nil {
		c.log.Errorw("命令執行失敗", "type", cmd.Type, "account_id", cmd.AccountID, "error", err)
		if pubErr := c.producer.PublishCommandError(ctx, cmd.AccountID, cmd.ID, err.Error(), ""); pubErr != nil {
			c.log.Errorw("發送命令錯誤事件失敗", "error", pubErr)
		}
	} else {
		c.log.Infow("命令執行成功", "type", cmd.Type, "account_id", cmd.AccountID)
		if pubErr := c.producer.PublishCommandAck(ctx, cmd.AccountID, cmd.ID); pubErr != nil {
			c.log.Errorw("發送命令確認事件失敗", "error", pubErr)
		}
	}

	// 確認訊息已處理
	c.ackMessage(ctx, msg.ID, stream, group)
}

// ackMessage 確認訊息已處理
func (c *StreamConsumer) ackMessage(ctx context.Context, msgID, stream, group string) {
	if err := c.client.XAck(ctx, stream, group, msgID).Err(); err != nil {
		c.log.Errorw("確認命令訊息失敗", "error", err)
	}
}

// commandTimeout 依命令類型回傳適當的 timeout
func commandTimeout(cmdType protocol.CommandType) time.Duration {
	switch cmdType {
	case protocol.CmdSyncChats, protocol.CmdSyncContacts, protocol.CmdConnect:
		return 120 * time.Second
	case protocol.CmdUpdateProfile, protocol.CmdUpdateSettings,
		protocol.CmdArchiveChat, protocol.CmdSendMedia:
		return 60 * time.Second
	default:
		return 30 * time.Second
	}
}
