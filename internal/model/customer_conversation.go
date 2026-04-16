package model

import (
	"time"
)

// CustomerConversation 客户咨询对话记录表
type CustomerConversation struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	SessionID        string    `gorm:"size:100;index;not null" json:"session_id"` // 会话ID（同一用户的多轮对话）
	UserIdentifier   string    `gorm:"size:100;index" json:"user_identifier"`     // 用户标识（手机号、邮箱等）
	UserMessage      string    `gorm:"type:text" json:"user_message"`             // 用户消息
	BotReply         string    `gorm:"type:text" json:"bot_reply"`                // 机器人回复
	MatchedKeywordID *uint     `gorm:"index" json:"matched_keyword_id,omitempty"` // 匹配到的关键词ID（仅用于记录，无外键约束）
	IsMatched        bool      `gorm:"default:false;not null" json:"is_matched"`  // 是否匹配到关键词
	IsAdminReply     bool      `gorm:"default:false" json:"is_admin_reply"`       // 是否管理员回复
	AdminID          *uint     `json:"admin_id,omitempty"`                        // 管理员ID
	AdminName        string    `gorm:"size:100" json:"admin_name,omitempty"`      // 管理员名称
	NeedsHuman       bool      `gorm:"default:false" json:"needs_human"`          // 是否需要人工介入
	IPAddress        string    `gorm:"size:50" json:"ip_address,omitempty"`       // 用户IP地址
	UserAgent        string    `gorm:"size:500" json:"user_agent,omitempty"`      // 用户代理
	CreatedAt        time.Time `json:"created_at"`
}

// TableName 设置表名
func (CustomerConversation) TableName() string {
	return "customer_conversations"
}

// CustomerConversationStats 客户咨询统计
type CustomerConversationStats struct {
	TotalConversations int64   `json:"total_conversations"` // 总对话数
	TotalUsers         int64   `json:"total_users"`         // 咨询人数
	MatchedRate        float64 `json:"matched_rate"`        // 匹配率
	TodayConversations int64   `json:"today_conversations"` // 今日对话数
	TodayUsers         int64   `json:"today_users"`         // 今日咨询人数
}

// KeywordMatchStats 关键词匹配统计
type KeywordMatchStats struct {
	KeywordID   uint    `json:"keyword_id"`
	KeywordName string  `json:"keyword_name"`
	MatchCount  int64   `json:"match_count"`
	Percentage  float64 `json:"percentage"`
}

// CustomerSession 客户会话（用于活跃会话列表）
type CustomerSession struct {
	SessionID      string    `json:"session_id"`
	UserIdentifier string    `json:"user_identifier"`
	LastMessage    string    `json:"last_message"`
	LastTime       time.Time `json:"last_time"`
	MessageCount   int64     `json:"message_count"`
	HasAdminReply  bool      `json:"has_admin_reply"` // 是否已有管理员回复
	NeedsHuman     bool      `json:"needs_human"`     // 是否需要人工介入
}
