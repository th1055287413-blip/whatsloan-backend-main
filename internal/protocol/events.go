package protocol

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// EventType 事件類型
type EventType string

const (
	// EvtMessageReceived 收到訊息
	EvtMessageReceived EventType = "message_received"
	// EvtMessageSent 訊息已發送
	EvtMessageSent EventType = "message_sent"
	// EvtReceipt 訊息回執（已送達、已讀）
	EvtReceipt EventType = "receipt"
	// EvtConnected 帳號已連線
	EvtConnected EventType = "connected"
	// EvtDisconnected 帳號已斷線
	EvtDisconnected EventType = "disconnected"
	// EvtLoggedOut 帳號已登出
	EvtLoggedOut EventType = "logged_out"
	// EvtQRCode QR Code 已生成
	EvtQRCode EventType = "qr_code"
	// EvtPairingCode 配對碼已生成
	EvtPairingCode EventType = "pairing_code"
	// EvtLoginSuccess 登入成功
	EvtLoginSuccess EventType = "login_success"
	// EvtLoginFailed 登入失敗
	EvtLoginFailed EventType = "login_failed"
	// EvtLoginCancelled 登入已取消
	EvtLoginCancelled EventType = "login_cancelled"
	// EvtSyncProgress 同步進度更新
	EvtSyncProgress EventType = "sync_progress"
	// EvtSyncComplete 同步完成
	EvtSyncComplete EventType = "sync_complete"
	// EvtCommandAck 命令確認
	EvtCommandAck EventType = "command_ack"
	// EvtCommandError 命令執行錯誤
	EvtCommandError EventType = "command_error"
	// EvtManageCommandAck 管理命令確認
	EvtManageCommandAck EventType = "manage_command_ack"
	// EvtManageCommandError 管理命令執行錯誤
	EvtManageCommandError EventType = "manage_command_error"
	// EvtHeartbeat Connector 心跳
	EvtHeartbeat EventType = "heartbeat"
	// EvtProfileUpdated 帳號資料已更新
	EvtProfileUpdated EventType = "profile_updated"
	// EvtGroupsSync 群組同步事件
	EvtGroupsSync EventType = "groups_sync"
	// EvtChatsUpdated Chat 列表已更新（包含名稱與頭像）
	EvtChatsUpdated EventType = "chats_updated"
	// EvtMessageRevoked 訊息已被撤回（對方撤回）
	EvtMessageRevoked EventType = "message_revoked"
	// EvtMessageEdited 訊息已被編輯
	EvtMessageEdited EventType = "message_edited"
	// EvtMessageDeletedForMe 訊息被刪除（僅自己可見）
	EvtMessageDeletedForMe EventType = "message_deleted_for_me"
	// EvtChatArchiveChanged 聊天歸檔狀態變更（來自其他裝置或 WhatsApp 自動取消歸檔）
	EvtChatArchiveChanged EventType = "chat_archive_changed"
	// EvtChatArchiveBatch 批次聊天歸檔狀態同步（重連時 full sync）
	EvtChatArchiveBatch EventType = "chat_archive_batch"
	// EvtMediaDownloaded 媒體下載完成（歷史同步非同步下載）
	EvtMediaDownloaded EventType = "media_downloaded"
)

