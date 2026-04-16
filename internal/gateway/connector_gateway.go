package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

// CommandResult 命令執行結果
type CommandResult struct {
	Success bool
	Error   string
}

// ConnectorGateway 主服務與 Connector 的通訊閘道
type ConnectorGateway struct {
	redis   *redis.Client
	routing *RoutingService

	// 命令追蹤（用於超時重試）
	pendingCommands map[string]*PendingCommand
	mu              sync.RWMutex

	// 命令響應等待（用於同步等待）
	commandResponses map[string]chan *CommandResult
	responseMu       sync.RWMutex
}

// PendingCommand 待處理命令
type PendingCommand struct {
	Command    *protocol.Command
	SentAt     time.Time
	RetryCount int
	MaxRetries int
}

// NewConnectorGateway 建立 Connector 閘道
func NewConnectorGateway(redisClient *redis.Client, db *gorm.DB) *ConnectorGateway {
	return &ConnectorGateway{
		redis:            redisClient,
		routing:          NewRoutingService(redisClient, db),
		pendingCommands:  make(map[string]*PendingCommand),
		commandResponses: make(map[string]chan *CommandResult),
	}
}

// GetRoutingService 取得路由服務
func (g *ConnectorGateway) GetRoutingService() *RoutingService {
	return g.routing
}

// SendCommand 發送命令到指定 Connector
func (g *ConnectorGateway) SendCommand(ctx context.Context, connectorID string, cmd *protocol.Command) error {
	var streamName string
	if protocol.IsBulkCommand(cmd.Type) {
		streamName = protocol.GetBulkCommandStreamName(connectorID)
	} else {
		streamName = protocol.GetPriorityCommandStreamName(connectorID)
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("序列化命令失敗: %w", err)
	}

	_, err = g.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: streamName,
		MaxLen: 10000,
		Approx: true,
		Values: map[string]interface{}{
			"command":    string(data),
			"account_id": cmd.AccountID,
			"type":       string(cmd.Type),
		},
	}).Result()

	if err != nil {
		return fmt.Errorf("發送命令失敗: %w", err)
	}

	// 追蹤命令
	g.trackCommand(cmd)

	logger.Infow("命令已發送", "command_type", cmd.Type, "account_id", cmd.AccountID, "connector_id", connectorID)
	return nil
}

// SendCommandToAccount 發送命令到帳號對應的 Connector
func (g *ConnectorGateway) SendCommandToAccount(ctx context.Context, cmd *protocol.Command) error {
	connectorID, err := g.routing.GetConnectorForAccount(ctx, cmd.AccountID)
	if err != nil {
		return err
	}

	// 檢查 Connector 是否存活
	if !g.routing.IsConnectorAlive(ctx, connectorID) {
		return fmt.Errorf("Connector %s 已離線", connectorID)
	}

	return g.SendCommand(ctx, connectorID, cmd)
}

// BroadcastCommand 廣播命令到所有活動的 Connector
func (g *ConnectorGateway) BroadcastCommand(ctx context.Context, cmd *protocol.Command) error {
	connectors, err := g.routing.GetActiveConnectors(ctx)
	if err != nil {
		return fmt.Errorf("取得活動 Connector 列表失敗: %w", err)
	}

	if len(connectors) == 0 {
		return fmt.Errorf("沒有活動的 Connector")
	}

	var lastErr error
	successCount := 0
	for _, connectorID := range connectors {
		if err := g.SendCommand(ctx, connectorID, cmd); err != nil {
			lastErr = err
			logger.Warnw("廣播命令到 Connector 失敗", "connector_id", connectorID, "error", err)
		} else {
			successCount++
		}
	}

	if successCount == 0 {
		return fmt.Errorf("廣播命令失敗: %w", lastErr)
	}

	return nil
}

// trackCommand 追蹤命令
func (g *ConnectorGateway) trackCommand(cmd *protocol.Command) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.pendingCommands[cmd.ID] = &PendingCommand{
		Command:    cmd,
		SentAt:     time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}
}

// AcknowledgeCommand 確認命令已處理
func (g *ConnectorGateway) AcknowledgeCommand(commandID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.pendingCommands, commandID)
}

// --- 同步命令執行 ---

