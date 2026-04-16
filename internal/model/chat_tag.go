package model

import "time"

// ChatTag 聊天室標籤模型
type ChatTag struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ChatID    string    `gorm:"size:100;uniqueIndex:idx_chat_tag_unique" json:"chat_id"`
	AccountID uint      `gorm:"uniqueIndex:idx_chat_tag_unique" json:"account_id"`
	Tag       string    `gorm:"size:50;uniqueIndex:idx_chat_tag_unique" json:"tag"`
	Category  string    `gorm:"size:20;uniqueIndex:idx_chat_tag_unique" json:"category"` // "relationship" | "topic" | "sensitive_word"
	Source    string    `gorm:"size:20" json:"source"`                                   // "sensitive_word" | "ai"
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (ChatTag) TableName() string {
	return "chat_tags"
}
