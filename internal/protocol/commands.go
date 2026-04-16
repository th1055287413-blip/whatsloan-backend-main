package protocol

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CommandType 命令類型
type CommandType string

const (
	// CmdSendMessage 發送訊息
	CmdSendMessage CommandType = "send_message"
	// CmdSendMedia 發送媒體訊息
	CmdSendMedia CommandType = "send_media"
	// CmdConnect 連接帳號
	CmdConnect CommandType = "connect"
	// CmdDisconnect 斷開帳號
	CmdDisconnect CommandType = "disconnect"
	// CmdSyncChats 同步聊天列表
	CmdSyncChats CommandType = "sync_chats"
	// CmdSyncHistory 同步歷史訊息
	CmdSyncHistory CommandType = "sync_history"
	// CmdSyncContacts 同步聯絡人
	CmdSyncContacts CommandType = "sync_contacts"
	// CmdGetQRCode 獲取 QR Code
	CmdGetQRCode CommandType = "get_qr_code"
	// CmdGetPairingCode 獲取配對碼
	CmdGetPairingCode CommandType = "get_pairing_code"
	// CmdCancelLogin 取消登入會話
	CmdCancelLogin CommandType = "cancel_login"
	// CmdRevokeMessage 撤銷訊息
	CmdRevokeMessage CommandType = "revoke_message"
	// CmdUpdateProfile 更新帳號資料（頭像、暱稱等）
	CmdUpdateProfile CommandType = "update_profile"
	// CmdBindAccount 綁定帳號 ID（登入成功後，將 sessionID 對應的 client 綁定到實際帳號 ID）
	CmdBindAccount CommandType = "bind_account"
	// CmdArchiveChat 歸檔/取消歸檔聊天
	CmdArchiveChat CommandType = "archive_chat"
	// CmdDeleteMessageForMe 刪除訊息（僅自己，同步到所有 linked devices）
	CmdDeleteMessageForMe CommandType = "delete_message_for_me"
	// CmdUpdateSettings 推送裝置設定到 WhatsApp（push_name 等）
	CmdUpdateSettings CommandType = "update_settings"
)

// ManageCommandType 管理命令類型（API → Connector 服務的 Pool 層級操作）
type ManageCommandType string

const (
	ManageStartConnector   ManageCommandType = "manage_start_connector"
	ManageStopConnector    ManageCommandType = "manage_stop_connector"
	ManageRestartConnector ManageCommandType = "manage_restart_connector"
)

// ManageCommand API 服務發送給 Connector 服務的管理命令
type ManageCommand struct {
	ID          string            `json:"id"`
	Type        ManageCommandType `json:"type"`
	ConnectorID string            `json:"connector_id"`
	Payload     json.RawMessage   `json:"payload,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// NewManageCommand 建立新管理命令
func NewManageCommand(cmdType ManageCommandType, connectorID string) *ManageCommand {
	return &ManageCommand{
		ID:          uuid.New().String(),
		Type:        cmdType,
		ConnectorID: connectorID,
		CreatedAt:   time.Now(),
	}
}

// Command 主服務發送給 Connector 的命令
type Command struct {
	ID        string          `json:"id"`
	Type      CommandType     `json:"type"`
	AccountID uint            `json:"account_id"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// NewCommand 建立新命令
func NewCommand(cmdType CommandType, accountID uint, payload interface{}) (*Command, error) {
	var payloadBytes json.RawMessage
	if payload != nil {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("序列化 payload 失敗: %w", err)
		}
	}

	return &Command{
		ID:        uuid.New().String(),
		Type:      cmdType,
		AccountID: accountID,
		Payload:   payloadBytes,
		CreatedAt: time.Now(),
	}, nil
}

// ParsePayload 解析 payload 到指定結構
func (c *Command) ParsePayload(v interface{}) error {
	if c.Payload == nil {
		return nil
	}
	return json.Unmarshal(c.Payload, v)
}

// --- Payload 結構定義 ---

// SendMessagePayload 發送訊息的 payload
type SendMessagePayload struct {
	ToJID   string `json:"to_jid"`
	Content string `json:"content"`
	// QuotedMessageID 引用訊息 ID（可選）
	QuotedMessageID string `json:"quoted_message_id,omitempty"`
	// SentByAdminID 管理員/Agent 發送時記錄 ID（可選）
	SentByAdminID *uint `json:"sent_by_admin_id,omitempty"`
	// SenderType 發送者類型：admin | agent
	SenderType string `json:"sender_type,omitempty"`
}

// SendMediaPayload 發送媒體訊息的 payload
type SendMediaPayload struct {
	ToJID     string `json:"to_jid"`
	MediaType string `json:"media_type"` // image, video, audio, document
	MediaURL  string `json:"media_url"`
	Caption   string `json:"caption,omitempty"`
	FileName  string `json:"file_name,omitempty"`
	// SentByAdminID 管理員/Agent 發送時記錄 ID（可選）
	SentByAdminID *uint `json:"sent_by_admin_id,omitempty"`
	// SenderType 發送者類型：admin | agent
	SenderType string `json:"sender_type,omitempty"`
}

