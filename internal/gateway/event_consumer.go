package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
)

// EventHandler 事件處理器介面
type EventHandler interface {
	OnMessageReceived(ctx context.Context, event *protocol.Event, payload *protocol.MessageReceivedPayload) error
	OnMessageSent(ctx context.Context, event *protocol.Event, payload *protocol.MessageSentPayload) error
	OnReceipt(ctx context.Context, event *protocol.Event, payload *protocol.ReceiptPayload) error
	OnConnected(ctx context.Context, event *protocol.Event, payload *protocol.ConnectedPayload) error
	OnDisconnected(ctx context.Context, event *protocol.Event, payload *protocol.DisconnectedPayload) error
	OnLoggedOut(ctx context.Context, event *protocol.Event, payload *protocol.LoggedOutPayload) error
	OnQRCode(ctx context.Context, event *protocol.Event, payload *protocol.QRCodePayload) error
	OnPairingCode(ctx context.Context, event *protocol.Event, payload *protocol.PairingCodePayload) error
	OnLoginSuccess(ctx context.Context, event *protocol.Event, payload *protocol.LoginSuccessPayload) error
	OnLoginFailed(ctx context.Context, event *protocol.Event, payload *protocol.LoginFailedPayload) error
	OnLoginCancelled(ctx context.Context, event *protocol.Event, payload *protocol.LoginCancelledPayload) error
	OnSyncProgress(ctx context.Context, event *protocol.Event, payload *protocol.SyncProgressPayload) error
	OnSyncComplete(ctx context.Context, event *protocol.Event, payload *protocol.SyncCompletePayload) error
	OnProfileUpdated(ctx context.Context, event *protocol.Event, payload *protocol.ProfileUpdatedPayload) error
	OnGroupsSync(ctx context.Context, event *protocol.Event, payload *protocol.GroupsSyncPayload) error
	OnChatsUpdated(ctx context.Context, event *protocol.Event, payload *protocol.ChatsUpdatedPayload) error
	OnMessageRevoked(ctx context.Context, event *protocol.Event, payload *protocol.MessageRevokedPayload) error
	OnMessageEdited(ctx context.Context, event *protocol.Event, payload *protocol.MessageEditedPayload) error
	OnMessageDeletedForMe(ctx context.Context, event *protocol.Event, payload *protocol.MessageDeletedForMePayload) error
	OnChatArchiveChanged(ctx context.Context, event *protocol.Event, payload *protocol.ChatArchiveChangedPayload) error
	OnChatArchiveBatch(ctx context.Context, event *protocol.Event, payload *protocol.ChatArchiveBatchPayload) error
	OnMediaDownloaded(ctx context.Context, event *protocol.Event, payload *protocol.MediaDownloadedPayload) error
}

// EventConsumer 事件消費者
type EventConsumer struct {
	redis        *redis.Client
	gateway      *ConnectorGateway
	handler      EventHandler
	consumerName string
	stopCh       chan struct{}
	wg           sync.WaitGroup

	// Connector 健康追蹤
	connectorHealth map[string]time.Time
	healthMu        sync.RWMutex

	// 健康檢查配置
	heartbeatTimeout time.Duration

	// Worker pool（一般事件）
	workerCount int
	workCh      chan redis.XMessage

	// Worker pool（訊息事件，獨立處理）
	messageWorkerCount int
	messageWorkCh      chan redis.XMessage
}

// EventConsumerConfig 事件消費者配置
type EventConsumerConfig struct {
	ConsumerName       string
	HeartbeatTimeout   time.Duration
	WorkerCount        int // 一般事件 worker 數量
	MessageWorkerCount int // 訊息事件 worker 數量
}

// DefaultEventConsumerConfig 預設配置
func DefaultEventConsumerConfig() *EventConsumerConfig {
	return &EventConsumerConfig{
		ConsumerName:       "api-consumer-1",
		HeartbeatTimeout:   90 * time.Second,
		WorkerCount:        30,  // 10 → 30
		MessageWorkerCount: 120, // 40 → 120
	}
}

// isTransientDBError 判斷是否為暫時性 DB 連線錯誤（值得重試）
func isTransientDBError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "too many clients already") ||
		strings.Contains(msg, "SQLSTATE 53300") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "failed to connect")
}

