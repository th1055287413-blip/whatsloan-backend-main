package model

import "time"

// AiTagDefinition AI 標籤定義（DB 驅動，動態組裝 prompt）
type AiTagDefinition struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Category    string    `gorm:"size:20;uniqueIndex:idx_ai_tag_def_unique;not null" json:"category"` // "relationship" | "topic"
	Key         string    `gorm:"size:50;uniqueIndex:idx_ai_tag_def_unique;not null" json:"key"`      // "customer", "daily" 等
	Label       string    `gorm:"size:50;not null" json:"label"`                                       // 顯示名稱（中文）
	Description string    `gorm:"type:text" json:"description"`                                        // 給 LLM 看的說明
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	SortOrder   int       `gorm:"default:0" json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (AiTagDefinition) TableName() string {
	return "ai_tag_definitions"
}