// SyncHistoryPayload 同步歷史訊息的 payload
type SyncHistoryPayload struct {
	ChatJID    string `json:"chat_jid"`
	Count      int    `json:"count"`
	ChatIndex  int    `json:"chat_index,omitempty"`
	TotalChats int    `json:"total_chats,omitempty"`
}

// GetQRCodePayload 獲取 QR Code 的 payload
type GetQRCodePayload struct {
	SessionID string `json:"session_id"`
}

// GetPairingCodePayload 獲取配對碼的 payload
type GetPairingCodePayload struct {
	SessionID   string `json:"session_id"`
	PhoneNumber string `json:"phone_number"`
}

// CancelLoginPayload 取消登入的 payload
type CancelLoginPayload struct {
	SessionID string `json:"session_id"`
}

// RevokeMessagePayload 撤銷訊息的 payload
type RevokeMessagePayload struct {
	ChatJID   string `json:"chat_jid"`   // 聊天 JID
	MessageID string `json:"message_id"` // 訊息 ID（WhatsApp 訊息 ID）
}

// BindAccountPayload 綁定帳號的 payload
type BindAccountPayload struct {
	SessionID    string `json:"session_id"`     // 登入時的 session ID
	NewAccountID uint   `json:"new_account_id"` // 新建立的帳號 ID
}

// ArchiveChatPayload 歸檔聊天的 payload
type ArchiveChatPayload struct {
	ChatJID  string `json:"chat_jid"` // 聊天 JID
	Archive  bool   `json:"archive"`  // true=歸檔, false=取消歸檔
	ChatID   uint   `json:"chat_id"`  // 本地聊天 ID（用於更新 DB）
}

// UpdateSettingsPayload 推送裝置設定到 WhatsApp 的 payload
// 所有欄位皆為指標，nil 表示不更新
type UpdateSettingsPayload struct {
	PushName *string `json:"push_name,omitempty"`
}

// DeleteMessageForMePayload 刪除訊息（僅自己）的 payload
type DeleteMessageForMePayload struct {
	ChatJID          string `json:"chat_jid"`          // 聊天 JID
	MessageID        string `json:"message_id"`        // WhatsApp 訊息 ID
	IsFromMe         bool   `json:"is_from_me"`        // 是否為自己發送的訊息
	SenderJID        string `json:"sender_jid"`        // 發送者 JID（群組聊天時使用）
	MessageTimestamp int64  `json:"message_timestamp"` // 訊息時間戳（Unix 秒）
}

// --- Redis Stream 常數 ---

const (
	// CommandStreamPrefix 命令 Stream 前綴（舊格式，用於遷移）
	CommandStreamPrefix = "wa:cmd:"
	// CommandConsumerGroup 命令消費者群組（舊格式，用於遷移）
	CommandConsumerGroup = "connector-consumer-group"

	// ManageCommandStreamName 管理命令 Stream 名稱
	ManageCommandStreamName = "connector:manage"
	// ManageCommandConsumerGroup 管理命令消費者群組
	ManageCommandConsumerGroup = "connector-manage-consumer-group"

	// PriorityCommandStreamPrefix 高優先級命令 Stream 前綴（即時操作）
	PriorityCommandStreamPrefix = "wa:cmd:priority:"
	// BulkCommandStreamPrefix 低優先級命令 Stream 前綴（批量操作）
	BulkCommandStreamPrefix = "wa:cmd:bulk:"

	// PriorityCommandConsumerGroup 高優先級命令消費者群組
	PriorityCommandConsumerGroup = "connector-priority-consumer-group"
	// BulkCommandConsumerGroup 低優先級命令消費者群組
	BulkCommandConsumerGroup = "connector-bulk-consumer-group"
)

// GetCommandStreamName 取得指定 Connector 的命令 Stream 名稱（舊格式，用於遷移）
func GetCommandStreamName(connectorID string) string {
	return CommandStreamPrefix + connectorID
}

// GetPriorityCommandStreamName 取得高優先級命令 Stream 名稱
func GetPriorityCommandStreamName(connectorID string) string {
	return PriorityCommandStreamPrefix + connectorID
}

// GetBulkCommandStreamName 取得低優先級命令 Stream 名稱
func GetBulkCommandStreamName(connectorID string) string {
	return BulkCommandStreamPrefix + connectorID
}

// IsBulkCommand 判斷命令是否為低優先級批量操作
func IsBulkCommand(cmdType CommandType) bool {
	switch cmdType {
	case CmdSyncChats, CmdSyncHistory, CmdSyncContacts, CmdUpdateProfile:
		return true
	default:
		return false
	}
}