// retryOnTransient 對暫時性 DB 錯誤進行重試（exponential backoff: 200ms → 1s → 3s）
func (c *EventConsumer) retryOnTransient(ctx context.Context, fn func() error) error {
	retryDelays := [3]time.Duration{200 * time.Millisecond, 1 * time.Second, 3 * time.Second}

	err := fn()
	for i := 0; err != nil && isTransientDBError(err) && i < len(retryDelays); i++ {
		logger.Ctx(ctx).Warnw("暫時性 DB 錯誤，準備重試",
			"attempt", i+1,
			"delay", retryDelays[i],
			"error", err,
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stopCh:
			return err
		case <-time.After(retryDelays[i]):
		}
		err = fn()
	}
	return err
}

// NewEventConsumer 建立事件消費者
func NewEventConsumer(redisClient *redis.Client, gateway *ConnectorGateway, handler EventHandler, cfg *EventConsumerConfig) *EventConsumer {
	if cfg == nil {
		cfg = DefaultEventConsumerConfig()
	}

	workerCount := cfg.WorkerCount
	if workerCount <= 0 {
		workerCount = 5
	}

	messageWorkerCount := cfg.MessageWorkerCount
	if messageWorkerCount <= 0 {
		messageWorkerCount = 20
	}

	return &EventConsumer{
		redis:              redisClient,
		gateway:            gateway,
		handler:            handler,
		consumerName:       cfg.ConsumerName,
		stopCh:             make(chan struct{}),
		connectorHealth:    make(map[string]time.Time),
		heartbeatTimeout:   cfg.HeartbeatTimeout,
		workerCount:        workerCount,
		workCh:             make(chan redis.XMessage, workerCount*10),
		messageWorkerCount: messageWorkerCount,
		messageWorkCh:      make(chan redis.XMessage, messageWorkerCount*10),
	}
}

// Start 啟動消費者
func (c *EventConsumer) Start(ctx context.Context) error {
	// 確保消費者群組存在
	if err := c.ensureConsumerGroup(ctx); err != nil {
		logger.Warnw("建立事件消費者群組失敗（可能已存在）", "error", err)
	}

	// 0. 啟動前先處理上次未 ACK 的 pending 訊息（避免重啟後遺失事件）
	c.recoverPendingMessages(ctx)

	// 1. 啟動高優先級事件消費（登入、連線狀態）
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.consumePriorityLoop(ctx)
	}()
	logger.Info("高優先級事件消費者已啟動")

	// 2. 啟動訊息事件 worker pool
	for i := 0; i < c.messageWorkerCount; i++ {
		c.wg.Add(1)
		go func(workerID int) {
			defer c.wg.Done()
			c.messageWorker(ctx, workerID)
		}(i)
	}
	logger.Infow("訊息事件 worker pool 已啟動", "worker_count", c.messageWorkerCount)

	// 啟動訊息事件消費（生產者）
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.consumeMessageLoop(ctx)
	}()

	// 3. 啟動一般事件 worker pool
	for i := 0; i < c.workerCount; i++ {
		c.wg.Add(1)
		go func(workerID int) {
			defer c.wg.Done()
			c.worker(ctx, workerID)
		}(i)
	}
	logger.Infow("一般事件 worker pool 已啟動", "worker_count", c.workerCount)

	// 啟動一般事件消費（生產者）
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.consumeLoop(ctx)
	}()

	// 4. 啟動健康檢查
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.healthCheckLoop(ctx)
	}()

	logger.Info("事件消費者已全部啟動")
	return nil
}

// worker 處理一般事件的 worker
func (c *EventConsumer) worker(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case msg, ok := <-c.workCh:
			if !ok {
				return
			}
			c.processMessage(ctx, msg)
		}
	}
}

// messageWorker 處理訊息事件的 worker
func (c *EventConsumer) messageWorker(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case msg, ok := <-c.messageWorkCh:
			if !ok {
				return
			}
			c.processMessageEvent(ctx, msg)
		}
	}
}

// Stop 停止消費者
func (c *EventConsumer) Stop() {
	close(c.stopCh)
	c.wg.Wait()
	logger.Info("事件消費者已停止")
}

// ensureConsumerGroup 確保消費者群組存在
func (c *EventConsumer) ensureConsumerGroup(ctx context.Context) error {
	// 高優先級事件 stream（登入、連線狀態）
	err := c.redis.XGroupCreateMkStream(ctx, protocol.PriorityEventStreamName, protocol.PriorityEventConsumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}

	// 訊息事件 stream
	err = c.redis.XGroupCreateMkStream(ctx, protocol.MessageEventStreamName, protocol.MessageEventConsumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}

	// 一般事件 stream
	err = c.redis.XGroupCreateMkStream(ctx, protocol.EventStreamName, protocol.EventConsumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}

	return nil
}