// Event Connector 發送給主服務的事件
type Event struct {
	ID          string          `json:"id"`
	Type        EventType       `json:"type"`
	ConnectorID string          `json:"connector_id"`
	AccountID   uint            `json:"account_id,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Timestamp   int64           `json:"timestamp"`
}

// NewEvent 建立新事件
func NewEvent(evtType EventType, connectorID string, accountID uint, payload interface{}) (*Event, error) {
	var payloadBytes json.RawMessage
	if payload != nil {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("序列化 payload 失敗: %w", err)
		}
	}

	return &Event{
		ID:          uuid.New().String(),
		Type:        evtType,
		ConnectorID: connectorID,
		AccountID:   accountID,
		Payload:     payloadBytes,
		Timestamp:   time.Now().UnixMilli(),
	}, nil
}

// ParsePayload 解析 payload 到指定結構
func (e *Event) ParsePayload(v interface{}) error {
	if e.Payload == nil {
		return nil
	}
	return json.Unmarshal(e.Payload, v)
}

// --- Payload 結構定義 ---

// MessageReceivedPayload 收到訊息的 payload
type MessageReceivedPayload struct {
	MessageID   string `json:"message_id"`
	ChatJID     string `json:"chat_jid"`
	SenderJID   string `json:"sender_jid"`
	SenderName  string `json:"sender_name,omitempty"`
	Content     string `json:"content"`
	ContentType string `json:"content_type"` // text, image, video, audio, document, sticker
	MediaURL    string `json:"media_url,omitempty"`
	Timestamp   int64  `json:"timestamp"`
	IsGroup     bool   `json:"is_group"`
	IsFromMe    bool   `json:"is_from_me"`           // 是否為自己發送的訊息
	IsHistory   bool   `json:"is_history,omitempty"` // 是否為歷史同步訊息
	// 引用訊息資訊
	QuotedMessageID string `json:"quoted_message_id,omitempty"`
	QuotedContent   string `json:"quoted_content,omitempty"`
	// LID ↔ PhoneJID 映射支援
	ChatAltJID      string `json:"chat_alt_jid,omitempty"`      // 聊天室替代 JID (原始 LID)
	SenderAltJID    string `json:"sender_alt_jid,omitempty"`    // 發送者替代 JID (SenderAlt)
	RecipientAltJID string `json:"recipient_alt_jid,omitempty"` // 接收者替代 JID (RecipientAlt，僅 DM)
	AddressingMode  string `json:"addressing_mode,omitempty"`   // 定址模式: "pn" 或 "lid"
	// 管理員/Agent 發送標記
	SentByAdminID *uint  `json:"sent_by_admin_id,omitempty"` // 管理員/Agent 發送時記錄 ID
	SenderType    string `json:"sender_type,omitempty"`       // "admin" | "agent"
}

// MediaDownloadedPayload 媒體下載完成的 payload
type MediaDownloadedPayload struct {
	MessageID string `json:"message_id"`
	ChatJID   string `json:"chat_jid"`
	MediaURL  string `json:"media_url"`
}

// MessageSentPayload 訊息已發送的 payload
type MessageSentPayload struct {
	CommandID string `json:"command_id"` // 對應的命令 ID
	MessageID string `json:"message_id"`
	ChatJID   string `json:"chat_jid"`
	Timestamp int64  `json:"timestamp"`
}

// ReceiptPayload 訊息回執的 payload
type ReceiptPayload struct {
	MessageID   string `json:"message_id"`
	ChatJID     string `json:"chat_jid"`
	ReceiptType string `json:"receipt_type"` // delivered, read, read-self, played, played-self
	IsFromMe    bool   `json:"is_from_me"`
	Timestamp   int64  `json:"timestamp"`
}

// ConnectedPayload 帳號已連線的 payload
type ConnectedPayload struct {
	PhoneNumber  string `json:"phone_number"`
	PushName     string `json:"push_name,omitempty"`
	Platform     string `json:"platform,omitempty"`
	BusinessName string `json:"business_name,omitempty"`
	DeviceID     string `json:"device_id,omitempty"` // whatsmeow 的 device JID
}

// DisconnectedPayload 帳號已斷線的 payload
type DisconnectedPayload struct {
	Reason string `json:"reason,omitempty"`
}

// LoggedOutPayload 帳號已登出的 payload
type LoggedOutPayload struct {
	Reason string `json:"reason,omitempty"`
}

// QRCodePayload QR Code 的 payload
type QRCodePayload struct {
	SessionID string `json:"session_id"`
	QRCode    string `json:"qr_code"` // base64 編碼的圖片或原始字串
	ExpiresAt int64  `json:"expires_at"`
}

// PairingCodePayload 配對碼的 payload
type PairingCodePayload struct {
	SessionID   string `json:"session_id"`
	PairingCode string `json:"pairing_code"`
	ExpiresAt   int64  `json:"expires_at"`
}

// LoginSuccessPayload 登入成功的 payload
type LoginSuccessPayload struct {
	SessionID    string `json:"session_id"`
	JID          string `json:"jid"`
	PhoneNumber  string `json:"phone_number"`
	PushName     string `json:"push_name,omitempty"`
	Platform     string `json:"platform,omitempty"`
	BusinessName string `json:"business_name,omitempty"`
}

// LoginFailedPayload 登入失敗的 payload
type LoginFailedPayload struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason"`
	Code      string `json:"code,omitempty"`
}