// SendCommandAndWait 發送命令並等待執行結果
func (g *ConnectorGateway) SendCommandAndWait(ctx context.Context, connectorID string, cmd *protocol.Command, timeout time.Duration) error {
	// 建立響應 channel
	respCh := make(chan *CommandResult, 1)
	g.responseMu.Lock()
	g.commandResponses[cmd.ID] = respCh
	g.responseMu.Unlock()

	// 確保清理
	defer func() {
		g.responseMu.Lock()
		delete(g.commandResponses, cmd.ID)
		g.responseMu.Unlock()
	}()

	// 發送命令
	if err := g.SendCommand(ctx, connectorID, cmd); err != nil {
		return err
	}

	// 等待響應或超時
	select {
	case result := <-respCh:
		if !result.Success {
			return fmt.Errorf("%s", result.Error)
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("命令執行超時")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendCommandToAccountAndWait 發送命令到帳號對應的 Connector 並等待結果
func (g *ConnectorGateway) SendCommandToAccountAndWait(ctx context.Context, cmd *protocol.Command, timeout time.Duration) error {
	connectorID, err := g.routing.GetConnectorForAccount(ctx, cmd.AccountID)
	if err != nil {
		return err
	}

	// 檢查 Connector 是否存活
	if !g.routing.IsConnectorAlive(ctx, connectorID) {
		return fmt.Errorf("Connector %s 已離線", connectorID)
	}

	return g.SendCommandAndWait(ctx, connectorID, cmd, timeout)
}

// NotifyCommandSuccess 通知命令執行成功（由 EventConsumer 調用）
func (g *ConnectorGateway) NotifyCommandSuccess(commandID string) {
	g.responseMu.RLock()
	respCh, exists := g.commandResponses[commandID]
	g.responseMu.RUnlock()

	if exists {
		select {
		case respCh <- &CommandResult{Success: true}:
		default:
			// channel 已滿或已關閉，忽略
		}
	}

	// 同時清理 pendingCommands
	g.AcknowledgeCommand(commandID)
}

// NotifyCommandError 通知命令執行失敗（由 EventConsumer 調用）
func (g *ConnectorGateway) NotifyCommandError(commandID string, errMsg string) {
	g.responseMu.RLock()
	respCh, exists := g.commandResponses[commandID]
	g.responseMu.RUnlock()

	if exists {
		select {
		case respCh <- &CommandResult{Success: false, Error: errMsg}:
		default:
			// channel 已滿或已關閉，忽略
		}
	}

	// 同時清理 pendingCommands
	g.AcknowledgeCommand(commandID)
}

// GetPendingCommand 取得待處理命令
func (g *ConnectorGateway) GetPendingCommand(commandID string) *PendingCommand {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.pendingCommands[commandID]
}

// --- 便捷方法 ---

// SendMessage 發送訊息（同步等待結果）
func (g *ConnectorGateway) SendMessage(ctx context.Context, accountID uint, toJID string, content string) error {
	return g.SendMessageAsAdmin(ctx, accountID, toJID, content, nil)
}

// SendMessageAsAdmin 發送訊息（同步等待結果，記錄管理員 ID）
func (g *ConnectorGateway) SendMessageAsAdmin(ctx context.Context, accountID uint, toJID string, content string, adminID *uint) error {
	payload := &protocol.SendMessagePayload{
		ToJID:         toJID,
		Content:       content,
		SentByAdminID: adminID,
	}

	cmd, err := protocol.NewCommand(protocol.CmdSendMessage, accountID, payload)
	if err != nil {
		return err
	}

	return g.SendCommandToAccountAndWait(ctx, cmd, defaultCommandTimeout)
}

// SendMedia 發送媒體訊息（同步等待結果）
func (g *ConnectorGateway) SendMedia(ctx context.Context, accountID uint, toJID string, mediaType string, mediaURL string, caption string) error {
	return g.SendMediaAsAdmin(ctx, accountID, toJID, mediaType, mediaURL, caption, nil)
}

// SendMediaAsAdmin 發送媒體訊息（同步等待結果，記錄管理員 ID）
func (g *ConnectorGateway) SendMediaAsAdmin(ctx context.Context, accountID uint, toJID string, mediaType string, mediaURL string, caption string, adminID *uint) error {
	payload := &protocol.SendMediaPayload{
		ToJID:         toJID,
		MediaType:     mediaType,
		MediaURL:      mediaURL,
		Caption:       caption,
		SentByAdminID: adminID,
	}

	cmd, err := protocol.NewCommand(protocol.CmdSendMedia, accountID, payload)
	if err != nil {
		return err
	}

	return g.SendCommandToAccountAndWait(ctx, cmd, defaultCommandTimeout)
}

// ConnectAccount 連接帳號
func (g *ConnectorGateway) ConnectAccount(ctx context.Context, accountID uint) error {
	// 先檢查是否已分配
	connectorID, err := g.routing.GetConnectorForAccount(ctx, accountID)
	if err != nil {
		// 尚未分配，自動分配
		connectorID, err = g.routing.AssignAccountAuto(ctx, accountID)
		if err != nil {
			return fmt.Errorf("分配 Connector 失敗: %w", err)
		}
	}

	cmd, err := protocol.NewCommand(protocol.CmdConnect, accountID, nil)
	if err != nil {
		return err
	}

	return g.SendCommand(ctx, connectorID, cmd)
}

// DisconnectAccount 斷開帳號（同步等待結果）
func (g *ConnectorGateway) DisconnectAccount(ctx context.Context, accountID uint) error {
	cmd, err := protocol.NewCommand(protocol.CmdDisconnect, accountID, nil)
	if err != nil {
		return err
	}

	return g.SendCommandToAccountAndWait(ctx, cmd, defaultCommandTimeout)
}

// SyncChats 同步聊天列表（同步等待結果）
func (g *ConnectorGateway) SyncChats(ctx context.Context, accountID uint) error {
	cmd, err := protocol.NewCommand(protocol.CmdSyncChats, accountID, nil)
	if err != nil {
		return err
	}

	return g.SendCommandToAccountAndWait(ctx, cmd, defaultCommandTimeout)
}

// SyncChatsAsync 同步聊天列表（非阻塞，不等待結果）
// 用於後台同步任務，避免大量帳號同時連線時命令排隊超時
func (g *ConnectorGateway) SyncChatsAsync(ctx context.Context, accountID uint) error {
	cmd, err := protocol.NewCommand(protocol.CmdSyncChats, accountID, nil)
	if err != nil {
		return err
	}

	return g.SendCommandToAccount(ctx, cmd)
}

// SyncHistory 同步歷史訊息（同步等待結果）
func (g *ConnectorGateway) SyncHistory(ctx context.Context, accountID uint, chatJID string, count int) error {
	payload := &protocol.SyncHistoryPayload{
		ChatJID: chatJID,
		Count:   count,
	}

	cmd, err := protocol.NewCommand(protocol.CmdSyncHistory, accountID, payload)
	if err != nil {
		return err
	}

	return g.SendCommandToAccountAndWait(ctx, cmd, defaultCommandTimeout)
}

// SyncContacts 同步聯絡人（同步等待結果）
func (g *ConnectorGateway) SyncContacts(ctx context.Context, accountID uint) error {
	cmd, err := protocol.NewCommand(protocol.CmdSyncContacts, accountID, nil)
	if err != nil {
		return err
	}

	return g.SendCommandToAccountAndWait(ctx, cmd, defaultCommandTimeout)
}

// RequestQRCode 請求 QR Code
func (g *ConnectorGateway) RequestQRCode(ctx context.Context, accountID uint, sessionID string) error {
	// QR Code 請求需要先分配 Connector
	connectorID, err := g.routing.GetConnectorForAccount(ctx, accountID)
	if err != nil {
		// 尚未分配，自動分配
		connectorID, err = g.routing.AssignAccountAuto(ctx, accountID)
		if err != nil {
			return fmt.Errorf("分配 Connector 失敗: %w", err)
		}
	}

	payload := &protocol.GetQRCodePayload{
		SessionID: sessionID,
	}

	cmd, err := protocol.NewCommand(protocol.CmdGetQRCode, accountID, payload)
	if err != nil {
		return err
	}

	return g.SendCommand(ctx, connectorID, cmd)
}

// RequestPairingCode 請求配對碼
func (g *ConnectorGateway) RequestPairingCode(ctx context.Context, accountID uint, sessionID string, phoneNumber string) error {
	// 配對請求需要先分配 Connector（用電話號碼做國碼路由）
	connectorID, err := g.routing.GetConnectorForAccount(ctx, accountID)
	if err != nil {
		// 尚未分配，自動分配（帶電話號碼以供國碼路由）
		connectorID, err = g.routing.AssignAccountAuto(ctx, accountID, phoneNumber)
		if err != nil {
			return fmt.Errorf("分配 Connector 失敗: %w", err)
		}
	}

	payload := &protocol.GetPairingCodePayload{
		SessionID:   sessionID,
		PhoneNumber: phoneNumber,
	}

	cmd, err := protocol.NewCommand(protocol.CmdGetPairingCode, accountID, payload)
	if err != nil {
		return err
	}

	return g.SendCommand(ctx, connectorID, cmd)
}

// AssignAccount 分配帳號到 Connector（手動指定或自動）
func (g *ConnectorGateway) AssignAccount(ctx context.Context, accountID uint, connectorID string) error {
	if connectorID == "" {
		// 自動分配
		_, err := g.routing.AssignAccountAuto(ctx, accountID)
		return err
	}

	// 檢查 Connector 是否存活
	if !g.routing.IsConnectorAlive(ctx, connectorID) {
		return fmt.Errorf("Connector %s 不存在或已離線", connectorID)
	}

	return g.routing.AssignAccountToConnector(ctx, accountID, connectorID)
}

// UnassignAccount 取消帳號分配
func (g *ConnectorGateway) UnassignAccount(ctx context.Context, accountID uint) error {
	return g.routing.RemoveAccountRouting(ctx, accountID)
}

// CancelLogin 取消登入會話（同步等待結果）
func (g *ConnectorGateway) CancelLogin(ctx context.Context, accountID uint, sessionID string) error {
	payload := &protocol.CancelLoginPayload{
		SessionID: sessionID,
	}

	cmd, err := protocol.NewCommand(protocol.CmdCancelLogin, accountID, payload)
	if err != nil {
		return err
	}

	return g.SendCommandToAccountAndWait(ctx, cmd, defaultCommandTimeout)
}

// RevokeMessage 撤銷訊息（同步等待結果）
func (g *ConnectorGateway) RevokeMessage(ctx context.Context, accountID uint, chatJID string, messageID string) error {
	payload := &protocol.RevokeMessagePayload{
		ChatJID:   chatJID,
		MessageID: messageID,
	}

	cmd, err := protocol.NewCommand(protocol.CmdRevokeMessage, accountID, payload)
	if err != nil {
		return err
	}

	return g.SendCommandToAccountAndWait(ctx, cmd, defaultCommandTimeout)
}

// DeleteMessageForMe 刪除訊息（僅自己，同步等待結果）
func (g *ConnectorGateway) DeleteMessageForMe(ctx context.Context, accountID uint, chatJID, messageID, senderJID string, isFromMe bool, messageTimestamp int64) error {
	payload := &protocol.DeleteMessageForMePayload{
		ChatJID:          chatJID,
		MessageID:        messageID,
		IsFromMe:         isFromMe,
		SenderJID:        senderJID,
		MessageTimestamp: messageTimestamp,
	}

	cmd, err := protocol.NewCommand(protocol.CmdDeleteMessageForMe, accountID, payload)
	if err != nil {
		return err
	}

	return g.SendCommandToAccountAndWait(ctx, cmd, defaultCommandTimeout)
}

// UpdateAccountProfile 更新帳號資料（頭像、暱稱等，同步等待結果）
func (g *ConnectorGateway) UpdateAccountProfile(ctx context.Context, accountID uint) error {
	cmd, err := protocol.NewCommand(protocol.CmdUpdateProfile, accountID, nil)
	if err != nil {
		return err
	}

	return g.SendCommandToAccountAndWait(ctx, cmd, defaultCommandTimeout)
}

// UpdateAccountProfileAsync 更新帳號資料（非阻塞，不等待結果）
// 用於後台同步任務，避免大量帳號同時連線時命令排隊超時
func (g *ConnectorGateway) UpdateAccountProfileAsync(ctx context.Context, accountID uint) error {
	cmd, err := protocol.NewCommand(protocol.CmdUpdateProfile, accountID, nil)
	if err != nil {
		return err
	}

	return g.SendCommandToAccount(ctx, cmd)
}

// UpdateSettings 推送裝置設定到 WhatsApp
func (g *ConnectorGateway) UpdateSettings(ctx context.Context, accountID uint, payload *protocol.UpdateSettingsPayload) error {
	cmd, err := protocol.NewCommand(protocol.CmdUpdateSettings, accountID, payload)
	if err != nil {
		return err
	}

	return g.SendCommandToAccountAndWait(ctx, cmd, defaultCommandTimeout)
}

// BindAccount 綁定帳號 ID（登入成功後，將 sessionID 對應的 client 綁定到實際帳號 ID）
func (g *ConnectorGateway) BindAccount(ctx context.Context, sessionID string, newAccountID uint) error {
	payload := &protocol.BindAccountPayload{
		SessionID:    sessionID,
		NewAccountID: newAccountID,
	}

	cmd, err := protocol.NewCommand(protocol.CmdBindAccount, 0, payload)
	if err != nil {
		return err
	}

	// 因為 accountID=0 還沒有路由，需要廣播到所有 Connector
	return g.BroadcastCommand(ctx, cmd)
}

// ArchiveChat 歸檔或取消歸檔聊天（實際調用 WhatsApp API，同步等待結果）
func (g *ConnectorGateway) ArchiveChat(ctx context.Context, accountID uint, chatJID string, chatID uint, archive bool) error {
	payload := &protocol.ArchiveChatPayload{
		ChatJID: chatJID,
		Archive: archive,
		ChatID:  chatID,
	}

	cmd, err := protocol.NewCommand(protocol.CmdArchiveChat, accountID, payload)
	if err != nil {
		return err
	}

	return g.SendCommandToAccountAndWait(ctx, cmd, defaultCommandTimeout)
}

// --- 管理命令 ---

// SendManageCommand 發送管理命令到 Connector 服務
func (g *ConnectorGateway) SendManageCommand(ctx context.Context, cmd *protocol.ManageCommand) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("序列化管理命令失敗: %w", err)
	}

	_, err = g.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: protocol.ManageCommandStreamName,
		MaxLen: 1000,
		Approx: true,
		Values: map[string]interface{}{
			"command":      string(data),
			"connector_id": cmd.ConnectorID,
			"type":         string(cmd.Type),
		},
	}).Result()
	if err != nil {
		return fmt.Errorf("發送管理命令失敗: %w", err)
	}

	logger.Infow("管理命令已發送", "type", cmd.Type, "connector_id", cmd.ConnectorID)
	return nil
}