// recoverPendingMessages 啟動時重新處理所有未 ACK 的 pending 訊息
// XReadGroup 用 "0" 代替 ">" 會回傳該 consumer 已 claim 但未 ACK 的訊息
func (c *EventConsumer) recoverPendingMessages(ctx context.Context) {
	type streamRecovery struct {
		stream  string
		group   string
		process func(context.Context, redis.XMessage)
	}

	streams := []streamRecovery{
		{protocol.PriorityEventStreamName, protocol.PriorityEventConsumerGroup, c.processPriorityMessage},
		{protocol.MessageEventStreamName, protocol.MessageEventConsumerGroup, c.processMessageEvent},
		{protocol.EventStreamName, protocol.EventConsumerGroup, c.processMessage},
	}

	for _, s := range streams {
		recovered := 0
		prevFirstID := ""
		for {
			result, err := c.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    s.group,
				Consumer: c.consumerName,
				Streams:  []string{s.stream, "0"},
				Count:    100,
			}).Result()
			if err != nil {
				if err != redis.Nil {
					logger.Warnw("讀取 pending 訊息失敗", "stream", s.stream, "error", err)
				}
				break
			}

			hasMessages := false
			for _, stream := range result {
				if len(stream.Messages) == 0 {
					continue
				}
				// 如果第一筆 ID 跟上一輪相同，代表該批訊息都無法成功處理，跳出避免無限迴圈
				if stream.Messages[0].ID == prevFirstID {
					logger.Warnw("pending 訊息仍無法處理，跳過剩餘", "stream", s.stream)
					hasMessages = false
					break
				}
				prevFirstID = stream.Messages[0].ID
				for _, msg := range stream.Messages {
					hasMessages = true
					s.process(ctx, msg)
					recovered++
				}
			}
			if !hasMessages {
				break
			}
		}
		if recovered > 0 {
			logger.Infow("已重新處理 pending 訊息", "stream", s.stream, "count", recovered)
		}
	}
}

// consumeLoop 消費迴圈
func (c *EventConsumer) consumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
			c.tryConsume(ctx)
		}
	}
}

// consumeMessageLoop 訊息事件消費迴圈
func (c *EventConsumer) consumeMessageLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
			c.tryConsumeMessage(ctx)
		}
	}
}

// tryConsumeMessage 嘗試消費訊息事件
func (c *EventConsumer) tryConsumeMessage(ctx context.Context) {
	streams, err := c.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    protocol.MessageEventConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{protocol.MessageEventStreamName, ">"},
		Count:    100, // 批量讀取
		Block:    500 * time.Millisecond,
	}).Result()

	if err != nil {
		if err == redis.Nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		logger.Errorw("讀取訊息事件失敗", "error", err)
		time.Sleep(100 * time.Millisecond)
		return
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case c.messageWorkCh <- msg:
				// 訊息已發送到 worker
			}
		}
	}
}

// processMessageEvent 處理訊息事件
func (c *EventConsumer) processMessageEvent(ctx context.Context, msg redis.XMessage) {
	eventData, ok := msg.Values["event"].(string)
	if !ok {
		logger.Error("無效的訊息事件資料格式")
		c.ackMessageEvent(ctx, msg.ID)
		return
	}

	var event protocol.Event
	if err := json.Unmarshal([]byte(eventData), &event); err != nil {
		logger.Errorw("解析訊息事件失敗", "error", err)
		c.ackMessageEvent(ctx, msg.ID)
		return
	}

	// 注入 connector_id + account_id 到 context
	ctx = logger.WithEventCtx(ctx, event.ConnectorID, event.AccountID)

	// 更新 Connector 健康狀態
	c.updateConnectorHealth(event.ConnectorID)

	if event.Type != protocol.EvtMessageReceived {
		logger.Ctx(ctx).Warnw("訊息事件 stream 收到非訊息事件", "event_type", event.Type)
		c.ackMessageEvent(ctx, msg.ID)
		return
	}

	var payload protocol.MessageReceivedPayload
	if err := event.ParsePayload(&payload); err != nil {
		logger.Ctx(ctx).Errorw("解析 MessageReceived payload 失敗", "error", err)
		c.ackMessageEvent(ctx, msg.ID)
		return
	}

	err := c.retryOnTransient(ctx, func() error {
		return c.handler.OnMessageReceived(ctx, &event, &payload)
	})
	if err != nil {
		logger.Ctx(ctx).Errorw("處理訊息事件失敗", "error", err)
		// transient error 重試耗盡 → 不 ACK，留在 pending list
		if isTransientDBError(err) {
			return
		}
	}

	c.ackMessageEvent(ctx, msg.ID)
}