// LoginCancelledPayload 登入已取消的 payload
type LoginCancelledPayload struct {
	SessionID string `json:"session_id"`
}

// SyncProgressPayload 同步進度的 payload
type SyncProgressPayload struct {
	SyncType    string `json:"sync_type"` // chats, history, contacts
	Current     int    `json:"current"`
	Total       int    `json:"total"`
	Description string `json:"description,omitempty"`
}

// SyncCompletePayload 同步完成的 payload
type SyncCompletePayload struct {
	SyncType string `json:"sync_type"`
	Count    int    `json:"count"`
}

// CommandAckPayload 命令確認的 payload
type CommandAckPayload struct {
	CommandID string `json:"command_id"`
}

// CommandErrorPayload 命令錯誤的 payload
type CommandErrorPayload struct {
	CommandID string `json:"command_id"`
	Error     string `json:"error"`
	Code      string `json:"code,omitempty"`
}

// HeartbeatPayload 心跳的 payload
type HeartbeatPayload struct {
	AccountCount     int          `json:"account_count"`                // 目前管理的帳號數量
	AccountIDs       []uint       `json:"account_ids"`                  // 目前管理的帳號 ID 列表
	Uptime           int64        `json:"uptime"`                       // 運行時間（秒）
	StartTime        int64        `json:"start_time"`                   // 啟動時間（Unix 秒）
	MemoryMB         int          `json:"memory_mb"`                    // 記憶體使用量（MB）
	Version          string       `json:"version"`                      // Connector 版本
	EventWorkerStats map[uint]int `json:"event_worker_stats,omitempty"` // 各帳號 event worker queue depth
}

// ProfileUpdatedPayload 帳號資料已更新的 payload
type ProfileUpdatedPayload struct {
	Avatar    string `json:"avatar,omitempty"`     // 頭像 URL
	AvatarID  string `json:"avatar_id,omitempty"`  // 頭像 ID（用於快取判斷）
	PushName  string `json:"push_name,omitempty"`  // 暱稱
	FullName  string `json:"full_name,omitempty"`  // 完整名稱
	FirstName         string `json:"first_name,omitempty"`          // 名字
	KeepChatsArchived *bool  `json:"keep_chats_archived,omitempty"` // 保持對話封存（nil=未取得）
}

// GroupsSyncPayload 群組同步的 payload
type GroupsSyncPayload struct {
	Groups []GroupInfo `json:"groups"`
}

// GroupInfo 群組資訊
type GroupInfo struct {
	JID  string `json:"jid"`  // 群組 JID
	Name string `json:"name"` // 群組名稱
}

// ChatsUpdatedPayload Chat 列表更新的 payload
type ChatsUpdatedPayload struct {
	Chats []ChatInfo `json:"chats"`
}

// ChatInfo 聊天資訊（用於同步名稱和頭像）
type ChatInfo struct {
	JID     string `json:"jid"`
	Name    string `json:"name"`
	IsGroup bool   `json:"is_group"`
	Avatar  string `json:"avatar,omitempty"`
}

// MessageRevokedPayload 訊息被撤回的 payload
type MessageRevokedPayload struct {
	MessageID string `json:"message_id"`           // 被撤回的訊息 ID
	ChatJID   string `json:"chat_jid"`             // 聊天 JID
	SenderJID string `json:"sender_jid,omitempty"` // 撤回者 JID
	IsFromMe  bool   `json:"is_from_me"`           // 是否是自己撤回
	Timestamp int64  `json:"timestamp"`            // 撤回時間
}

// MessageEditedPayload 訊息被編輯的 payload
type MessageEditedPayload struct {
	MessageID  string `json:"message_id"`            // 被編輯的訊息 ID
	ChatJID    string `json:"chat_jid"`              // 聊天 JID
	NewContent string `json:"new_content"`           // 編輯後的新內容
	SenderJID  string `json:"sender_jid,omitempty"`  // 編輯者 JID
	IsFromMe   bool   `json:"is_from_me"`            // 是否是自己編輯
	Timestamp  int64  `json:"timestamp"`             // 編輯時間
}

