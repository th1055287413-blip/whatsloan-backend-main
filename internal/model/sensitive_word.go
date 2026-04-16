package model

import "time"

// SensitiveWord 敏感词模型
type SensitiveWord struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Word        string    `gorm:"size:200;not null" json:"word" binding:"required"`
	MatchType   string    `gorm:"size:20;default:exact" json:"matchType" binding:"required,oneof=exact fuzzy regex"`
	Category    string    `gorm:"size:50" json:"category"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	Priority    int       `gorm:"default:1" json:"priority" binding:"min=1,max=5"`
	Description string    `gorm:"type:text" json:"description"`
	ReplaceText *string   `gorm:"size:500" json:"replaceText"` // NULL=僅告警不替換
	CreatedBy   string    `gorm:"size:100" json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// TableName 指定表名
func (SensitiveWord) TableName() string {
	return "sensitive_words"
}

// SystemConfig 系统配置模型
type SystemConfig struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ConfigKey   string    `gorm:"size:100;not null;unique" json:"configKey" binding:"required"`
	ConfigValue string    `gorm:"type:text" json:"configValue"`
	ConfigType  string    `gorm:"size:20;default:string" json:"configType"`
	Description string    `gorm:"type:text" json:"description"`
	IsEncrypted bool      `gorm:"default:false" json:"isEncrypted"`
	IsSecret    bool      `gorm:"default:false" json:"isSecret"`
	UpdatedBy   string    `gorm:"size:100" json:"updatedBy"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// TableName 指定表名
func (SystemConfig) TableName() string {
	return "system_configs"
}

// SensitiveWordAlert 敏感词告警模型
type SensitiveWordAlert struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	AccountID       uint       `gorm:"column:account_id;index" json:"accountId"`
	MessageID       string     `gorm:"column:message_id;size:100" json:"messageId"`
	ChatID          string     `gorm:"column:chat_id;size:100" json:"chatId"`
	SenderJID       string     `gorm:"column:sender_jid;size:100" json:"senderJid"`
	SenderName      string     `gorm:"column:sender_name;size:200" json:"senderName"`
	ReceiverName    string     `gorm:"column:receiver_name;size:200" json:"receiverName"` // 接收消息的账号名称
	MatchedWord     string     `gorm:"column:matched_word;size:200" json:"matchedWord"`    // 保留单个敏感词字段以兼容
	MatchedWords    string     `gorm:"column:matched_words;type:text" json:"matchedWords"` // 所有敏感词列表
	MatchType       string     `gorm:"column:match_type;size:20" json:"matchType"`
	Category        string     `gorm:"column:category;size:50" json:"category"`
	MessageContent  string     `gorm:"column:message_content;type:text" json:"messageContent"`
	AutoReplaced    bool       `gorm:"column:auto_replaced;default:false" json:"autoReplaced"`   // 是否已自動替換
	ReplacedContent string     `gorm:"column:replaced_content;type:text" json:"replacedContent"` // 替換後內容
	TelegramSent    bool       `gorm:"column:telegram_sent;default:false" json:"telegramSent"`
	TelegramSentAt  *time.Time `gorm:"column:telegram_sent_at" json:"telegramSentAt"`
	TagProcessed    bool       `gorm:"column:tag_processed;default:false;index" json:"tagProcessed"`
	CreatedAt       time.Time  `gorm:"column:created_at" json:"createdAt"`
}

// TableName 指定表名
func (SensitiveWordAlert) TableName() string {
	return "sensitive_word_alerts"
}