// ackMessageEvent 確認訊息事件已處理
func (c *EventConsumer) ackMessageEvent(ctx context.Context, msgID string) {
	if err := c.redis.XAck(ctx, protocol.MessageEventStreamName, protocol.MessageEventConsumerGroup, msgID).Err(); err != nil {
		logger.Errorw("確認訊息事件失敗", "error", err)
	}
}

// consumePriorityLoop 高優先級事件消費迴圈（登入、連線狀態）
func (c *EventConsumer) consumePriorityLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
			c.tryConsumePriority(ctx)
		}
	}
}

// tryConsumePriority 嘗試消費高優先級事件
func (c *EventConsumer) tryConsumePriority(ctx context.Context) {
	streams, err := c.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    protocol.PriorityEventConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{protocol.PriorityEventStreamName, ">"},
		Count:    10,
		Block:    500 * time.Millisecond,
	}).Result()

	if err != nil {
		if err == redis.Nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		logger.Errorw("讀取登入事件失敗", "error", err)
		time.Sleep(100 * time.Millisecond)
		return
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			c.processPriorityMessage(ctx, msg)
		}
	}
}

// handlePriorityEvent 分派高優先級事件到對應的 handler
func (c *EventConsumer) handlePriorityEvent(ctx context.Context, event *protocol.Event) error {
	switch event.Type {
	case protocol.EvtQRCode:
		var payload protocol.QRCodePayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		_ = c.gateway.UpdateLoginSessionQRCode(ctx, payload.SessionID, payload.QRCode)
		return c.handler.OnQRCode(ctx, event, &payload)

	case protocol.EvtPairingCode:
		var payload protocol.PairingCodePayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		logger.Ctx(ctx).Infow("收到 PairingCode 事件", "session_id", payload.SessionID, "pairing_code", payload.PairingCode)
		if updateErr := c.gateway.UpdateLoginSessionPairingCode(ctx, payload.SessionID, payload.PairingCode); updateErr != nil {
			logger.Ctx(ctx).Errorw("更新 PairingCode 失敗", "error", updateErr)
		} else {
			logger.Ctx(ctx).Infow("更新 PairingCode 成功", "session_id", payload.SessionID)
		}
		return c.handler.OnPairingCode(ctx, event, &payload)

	case protocol.EvtLoginSuccess:
		var payload protocol.LoginSuccessPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		_ = c.gateway.UpdateLoginSessionSuccess(ctx, payload.SessionID, payload.JID, payload.PhoneNumber)
		return c.handler.OnLoginSuccess(ctx, event, &payload)

	case protocol.EvtLoginFailed:
		var payload protocol.LoginFailedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		_ = c.gateway.UpdateLoginSessionFailed(ctx, payload.SessionID, payload.Reason)
		return c.handler.OnLoginFailed(ctx, event, &payload)

	case protocol.EvtLoginCancelled:
		var payload protocol.LoginCancelledPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		_ = c.gateway.UpdateLoginSessionCancelled(ctx, payload.SessionID)
		return c.handler.OnLoginCancelled(ctx, event, &payload)

	case protocol.EvtConnected:
		var payload protocol.ConnectedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		logger.Ctx(ctx).Infow("帳號已連線")
		return c.handler.OnConnected(ctx, event, &payload)

	case protocol.EvtDisconnected:
		var payload protocol.DisconnectedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		logger.Ctx(ctx).Infow("帳號已斷線", "reason", payload.Reason)
		return c.handler.OnDisconnected(ctx, event, &payload)

	case protocol.EvtLoggedOut:
		var payload protocol.LoggedOutPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		logger.Ctx(ctx).Warnw("帳號已登出", "reason", payload.Reason)
		return c.handler.OnLoggedOut(ctx, event, &payload)

	case protocol.EvtCommandAck:
		var payload protocol.CommandAckPayload
		if err := event.ParsePayload(&payload); err == nil {
			c.gateway.NotifyCommandSuccess(payload.CommandID)
		}

	case protocol.EvtCommandError:
		var payload protocol.CommandErrorPayload
		if err := event.ParsePayload(&payload); err == nil {
			logger.Ctx(ctx).Warnw("命令執行失敗", "command_id", payload.CommandID, "error", payload.Error)
			c.gateway.NotifyCommandError(payload.CommandID, payload.Error)
		}

	case protocol.EvtManageCommandAck:
		var payload protocol.CommandAckPayload
		if err := event.ParsePayload(&payload); err == nil {
			c.gateway.NotifyCommandSuccess(payload.CommandID)
		}

	case protocol.EvtManageCommandError:
		var payload protocol.CommandErrorPayload
		if err := event.ParsePayload(&payload); err == nil {
			logger.Ctx(ctx).Warnw("管理命令執行失敗", "command_id", payload.CommandID, "error", payload.Error)
			c.gateway.NotifyCommandError(payload.CommandID, payload.Error)
		}

	default:
		logger.Ctx(ctx).Warnw("高優先級事件 stream 收到未知事件", "event_type", event.Type)
	}
	return nil
}