// SendManageCommandAndWait 發送管理命令並等待結果
func (g *ConnectorGateway) SendManageCommandAndWait(ctx context.Context, cmd *protocol.ManageCommand, timeout time.Duration) error {
	respCh := make(chan *CommandResult, 1)
	g.responseMu.Lock()
	g.commandResponses[cmd.ID] = respCh
	g.responseMu.Unlock()

	defer func() {
		g.responseMu.Lock()
		delete(g.commandResponses, cmd.ID)
		g.responseMu.Unlock()
	}()

	if err := g.SendManageCommand(ctx, cmd); err != nil {
		return err
	}

	select {
	case result := <-respCh:
		if !result.Success {
			return fmt.Errorf("%s", result.Error)
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("管理命令執行超時")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// --- Login Session 狀態追蹤 ---

// LoginSessionState 登入會話狀態
type LoginSessionState string

const (
	LoginStatePending   LoginSessionState = "pending"   // 等待掃碼/配對
	LoginStateConnected LoginSessionState = "connected" // 登入成功
	LoginStateFailed    LoginSessionState = "failed"    // 登入失敗
	LoginStateCancelled LoginSessionState = "cancelled" // 已取消
)

// LoginSession 登入會話資訊
type LoginSession struct {
	SessionID   string            `json:"session_id"`
	AccountID   uint              `json:"account_id"`
	State       LoginSessionState `json:"state"`
	QRCode      string            `json:"qr_code,omitempty"`
	PairingCode string            `json:"pairing_code,omitempty"`
	JID         string            `json:"jid,omitempty"`
	PhoneNumber string            `json:"phone_number,omitempty"`
	FailReason  string            `json:"fail_reason,omitempty"`
	ChannelCode string            `json:"channel_code,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

const loginSessionKeyPrefix = "wa:login_session:"
const loginSessionTTL = 10 * time.Minute

// 命令執行超時時間
const defaultCommandTimeout = 30 * time.Second

func (g *ConnectorGateway) loginSessionKey(sessionID string) string {
	return loginSessionKeyPrefix + sessionID
}

// CreateLoginSession 建立登入會話
func (g *ConnectorGateway) CreateLoginSession(ctx context.Context, sessionID string, accountID uint, channelCode string) error {
	session := &LoginSession{
		SessionID:   sessionID,
		AccountID:   accountID,
		State:       LoginStatePending,
		ChannelCode: channelCode,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	data, err := json.Marshal(session)
	if err != nil {
		return err
	}

	return g.redis.Set(ctx, g.loginSessionKey(sessionID), data, loginSessionTTL).Err()
}

// GetLoginSession 取得登入會話狀態
func (g *ConnectorGateway) GetLoginSession(ctx context.Context, sessionID string) (*LoginSession, error) {
	data, err := g.redis.Get(ctx, g.loginSessionKey(sessionID)).Bytes()
	if err == redis.Nil {
		return nil, nil // 會話不存在
	}
	if err != nil {
		return nil, err
	}

	var session LoginSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

// UpdateLoginSessionQRCode 更新 QR Code
func (g *ConnectorGateway) UpdateLoginSessionQRCode(ctx context.Context, sessionID string, qrCode string) error {
	session, err := g.GetLoginSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return nil // 會話不存在，忽略
	}

	session.QRCode = qrCode
	session.UpdatedAt = time.Now()

	data, _ := json.Marshal(session)
	return g.redis.Set(ctx, g.loginSessionKey(sessionID), data, loginSessionTTL).Err()
}

// UpdateLoginSessionPairingCode 更新配對碼
func (g *ConnectorGateway) UpdateLoginSessionPairingCode(ctx context.Context, sessionID string, pairingCode string) error {
	session, err := g.GetLoginSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return nil
	}

	session.PairingCode = pairingCode
	session.UpdatedAt = time.Now()

	data, _ := json.Marshal(session)
	return g.redis.Set(ctx, g.loginSessionKey(sessionID), data, loginSessionTTL).Err()
}

// UpdateLoginSessionSuccess 更新登入成功
func (g *ConnectorGateway) UpdateLoginSessionSuccess(ctx context.Context, sessionID string, jid, phoneNumber string) error {
	session, err := g.GetLoginSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return nil
	}

	session.State = LoginStateConnected
	session.JID = jid
	session.PhoneNumber = phoneNumber
	session.UpdatedAt = time.Now()

	data, _ := json.Marshal(session)
	return g.redis.Set(ctx, g.loginSessionKey(sessionID), data, loginSessionTTL).Err()
}

// UpdateLoginSessionFailed 更新登入失敗
func (g *ConnectorGateway) UpdateLoginSessionFailed(ctx context.Context, sessionID string, reason string) error {
	session, err := g.GetLoginSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return nil
	}

	session.State = LoginStateFailed
	session.FailReason = reason
	session.UpdatedAt = time.Now()

	data, _ := json.Marshal(session)
	return g.redis.Set(ctx, g.loginSessionKey(sessionID), data, loginSessionTTL).Err()
}

// UpdateLoginSessionCancelled 更新登入已取消
func (g *ConnectorGateway) UpdateLoginSessionCancelled(ctx context.Context, sessionID string) error {
	session, err := g.GetLoginSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return nil
	}

	session.State = LoginStateCancelled
	session.UpdatedAt = time.Now()

	data, _ := json.Marshal(session)
	return g.redis.Set(ctx, g.loginSessionKey(sessionID), data, loginSessionTTL).Err()
}

// WaitForQRCode 等待 QR Code 生成
func (g *ConnectorGateway) WaitForQRCode(ctx context.Context, sessionID string, timeout time.Duration) (*LoginSession, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("等待 QR Code 超時")
			}

			session, err := g.GetLoginSession(ctx, sessionID)
			if err != nil {
				return nil, err
			}
			if session == nil {
				return nil, fmt.Errorf("會話不存在")
			}

			// 檢查是否已有 QR Code 或已結束
			if session.QRCode != "" || session.State != LoginStatePending {
				return session, nil
			}
		}
	}
}

// WaitForPairingCode 等待配對碼生成
func (g *ConnectorGateway) WaitForPairingCode(ctx context.Context, sessionID string, timeout time.Duration) (*LoginSession, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("等待配對碼超時")
			}

			session, err := g.GetLoginSession(ctx, sessionID)
			if err != nil {
				return nil, err
			}
			if session == nil {
				return nil, fmt.Errorf("會話不存在")
			}

			// 檢查是否已有配對碼或已結束
			if session.PairingCode != "" || session.State != LoginStatePending {
				return session, nil
			}
		}
	}
}