// MessageDeletedForMePayload 訊息被刪除（僅自己）的 payload
type MessageDeletedForMePayload struct {
	MessageID string `json:"message_id"`            // 被刪除的訊息 ID
	ChatJID   string `json:"chat_jid"`              // 聊天 JID
	SenderJID string `json:"sender_jid,omitempty"`  // 發送者 JID
	IsFromMe  bool   `json:"is_from_me"`            // 是否是自己發送的訊息
	Timestamp int64  `json:"timestamp"`             // 刪除時間
}

// ChatArchiveChangedPayload 聊天歸檔狀態變更的 payload
type ChatArchiveChangedPayload struct {
	ChatJID   string `json:"chat_jid"`   // 聊天 JID
	Archived  bool   `json:"archived"`   // 是否已歸檔
	Timestamp int64  `json:"timestamp"`  // 變更時間
}

// ChatArchiveItem 單一聊天歸檔狀態
type ChatArchiveItem struct {
	ChatJID  string `json:"chat_jid"`
	Archived bool   `json:"archived"`
}

// ChatArchiveBatchPayload 批次聊天歸檔狀態同步的 payload（重連時 full sync 使用）
type ChatArchiveBatchPayload struct {
	Items []ChatArchiveItem `json:"items"`
}

// --- Redis Stream 常數 ---

const (
	// EventStreamName 事件 Stream 名稱
	EventStreamName = "wa:events"
	// EventConsumerGroup 事件消費者群組
	EventConsumerGroup = "api-consumer-group"
	// EventConsumerName 事件消費者名稱前綴
	EventConsumerName = "api-consumer"

	// PriorityEventStreamName 高優先級事件 Stream 名稱（登入、連線狀態）
	PriorityEventStreamName = "wa:priority-events"
	// PriorityEventConsumerGroup 高優先級事件消費者群組
	PriorityEventConsumerGroup = "api-priority-consumer-group"

	// MessageEventStreamName 訊息事件 Stream 名稱
	MessageEventStreamName = "wa:message-events"
	// MessageEventConsumerGroup 訊息事件消費者群組
	MessageEventConsumerGroup = "api-message-consumer-group"

	// Deprecated: 保留舊名稱相容性
	LoginEventStreamName    = PriorityEventStreamName
	LoginEventConsumerGroup = PriorityEventConsumerGroup
)

// IsMessageEvent 判斷是否為訊息事件
func IsMessageEvent(eventType EventType) bool {
	return eventType == EvtMessageReceived
}

// IsPriorityEvent 判斷是否為高優先級事件（登入、連線狀態、命令響應）
func IsPriorityEvent(eventType EventType) bool {
	switch eventType {
	// 登入相關
	case EvtPairingCode, EvtQRCode, EvtLoginSuccess, EvtLoginFailed, EvtLoginCancelled:
		return true
	// 連線狀態
	case EvtConnected, EvtDisconnected, EvtLoggedOut:
		return true
	// 命令響應（用戶主動操作的回饋，不應被慢事件阻塞）
	case EvtCommandAck, EvtCommandError, EvtManageCommandAck, EvtManageCommandError:
		return true
	default:
		return false
	}
}

// IsLoginEvent 判斷是否為登入相關事件（高優先級）
// Deprecated: 使用 IsPriorityEvent
func IsLoginEvent(eventType EventType) bool {
	return IsPriorityEvent(eventType)
}

// --- 路由表常數 ---

const (
	// RoutingHashKey 路由表 Redis Hash Key
	RoutingHashKey = "wa:routing"
	// ConnectorsSetKey Connector 集合 Redis Set Key
	ConnectorsSetKey = "wa:connectors"
	// ConnectorHeartbeatPrefix Connector 心跳 Key 前綴
	ConnectorHeartbeatPrefix = "wa:connector:heartbeat:"
)

// GetRoutingField 取得路由表中帳號的 field 名稱
func GetRoutingField(accountID uint) string {
	return fmt.Sprintf("account:%d", accountID)
}

// GetConnectorHeartbeatKey 取得 Connector 心跳 Key
func GetConnectorHeartbeatKey(connectorID string) string {
	return ConnectorHeartbeatPrefix + connectorID
}

// ConnectorInfoPrefix Connector 資訊 Key 前綴
const ConnectorInfoPrefix = "wa:connector:info:"

// GetConnectorInfoKey 取得 Connector 資訊 Key
func GetConnectorInfoKey(connectorID string) string {
	return ConnectorInfoPrefix + connectorID
}