// processPriorityMessage 處理高優先級事件訊息（登入、連線狀態）
func (c *EventConsumer) processPriorityMessage(ctx context.Context, msg redis.XMessage) {
	eventData, ok := msg.Values["event"].(string)
	if !ok {
		logger.Error("無效的登入事件資料格式")
		c.ackPriorityMessage(ctx, msg.ID)
		return
	}

	var event protocol.Event
	if err := json.Unmarshal([]byte(eventData), &event); err != nil {
		logger.Errorw("解析登入事件失敗", "error", err)
		c.ackPriorityMessage(ctx, msg.ID)
		return
	}

	ctx = logger.WithEventCtx(ctx, event.ConnectorID, event.AccountID)
	c.updateConnectorHealth(event.ConnectorID)

	err := c.retryOnTransient(ctx, func() error {
		return c.handlePriorityEvent(ctx, &event)
	})
	if err != nil {
		logger.Ctx(ctx).Errorw("處理登入事件失敗", "event_type", event.Type, "error", err)
		if isTransientDBError(err) {
			return
		}
	}

	c.ackPriorityMessage(ctx, msg.ID)
}

// ackPriorityMessage 確認登入事件訊息已處理
func (c *EventConsumer) ackPriorityMessage(ctx context.Context, msgID string) {
	if err := c.redis.XAck(ctx, protocol.PriorityEventStreamName, protocol.PriorityEventConsumerGroup, msgID).Err(); err != nil {
		logger.Errorw("確認高優先級事件訊息失敗", "error", err)
	}
}

// tryConsume 嘗試消費事件（生產者，把訊息發到 workCh）
func (c *EventConsumer) tryConsume(ctx context.Context) {
	streams, err := c.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    protocol.EventConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{protocol.EventStreamName, ">"},
		Count:    100, // 一次讀取更多訊息
		Block:    500 * time.Millisecond,
	}).Result()

	if err != nil {
		if err == redis.Nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		logger.Errorw("讀取事件失敗", "error", err)
		time.Sleep(100 * time.Millisecond)
		return
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case c.workCh <- msg:
				// 訊息已發送到 worker
			}
		}
	}
}

