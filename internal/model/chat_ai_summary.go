package model

import "time"

// ChatAISummary 聊天室 AI 分析摘要（滾動摘要 + 水位線）
type ChatAISummary struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	ChatID        uint      `gorm:"uniqueIndex:idx_chat_summary_unique;not null" json:"chat_id"`
	AccountID     uint      `gorm:"uniqueIndex:idx_chat_summary_unique;not null" json:"account_id"`
	Summary       string    `gorm:"type:text" json:"summary"`                  // LLM 產出的摘要
	LastMessageID uint      `gorm:"not null;default:0" json:"last_message_id"` // 水位線：上次分析到的訊息 ID
	LastAnalyzed  time.Time `json:"last_analyzed"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (ChatAISummary) TableName() string {
	return "chat_ai_summaries"
}
