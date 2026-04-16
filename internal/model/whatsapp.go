package model

import (
	"time"
)

// WhatsAppAccount WhatsApp账号信息
type WhatsAppAccount struct {
	ID                uint         `gorm:"primaryKey" json:"id"`
	PhoneNumber       string       `gorm:"size:20;uniqueIndex" json:"phone_number"`
	DeviceID          string       `gorm:"size:100" json:"device_id"`
	SessionData       []byte       `gorm:"type:bytea" json:"-"`
	Status            string       `gorm:"size:20;default:'disconnected'" json:"status"` // 裝置連線狀態: connected, disconnected, connecting, logged_out
	DisconnectReason  string       `gorm:"column:logged_out_reason;size:100" json:"disconnect_reason,omitempty"`
	DisconnectedAt    *time.Time   `gorm:"column:logged_out_at" json:"disconnected_at,omitempty"`
	IsOnline          bool         `gorm:"default:false" json:"is_online"` // 帳號主人是否在線（由 OwnerLastActive 驅動，5 分鐘無活動自動離線）
	LastSeen          time.Time    `json:"last_seen"`                      // 最后在线时间
	LastConnected     time.Time    `json:"last_connected"`
	LastSyncAt        *time.Time   `json:"last_sync_at"`                                                  // 上次同步時間（用於避免短時間重啟重複同步）
	Avatar            string       `gorm:"size:500" json:"avatar"`                                        // 用户头像URL
	AvatarID          string       `gorm:"size:100" json:"avatar_id"`                                     // 头像ID
	PushName          string       `gorm:"size:100" json:"push_name"`                                     // WhatsApp显示名称(最高优先级)
	FullName          string       `gorm:"size:100" json:"full_name"`                                     // 联系人完整名称(备用)
	FirstName         string       `gorm:"size:50" json:"first_name"`                                     // 联系人名字(备用)
	ChannelID         *uint        `gorm:"column:channel_id;index" json:"channel_id,omitempty"`           // 关联的渠道ID
	ChannelSource     string       `gorm:"column:channel_source;size:20" json:"channel_source,omitempty"` // 渠道来源：link/none
	ConnectorID       string       `gorm:"size:50;index" json:"connector_id,omitempty"`                   // 綁定的 Connector ID
	AIAnalysisEnabled bool         `gorm:"default:true" json:"ai_analysis_enabled"`                       // AI 聊天分析開關
	KeepChatsArchived bool         `gorm:"default:false" json:"keep_chats_archived"`                      // 保持對話封存設定
	Platform          string       `gorm:"size:20" json:"platform,omitempty"`                             // whatsmeow 回報的平台：smba, smbi, android, iphone
	BusinessName      string       `gorm:"size:100" json:"business_name,omitempty"`                       // WhatsApp Business 帳號名稱
	OwnerLastActive   *time.Time   `json:"owner_last_active,omitempty"`                                   // 帳號主人最後活躍時間
	MessageCount      int64        `gorm:"default:0" json:"message_count"`
	AdminStatus       string       `gorm:"size:20;default:'active'" json:"admin_status"`
	Tags              []AccountTag `gorm:"many2many:whatsapp_account_tags;joinForeignKey:account_id;joinReferences:tag_id" json:"tags,omitempty"` // 账号标签
	ChannelName       string       `gorm:"-" json:"channel_name,omitempty"`                                                                       // 渠道名称（关联查询后填充）
	ConnectorName     string       `gorm:"-" json:"connector_name,omitempty"`                                                                     // Connector 名稱（關聯查詢後填充）
	WorkgroupName     string       `gorm:"-" json:"workgroup_name,omitempty"`                                                                     // 工作組名稱（關聯查詢後填充）
	Source            *AccountSource `gorm:"-" json:"source"`                                                                                     // 帳號來源歸因（關聯查詢後填充）

	// 裂变推荐相关字段
	ReferralCode         *string    `gorm:"size:12;uniqueIndex" json:"referral_code,omitempty"`
	ReferralCount        int        `gorm:"-" json:"referral_count"` // 计算字段，不存储
	ReferredByAccountID  *uint      `json:"referred_by_account_id,omitempty"`
	ReferralOperatorID   *uint      `json:"referral_operator_id,omitempty"`
	ReferralRegisteredAt *time.Time `json:"referral_registered_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetDisplayName 获取显示名称(优先级: PushName > FullName > PhoneNumber)
func (a *WhatsAppAccount) GetDisplayName() string {
	if a.PushName != "" {
		return a.PushName
	}
	if a.FullName != "" {
		return a.FullName
	}
	return a.PhoneNumber
}

// IsBusiness 判斷是否為 WhatsApp Business 帳號
func (a *WhatsAppAccount) IsBusiness() bool {
	return a.Platform == "smba" || a.Platform == "smbi"
}

// AccountSource 帳號來源歸因
type AccountSource struct {
	SourceType         string     `json:"source_type"`                             // "referral" | "channel"
	ReferralCode       string     `json:"referral_code,omitempty"`                 // 裂變推薦碼
	SourceAccountID    *uint      `json:"source_account_id,omitempty"`             // 推薦來源帳號 ID
	SourceAccountPhone string     `json:"source_account_phone,omitempty"`          // 推薦來源帳號電話
	SourceAccountName  string     `json:"source_account_name,omitempty"`           // 推薦來源帳號名稱
	SourceAgentID      *uint      `json:"source_agent_id,omitempty"`               // 裂變來源組員 ID
	SourceAgentName    string     `json:"source_agent_name,omitempty"`             // 裂變來源組員名稱
	ChannelSourceID    *uint      `json:"channel_source_id,omitempty"`             // 渠道 ID
	ChannelSourceKey   string     `json:"channel_source_key,omitempty"`            // 渠道代碼
	ChannelSourceName  string     `json:"channel_source_name,omitempty"`           // 渠道名稱
	Platform           string     `json:"platform,omitempty"`                      // 渠道平台
	CapturedAt         *time.Time `json:"captured_at,omitempty"`                   // 來源捕獲時間
	CaptureMethod      string     `json:"capture_method,omitempty"`                // 捕獲方式
}

// WhatsAppContact WhatsApp联系人
type WhatsAppContact struct {
	ID           uint         `gorm:"primaryKey" json:"id"`
	AccountID    uint         `gorm:"uniqueIndex:idx_contact_account_jid" json:"account_id"`
	JID          string       `gorm:"column:jid;size:100;uniqueIndex:idx_contact_account_jid" json:"jid"`
	Phone        string       `gorm:"size:20" json:"phone"`
	PushName     string       `gorm:"size:100" json:"push_name"`     // 用戶自己設定的顯示名稱
	FirstName    string       `gorm:"size:100" json:"first_name"`    // 名字
	FullName     string       `gorm:"size:100" json:"full_name"`     // 完整名稱
	BusinessName string       `gorm:"size:100" json:"business_name"` // 商業帳號名稱
	Avatar       string       `gorm:"size:255" json:"avatar"`
	AvatarID     string       `gorm:"size:100" json:"avatar_id"`       // 頭像ID（用於差異更新）
	Status       string       `gorm:"size:20" json:"status"`           // 账号状态：connected, disconnected, connecting
	IsOnline     bool         `gorm:"default:false" json:"is_online"`  // 在线状态
	LastSeen     time.Time    `json:"last_seen"`                       // 最后在线时间
	MessageCount int64        `gorm:"-" json:"message_count"`          // 消息数量（从User传递过来，不在数据库中）
	Tags         []AccountTag `gorm:"-" json:"tags,omitempty"`         // 标签（从WhatsAppAccount传递过来，不在数据库中）
	ChannelID    *uint        `gorm:"-" json:"channel_id,omitempty"`   // 渠道ID（从User传递过来，不在数据库中）
	ChannelName  string       `gorm:"-" json:"channel_name,omitempty"` // 渠道名称（从User传递过来，不在数据库中）
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// WhatsAppChat 聊天会话
type WhatsAppChat struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	AccountID    uint      `gorm:"index;index:idx_account_jid,priority:1" json:"account_id"`
	JID          string    `gorm:"column:jid;size:100;index;index:idx_account_jid,priority:2" json:"jid"`
	PhoneJID     string    `gorm:"column:phone_jid;size:100" json:"phone_jid,omitempty"`
	Name         string    `gorm:"size:255" json:"name"`
	Avatar       string    `gorm:"size:500" json:"avatar"`    // 联系人头像URL
	AvatarID     string    `gorm:"size:100" json:"avatar_id"` // 頭像ID（用於差異更新）
	LastMessage  string    `gorm:"size:255" json:"last_message"`
	LastTime     time.Time `json:"last_time"`
	UnreadCount  int       `gorm:"default:0" json:"unread_count"`
	IsGroup      bool      `gorm:"default:false" json:"is_group"`
	Participants string    `gorm:"size:1000" json:"participants"`

	// 归档相关字段
	Archived   bool       `gorm:"default:false;index" json:"archived"` // 是否已归档
	ArchivedAt *time.Time `json:"archived_at,omitempty"`               // 归档时间

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ChatCounts struct {
	Total      int64 `json:"total"`
	Archived   int64 `json:"archived"`
	Unarchived int64 `json:"unarchived"`
}

// WhatsAppMessage 消息记录
type WhatsAppMessage struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	AccountID       uint       `gorm:"index" json:"account_id"`
	ChatID          uint       `gorm:"index" json:"chat_id"`
	MessageID       string     `gorm:"size:100;index" json:"message_id"`
	FromJID         string     `gorm:"size:100;column:from_j_id" json:"from_jid"`
	ToJID           string     `gorm:"size:100;column:to_j_id" json:"to_jid"`
	Content         string     `gorm:"type:text" json:"content"`
	OriginalText    string     `gorm:"type:text" json:"original_text"` // 翻译前的原文（用于自动翻译发送）
	Type            string     `gorm:"size:20" json:"type"`            // text, image, video, audio, document, sticker, location, contact, voice_note, reaction, live_location, etc.
	MediaURL        string     `gorm:"size:255" json:"media_url"`
	MessageMetadata []byte     `gorm:"type:jsonb" json:"message_metadata,omitempty"` // 消息元数据，JSON格式存储不同类型消息的特定信息
	Timestamp       time.Time  `json:"timestamp"`
	IsFromMe        bool       `json:"is_from_me"`
	IsRead          bool       `gorm:"default:false" json:"is_read"`
	SendStatus      string     `gorm:"size:20;default:'sent'" json:"send_status"` // pending, sent, delivered, read, failed
	DeletedAt       *time.Time `gorm:"index" json:"deleted_at,omitempty"`         // 软删除时间戳
	DeletedBy       string     `gorm:"size:100" json:"deleted_by,omitempty"`      // 删除操作者
	IsRevoked       bool       `gorm:"default:false;index" json:"is_revoked"`     // 是否已撤销
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`                      // 撤销时间戳
	RevokedBy       string     `gorm:"size:100" json:"revoked_by,omitempty"`      // 撤回操作者（空=裝置端）
	IsEdited        bool       `gorm:"default:false" json:"is_edited"`            // 是否已編輯
	EditedAt        *time.Time `json:"edited_at,omitempty"`                       // 編輯時間戳
	SentByAdminID   *uint      `gorm:"index" json:"sent_by_admin_id,omitempty"`   // 管理員發送時記錄 admin ID，NULL 表示用戶自己發送
	SenderType      string     `gorm:"size:10" json:"sender_type,omitempty"`      // "admin" | "agent"，空字串表示用戶自己發送
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	// 非 DB 欄位：LID ↔ PhoneJID 映射（用於 WebSocket 廣播）
	FromPhoneJID string `gorm:"-" json:"from_phone_jid,omitempty"`
	ToPhoneJID   string `gorm:"-" json:"to_phone_jid,omitempty"`
}

// TableName 设置表名
func (WhatsAppAccount) TableName() string {
	return "whatsapp_accounts"
}

func (WhatsAppContact) TableName() string {
	return "whatsapp_contacts"
}

func (WhatsAppChat) TableName() string {
	return "whatsapp_chats"
}

func (WhatsAppMessage) TableName() string {
	return "whatsapp_messages"
}

// MessageSender 消息发送者信息
type MessageSender struct {
	JID    string `json:"jid"`
	Name   string `json:"name"`
	Phone  string `json:"phone"`
	Avatar string `json:"avatar"`
}

// MessageWithSender 包含发送者信息的消息
type MessageWithSender struct {
	WhatsAppMessage
	Sender         *MessageSender `json:"sender"`
	TranslatedText string         `json:"translated_text,omitempty"` // 从translation_cache查询的翻译结果
}