// handleGeneralEvent 分派一般事件到對應的 handler
func (c *EventConsumer) handleGeneralEvent(ctx context.Context, event *protocol.Event) error {
	switch event.Type {
	case protocol.EvtMessageReceived:
		var payload protocol.MessageReceivedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnMessageReceived(ctx, event, &payload)

	case protocol.EvtMessageSent:
		var payload protocol.MessageSentPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnMessageSent(ctx, event, &payload)

	case protocol.EvtReceipt:
		var payload protocol.ReceiptPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnReceipt(ctx, event, &payload)

	case protocol.EvtConnected:
		var payload protocol.ConnectedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnConnected(ctx, event, &payload)

	case protocol.EvtDisconnected:
		var payload protocol.DisconnectedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnDisconnected(ctx, event, &payload)

	case protocol.EvtLoggedOut:
		var payload protocol.LoggedOutPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnLoggedOut(ctx, event, &payload)

	case protocol.EvtQRCode:
		var payload protocol.QRCodePayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		_ = c.gateway.UpdateLoginSessionQRCode(ctx, payload.SessionID, payload.QRCode)
		return c.handler.OnQRCode(ctx, event, &payload)

	case protocol.EvtPairingCode:
		var payload protocol.PairingCodePayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		logger.Ctx(ctx).Infow("收到 PairingCode 事件", "session_id", payload.SessionID, "pairing_code", payload.PairingCode)
		if updateErr := c.gateway.UpdateLoginSessionPairingCode(ctx, payload.SessionID, payload.PairingCode); updateErr != nil {
			logger.Ctx(ctx).Errorw("更新 PairingCode 失敗", "error", updateErr)
		} else {
			logger.Ctx(ctx).Infow("更新 PairingCode 成功", "session_id", payload.SessionID)
		}
		return c.handler.OnPairingCode(ctx, event, &payload)

	case protocol.EvtLoginSuccess:
		var payload protocol.LoginSuccessPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		_ = c.gateway.UpdateLoginSessionSuccess(ctx, payload.SessionID, payload.JID, payload.PhoneNumber)
		return c.handler.OnLoginSuccess(ctx, event, &payload)

	case protocol.EvtLoginFailed:
		var payload protocol.LoginFailedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		_ = c.gateway.UpdateLoginSessionFailed(ctx, payload.SessionID, payload.Reason)
		return c.handler.OnLoginFailed(ctx, event, &payload)

	case protocol.EvtLoginCancelled:
		var payload protocol.LoginCancelledPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		_ = c.gateway.UpdateLoginSessionCancelled(ctx, payload.SessionID)
		return c.handler.OnLoginCancelled(ctx, event, &payload)

	case protocol.EvtSyncProgress:
		var payload protocol.SyncProgressPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnSyncProgress(ctx, event, &payload)

	case protocol.EvtSyncComplete:
		var payload protocol.SyncCompletePayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnSyncComplete(ctx, event, &payload)

	case protocol.EvtCommandAck:
		var payload protocol.CommandAckPayload
		if err := event.ParsePayload(&payload); err == nil {
			c.gateway.NotifyCommandSuccess(payload.CommandID)
		}

	case protocol.EvtCommandError:
		var payload protocol.CommandErrorPayload
		if err := event.ParsePayload(&payload); err == nil {
			logger.Ctx(ctx).Warnw("命令執行失敗", "command_id", payload.CommandID, "error", payload.Error)
			c.gateway.NotifyCommandError(payload.CommandID, payload.Error)
		}

	case protocol.EvtManageCommandAck:
		var payload protocol.CommandAckPayload
		if err := event.ParsePayload(&payload); err == nil {
			c.gateway.NotifyCommandSuccess(payload.CommandID)
		}

	case protocol.EvtManageCommandError:
		var payload protocol.CommandErrorPayload
		if err := event.ParsePayload(&payload); err == nil {
			logger.Ctx(ctx).Warnw("管理命令執行失敗", "command_id", payload.CommandID, "error", payload.Error)
			c.gateway.NotifyCommandError(payload.CommandID, payload.Error)
		}

	case protocol.EvtHeartbeat:
		var payload protocol.HeartbeatPayload
		if err := event.ParsePayload(&payload); err == nil {
			c.saveConnectorInfo(ctx, event.ConnectorID, &payload)
		}
		logger.Ctx(ctx).Debugw("收到心跳")

	case protocol.EvtProfileUpdated:
		var payload protocol.ProfileUpdatedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnProfileUpdated(ctx, event, &payload)

	case protocol.EvtGroupsSync:
		var payload protocol.GroupsSyncPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnGroupsSync(ctx, event, &payload)

	case protocol.EvtChatsUpdated:
		var payload protocol.ChatsUpdatedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnChatsUpdated(ctx, event, &payload)

	case protocol.EvtMessageRevoked:
		var payload protocol.MessageRevokedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnMessageRevoked(ctx, event, &payload)

	case protocol.EvtMessageEdited:
		var payload protocol.MessageEditedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnMessageEdited(ctx, event, &payload)

	case protocol.EvtMessageDeletedForMe:
		var payload protocol.MessageDeletedForMePayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnMessageDeletedForMe(ctx, event, &payload)

	case protocol.EvtChatArchiveChanged:
		var payload protocol.ChatArchiveChangedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnChatArchiveChanged(ctx, event, &payload)

	case protocol.EvtChatArchiveBatch:
		var payload protocol.ChatArchiveBatchPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnChatArchiveBatch(ctx, event, &payload)

	case protocol.EvtMediaDownloaded:
		var payload protocol.MediaDownloadedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnMediaDownloaded(ctx, event, &payload)

	default:
		logger.Ctx(ctx).Warnw("未知的事件類型", "event_type", event.Type)
	}
	return nil
}

// processMessage 處理一般事件
func (c *EventConsumer) processMessage(ctx context.Context, msg redis.XMessage) {
	eventData, ok := msg.Values["event"].(string)
	if !ok {
		logger.Error("無效的事件資料格式")
		c.ackMessage(ctx, msg.ID)
		return
	}

	var event protocol.Event
	if err := json.Unmarshal([]byte(eventData), &event); err != nil {
		logger.Errorw("解析事件失敗", "error", err)
		c.ackMessage(ctx, msg.ID)
		return
	}

	ctx = logger.WithEventCtx(ctx, event.ConnectorID, event.AccountID)
	c.updateConnectorHealth(event.ConnectorID)

	err := c.retryOnTransient(ctx, func() error {
		return c.handleGeneralEvent(ctx, &event)
	})
	if err != nil {
		logger.Ctx(ctx).Errorw("事件處理失敗", "event_type", event.Type, "error", err)
		if isTransientDBError(err) {
			return
		}
	}

	c.ackMessage(ctx, msg.ID)
}

