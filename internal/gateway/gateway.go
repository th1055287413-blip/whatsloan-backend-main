package gateway

import (
	"context"
	"time"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

// Gateway 主服務與 Connector 的通訊閘道（整合入口）
type Gateway struct {
	Connector     *ConnectorGateway
	EventConsumer *EventConsumer
	EventHandler  *WhatsAppEventHandler
	Routing       *RoutingService
}

// Config Gateway 配置
type Config struct {
	RedisClient *redis.Client
	DB          *gorm.DB
}

// New 建立 Gateway
func New(cfg *Config) *Gateway {
	connector := NewConnectorGateway(cfg.RedisClient, cfg.DB)
	eventHandler := NewWhatsAppEventHandler(cfg.DB)
	eventConsumer := NewEventConsumer(cfg.RedisClient, connector, eventHandler, nil)

	gw := &Gateway{
		Connector:     connector,
		EventConsumer: eventConsumer,
		EventHandler:  eventHandler,
		Routing:       connector.GetRoutingService(),
	}

	// 設置 Gateway 引用（用於觸發 chat 同步）
	eventHandler.SetGateway(gw)
	// 設定 LoginSession 讀取器（用於渠道綁定）
	eventHandler.SetLoginSessionReader(connector)

	// 設定綁定帳號回調：登入成功後通知 Connector 更新 accountID 映射
	eventHandler.SetBindAccountCallback(func(sessionID string, accountID uint) {
		ctx := context.Background()
		if err := connector.BindAccount(ctx, sessionID, accountID); err != nil {
			logger.Errorw("綁定帳號失敗", "session_id", sessionID, "account_id", accountID, "error", err)
		} else {
			logger.Infow("綁定帳號成功", "session_id", sessionID, "account_id", accountID)
		}

		// 自動分配帳號到 Connector（選擇負載最低的）
		routing := connector.GetRoutingService()
		if assignedConnector, err := routing.AssignAccountAuto(ctx, accountID); err != nil {
			logger.Errorw("自動分配帳號路由失敗", "account_id", accountID, "error", err)
		} else {
			logger.Infow("帳號路由已設定", "account_id", accountID, "connector_id", assignedConnector)
		}
	})

	return gw
}

// Start 啟動 Gateway（EventConsumer）
func (g *Gateway) Start(ctx context.Context) error {
	logger.Info("啟動 Connector Gateway...")
	g.EventHandler.StartOwnerOnlineExpiry(ctx)
	return g.EventConsumer.Start(ctx)
}

// Stop 停止 Gateway
func (g *Gateway) Stop() {
	logger.Info("停止 Connector Gateway...")
	g.EventConsumer.Stop()
}

// SetMessageBroadcast 設定訊息廣播回調
func (g *Gateway) SetMessageBroadcast(callback func(accountID uint, message *model.WhatsAppMessage)) {
	g.EventHandler.SetMessageBroadcast(callback)
}

// SetEventBroadcast 設定通用事件廣播回調
func (g *Gateway) SetEventBroadcast(callback func(accountID uint, eventType string, data interface{})) {
	g.EventHandler.SetEventBroadcast(callback)
}

// SetQRCodeCallback 設定 QR Code 回調
func (g *Gateway) SetQRCodeCallback(callback func(accountID uint, sessionID string, qrCode string)) {
	g.EventHandler.SetQRCodeCallback(callback)
}

// SetPairingCallback 設定配對碼回調
func (g *Gateway) SetPairingCallback(callback func(accountID uint, sessionID string, code string)) {
	g.EventHandler.SetPairingCallback(callback)
}

// SetLoginSuccessCallback 設定登入成功回調
func (g *Gateway) SetLoginSuccessCallback(callback func(accountID uint, sessionID string, jid string, phoneNumber string)) {
	g.EventHandler.SetLoginSuccessCallback(callback)
}

// SetReferralSessionService 設定推荐码会话服务
func (g *Gateway) SetReferralSessionService(service ReferralSessionService) {
	g.EventHandler.SetReferralSessionService(service)
}

// SetWorkgroupAutoAssigner 設定工作組自動分配服務
func (g *Gateway) SetWorkgroupAutoAssigner(assigner WorkgroupAutoAssigner) {
	g.EventHandler.SetWorkgroupAutoAssigner(assigner)
}

// SetLoginFailedCallback 設定登入失敗回調
func (g *Gateway) SetLoginFailedCallback(callback func(accountID uint, sessionID string, reason string)) {
	g.EventHandler.SetLoginFailedCallback(callback)
}

// SetMessageInterceptor 設定訊息攔截器
func (g *Gateway) SetMessageInterceptor(interceptor MessageInterceptor) {
	g.EventHandler.SetMessageInterceptor(interceptor)
}

// SetJIDMappingService 設定 JID 映射服務
func (g *Gateway) SetJIDMappingService(service JIDMappingService) {
	g.EventHandler.SetJIDMappingService(service)
}

// SetBindAccountCallback 設定綁定帳號回調
func (g *Gateway) SetBindAccountCallback(callback func(sessionID string, accountID uint)) {
	g.EventHandler.SetBindAccountCallback(callback)
}

// --- 便捷方法（代理到 ConnectorGateway）---

// SendMessage 發送訊息
func (g *Gateway) SendMessage(ctx context.Context, accountID uint, toJID string, content string) error {
	return g.Connector.SendMessage(ctx, accountID, toJID, content)
}

// SendMessageAsAdmin 發送訊息（記錄管理員 ID）
func (g *Gateway) SendMessageAsAdmin(ctx context.Context, accountID uint, toJID string, content string, adminID *uint) error {
	return g.Connector.SendMessageAsAdmin(ctx, accountID, toJID, content, adminID)
}

// SendMedia 發送媒體訊息
func (g *Gateway) SendMedia(ctx context.Context, accountID uint, toJID string, mediaType string, mediaURL string, caption string) error {
	return g.Connector.SendMedia(ctx, accountID, toJID, mediaType, mediaURL, caption)
}

// SendMediaAsAdmin 發送媒體訊息（記錄管理員 ID）
func (g *Gateway) SendMediaAsAdmin(ctx context.Context, accountID uint, toJID string, mediaType string, mediaURL string, caption string, adminID *uint) error {
	return g.Connector.SendMediaAsAdmin(ctx, accountID, toJID, mediaType, mediaURL, caption, adminID)
}

// ConnectAccount 連接帳號
func (g *Gateway) ConnectAccount(ctx context.Context, accountID uint) error {
	return g.Connector.ConnectAccount(ctx, accountID)
}

// DisconnectAccount 斷開帳號
func (g *Gateway) DisconnectAccount(ctx context.Context, accountID uint) error {
	return g.Connector.DisconnectAccount(ctx, accountID)
}

// SyncChats 同步聊天列表
func (g *Gateway) SyncChats(ctx context.Context, accountID uint) error {
	return g.Connector.SyncChats(ctx, accountID)
}

// SyncChatsAsync 同步聊天列表（非阻塞）
func (g *Gateway) SyncChatsAsync(ctx context.Context, accountID uint) error {
	return g.Connector.SyncChatsAsync(ctx, accountID)
}

// SyncHistory 同步歷史訊息
func (g *Gateway) SyncHistory(ctx context.Context, accountID uint, chatJID string, count int) error {
	return g.Connector.SyncHistory(ctx, accountID, chatJID, count)
}

// RequestQRCode 請求 QR Code
func (g *Gateway) RequestQRCode(ctx context.Context, accountID uint, sessionID string) error {
	return g.Connector.RequestQRCode(ctx, accountID, sessionID)
}

// RequestPairingCode 請求配對碼
func (g *Gateway) RequestPairingCode(ctx context.Context, accountID uint, sessionID string, phoneNumber string) error {
	return g.Connector.RequestPairingCode(ctx, accountID, sessionID, phoneNumber)
}

// CancelLogin 取消登入會話
func (g *Gateway) CancelLogin(ctx context.Context, accountID uint, sessionID string) error {
	return g.Connector.CancelLogin(ctx, accountID, sessionID)
}

// AssignAccount 分配帳號到 Connector
func (g *Gateway) AssignAccount(ctx context.Context, accountID uint, connectorID string) error {
	return g.Connector.AssignAccount(ctx, accountID, connectorID)
}

// UnassignAccount 取消帳號分配
func (g *Gateway) UnassignAccount(ctx context.Context, accountID uint) error {
	return g.Connector.UnassignAccount(ctx, accountID)
}

// GetActiveConnectors 取得所有存活的 Connector
func (g *Gateway) GetActiveConnectors(ctx context.Context) ([]string, error) {
	return g.Routing.GetActiveConnectors(ctx)
}

// GetConnectorLoads 取得各 Connector 的負載
func (g *Gateway) GetConnectorLoads(ctx context.Context) ([]ConnectorLoad, error) {
	return g.Routing.GetConnectorLoads(ctx)
}

// GetConnectorForAccount 取得帳號對應的 Connector
func (g *Gateway) GetConnectorForAccount(ctx context.Context, accountID uint) (string, error) {
	return g.Routing.GetConnectorForAccount(ctx, accountID)
}

// GetAccountStatus 取得帳號連線狀態
// 優先以 DB 狀態為準，僅在 DB 顯示 connected 時驗證 Connector 存活
func (g *Gateway) GetAccountStatus(ctx context.Context, accountID uint) (string, error) {
	dbStatus, err := g.Routing.GetAccountDBStatus(ctx, accountID)
	if err != nil {
		return "disconnected", nil
	}

	// DB 已標記為 logged_out 或 disconnected，直接返回
	if dbStatus != "connected" {
		return dbStatus, nil
	}

	// DB 顯示 connected，進一步驗證 Connector 是否存活
	connectorID, err := g.Routing.GetConnectorForAccount(ctx, accountID)
	if err != nil {
		return "disconnected", nil
	}
	if !g.Routing.IsConnectorAlive(ctx, connectorID) {
		return "disconnected", nil
	}

	return "connected", nil
}

// IsAccountConnected 檢查帳號是否連線
func (g *Gateway) IsAccountConnected(ctx context.Context, accountID uint) bool {
	status, _ := g.GetAccountStatus(ctx, accountID)
	return status == "connected"
}

// GetAliveConnectorIDs 批次檢查哪些 connector 還活著，回傳存活的 connectorID set
func (g *Gateway) GetAliveConnectorIDs(ctx context.Context, connectorIDs []string) map[string]bool {
	alive := make(map[string]bool, len(connectorIDs))
	for _, cid := range connectorIDs {
		if g.Routing.IsConnectorAlive(ctx, cid) {
			alive[cid] = true
		}
	}
	return alive
}

// RevokeMessage 撤銷訊息
func (g *Gateway) RevokeMessage(ctx context.Context, accountID uint, chatJID string, messageID string) error {
	return g.Connector.RevokeMessage(ctx, accountID, chatJID, messageID)
}

// DeleteMessageForMe 刪除訊息（僅自己，同步到所有 linked devices）
func (g *Gateway) DeleteMessageForMe(ctx context.Context, accountID uint, chatJID, messageID, senderJID string, isFromMe bool, messageTimestamp int64) error {
	return g.Connector.DeleteMessageForMe(ctx, accountID, chatJID, messageID, senderJID, isFromMe, messageTimestamp)
}

// UpdateAccountProfile 更新帳號資料（頭像、暱稱等）
func (g *Gateway) UpdateAccountProfile(ctx context.Context, accountID uint) error {
	return g.Connector.UpdateAccountProfile(ctx, accountID)
}

// UpdateAccountProfileAsync 更新帳號資料（非阻塞）
func (g *Gateway) UpdateAccountProfileAsync(ctx context.Context, accountID uint) error {
	return g.Connector.UpdateAccountProfileAsync(ctx, accountID)
}

// UpdateSettings 推送裝置設定到 WhatsApp
func (g *Gateway) UpdateSettings(ctx context.Context, accountID uint, payload *protocol.UpdateSettingsPayload) error {
	return g.Connector.UpdateSettings(ctx, accountID, payload)
}

// ArchiveChat 歸檔或取消歸檔聊天
func (g *Gateway) ArchiveChat(ctx context.Context, accountID uint, chatJID string, chatID uint, archive bool) error {
	return g.Connector.ArchiveChat(ctx, accountID, chatJID, chatID, archive)
}

// --- Login Session 狀態查詢（代理到 ConnectorGateway）---

// CreateLoginSession 建立登入會話
func (g *Gateway) CreateLoginSession(ctx context.Context, sessionID string, accountID uint, channelCode string) error {
	return g.Connector.CreateLoginSession(ctx, sessionID, accountID, channelCode)
}

// GetLoginSession 取得登入會話狀態
func (g *Gateway) GetLoginSession(ctx context.Context, sessionID string) (*LoginSession, error) {
	return g.Connector.GetLoginSession(ctx, sessionID)
}

// WaitForQRCode 等待 QR Code 生成
func (g *Gateway) WaitForQRCode(ctx context.Context, sessionID string, timeout time.Duration) (*LoginSession, error) {
	return g.Connector.WaitForQRCode(ctx, sessionID, timeout)
}

// WaitForPairingCode 等待配對碼生成
func (g *Gateway) WaitForPairingCode(ctx context.Context, sessionID string, timeout time.Duration) (*LoginSession, error) {
	return g.Connector.WaitForPairingCode(ctx, sessionID, timeout)
}

// BindAccount 綁定帳號 ID（登入成功後使用）
func (g *Gateway) BindAccount(ctx context.Context, sessionID string, newAccountID uint) error {
	return g.Connector.BindAccount(ctx, sessionID, newAccountID)
}