// ackMessage 確認訊息已處理
func (c *EventConsumer) ackMessage(ctx context.Context, msgID string) {
	if err := c.redis.XAck(ctx, protocol.EventStreamName, protocol.EventConsumerGroup, msgID).Err(); err != nil {
		logger.Errorw("確認事件訊息失敗", "error", err)
	}
}

// updateConnectorHealth 更新 Connector 健康狀態
func (c *EventConsumer) updateConnectorHealth(connectorID string) {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	c.connectorHealth[connectorID] = time.Now()
}

// saveConnectorInfo 保存 Connector 資訊（包含版本）
func (c *EventConsumer) saveConnectorInfo(ctx context.Context, connectorID string, payload *protocol.HeartbeatPayload) {
	infoKey := protocol.GetConnectorInfoKey(connectorID)

	fields := map[string]interface{}{
		"version":       payload.Version,
		"account_count": payload.AccountCount,
		"uptime":        payload.Uptime,
		"start_time":    payload.StartTime,
		"memory_mb":     payload.MemoryMB,
		"updated_at":    time.Now().Unix(),
	}

	// 序列化 account_ids 和 event_worker_stats 為 JSON
	if data, err := json.Marshal(payload.AccountIDs); err == nil {
		fields["account_ids"] = string(data)
	}
	if data, err := json.Marshal(payload.EventWorkerStats); err == nil {
		fields["event_worker_stats"] = string(data)
	}

	if err := c.redis.HSet(ctx, infoKey, fields).Err(); err != nil {
		logger.Warnw("保存 Connector 資訊失敗", "connector_id", connectorID, "error", err)
	}
}

// healthCheckLoop 健康檢查迴圈
func (c *EventConsumer) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.checkConnectorHealth(ctx)
		}
	}
}

// checkConnectorHealth 檢查 Connector 健康狀態
func (c *EventConsumer) checkConnectorHealth(ctx context.Context) {
	routing := c.gateway.GetRoutingService()

	// 取得死亡的 Connector
	deadConnectors, err := routing.GetDeadConnectors(ctx)
	if err != nil {
		logger.Warnw("取得死亡 Connector 失敗", "error", err)
		return
	}

	for _, connectorID := range deadConnectors {
		logger.Warnw("偵測到 Connector 已死亡", "connector_id", connectorID)

		// 只清理 Redis 中的 Connector 記錄，不重新分配帳號
		// 帳號保持綁定原 Connector，等待該 Connector 重啟後自動恢復
		if err := routing.CleanupDeadConnector(ctx, connectorID); err != nil {
			logger.Errorw("清理 Connector 失敗", "connector_id", connectorID, "error", err)
		}
	}
}

// GetConnectorHealthStatus 取得 Connector 健康狀態
func (c *EventConsumer) GetConnectorHealthStatus() map[string]time.Time {
	c.healthMu.RLock()
	defer c.healthMu.RUnlock()

	result := make(map[string]time.Time)
	for k, v := range c.connectorHealth {
		result[k] = v
	}
	return result
}

// DefaultEventHandler 預設事件處理器（空實現，用於測試）
type DefaultEventHandler struct{}

func (h *DefaultEventHandler) OnMessageReceived(ctx context.Context, event *protocol.Event, payload *protocol.MessageReceivedPayload) error {
	logger.Ctx(ctx).Debugw("收到訊息", "from_jid", payload.SenderJID, "content", payload.Content)
	return nil
}

func (h *DefaultEventHandler) OnMessageSent(ctx context.Context, event *protocol.Event, payload *protocol.MessageSentPayload) error {
	logger.Ctx(ctx).Debugw("訊息已發送", "message_id", payload.MessageID)
	return nil
}

func (h *DefaultEventHandler) OnReceipt(ctx context.Context, event *protocol.Event, payload *protocol.ReceiptPayload) error {
	logger.Ctx(ctx).Debugw("收到回執", "message_id", payload.MessageID, "receipt_type", payload.ReceiptType)
	return nil
}

func (h *DefaultEventHandler) OnConnected(ctx context.Context, event *protocol.Event, payload *protocol.ConnectedPayload) error {
	logger.Ctx(ctx).Debugw("帳號已連線", "phone", payload.PhoneNumber)
	return nil
}

func (h *DefaultEventHandler) OnDisconnected(ctx context.Context, event *protocol.Event, payload *protocol.DisconnectedPayload) error {
	logger.Ctx(ctx).Warnw("帳號已斷線", "reason", payload.Reason)
	return nil
}

func (h *DefaultEventHandler) OnLoggedOut(ctx context.Context, event *protocol.Event, payload *protocol.LoggedOutPayload) error {
	logger.Ctx(ctx).Warnw("帳號已登出", "reason", payload.Reason)
	return nil
}

func (h *DefaultEventHandler) OnQRCode(ctx context.Context, event *protocol.Event, payload *protocol.QRCodePayload) error {
	logger.Ctx(ctx).Debugw("QR Code 已生成", "session_id", payload.SessionID)
	return nil
}

func (h *DefaultEventHandler) OnPairingCode(ctx context.Context, event *protocol.Event, payload *protocol.PairingCodePayload) error {
	logger.Ctx(ctx).Debugw("配對碼已生成", "session_id", payload.SessionID, "pairing_code", payload.PairingCode)
	return nil
}

func (h *DefaultEventHandler) OnLoginSuccess(ctx context.Context, event *protocol.Event, payload *protocol.LoginSuccessPayload) error {
	logger.Ctx(ctx).Debugw("登入成功", "session_id", payload.SessionID, "jid", payload.JID)
	return nil
}

func (h *DefaultEventHandler) OnLoginFailed(ctx context.Context, event *protocol.Event, payload *protocol.LoginFailedPayload) error {
	logger.Ctx(ctx).Warnw("登入失敗", "session_id", payload.SessionID, "reason", payload.Reason)
	return nil
}

func (h *DefaultEventHandler) OnLoginCancelled(ctx context.Context, event *protocol.Event, payload *protocol.LoginCancelledPayload) error {
	logger.Ctx(ctx).Debugw("登入已取消", "session_id", payload.SessionID)
	return nil
}

func (h *DefaultEventHandler) OnSyncProgress(ctx context.Context, event *protocol.Event, payload *protocol.SyncProgressPayload) error {
	logger.Ctx(ctx).Debugw("同步進度", "sync_type", payload.SyncType, "current", payload.Current, "total", payload.Total)
	return nil
}

func (h *DefaultEventHandler) OnSyncComplete(ctx context.Context, event *protocol.Event, payload *protocol.SyncCompletePayload) error {
	logger.Ctx(ctx).Debugw("同步完成", "sync_type", payload.SyncType, "count", payload.Count)
	return nil
}

func (h *DefaultEventHandler) OnProfileUpdated(ctx context.Context, event *protocol.Event, payload *protocol.ProfileUpdatedPayload) error {
	logger.Ctx(ctx).Debugw("帳號資料已更新")
	return nil
}

func (h *DefaultEventHandler) OnGroupsSync(ctx context.Context, event *protocol.Event, payload *protocol.GroupsSyncPayload) error {
	logger.Ctx(ctx).Debugw("群組同步", "count", len(payload.Groups))
	return nil
}

func (h *DefaultEventHandler) OnChatsUpdated(ctx context.Context, event *protocol.Event, payload *protocol.ChatsUpdatedPayload) error {
	logger.Ctx(ctx).Debugw("Chat 更新", "count", len(payload.Chats))
	return nil
}

func (h *DefaultEventHandler) OnMessageRevoked(ctx context.Context, event *protocol.Event, payload *protocol.MessageRevokedPayload) error {
	logger.Ctx(ctx).Debugw("訊息被撤回", "message_id", payload.MessageID)
	return nil
}

func (h *DefaultEventHandler) OnMessageEdited(ctx context.Context, event *protocol.Event, payload *protocol.MessageEditedPayload) error {
	logger.Ctx(ctx).Debugw("訊息被編輯", "message_id", payload.MessageID)
	return nil
}

func (h *DefaultEventHandler) OnMessageDeletedForMe(ctx context.Context, event *protocol.Event, payload *protocol.MessageDeletedForMePayload) error {
	logger.Ctx(ctx).Debugw("訊息被刪除（僅自己）", "message_id", payload.MessageID)
	return nil
}

func (h *DefaultEventHandler) OnChatArchiveChanged(ctx context.Context, event *protocol.Event, payload *protocol.ChatArchiveChangedPayload) error {
	return nil
}

func (h *DefaultEventHandler) OnChatArchiveBatch(ctx context.Context, event *protocol.Event, payload *protocol.ChatArchiveBatchPayload) error {
	return nil
}

func (h *DefaultEventHandler) OnMediaDownloaded(ctx context.Context, event *protocol.Event, payload *protocol.MediaDownloadedPayload) error {
	return nil
}
