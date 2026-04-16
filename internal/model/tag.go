package model

import (
	"time"
)

// TagType 标签类型
type TagType string

const (
	TagTypeSystem TagType = "system" // 系统标签
	TagTypeCustom TagType = "custom" // 自定义标签
)

// AccountTag 账号标签表
type AccountTag struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Name         string     `gorm:"size:50;uniqueIndex;not null" json:"name"`
	TagType      TagType    `gorm:"size:20;default:'custom'" json:"tag_type"`
	Color        string     `gorm:"size:7;not null" json:"color"`                    // 颜色代码，如 #FF5733
	Description  string     `gorm:"type:text" json:"description"`                    // 标签描述
	SourceKey    *string    `gorm:"size:50;uniqueIndex" json:"source_key,omitempty"` // 来源代码，用于 URL 参数匹配（如 ?source=FB_AD_2024）
	AccountCount int        `gorm:"default:0" json:"account_count"`                  // 关联账号数量
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

// WhatsAppAccountTag 账号-标签关联表（多对多）
type WhatsAppAccountTag struct {
	AccountID uint      `gorm:"primaryKey;autoIncrement:false" json:"account_id"`
	TagID     uint      `gorm:"primaryKey;autoIncrement:false" json:"tag_id"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName 设置表名
func (AccountTag) TableName() string {
	return "account_tags"
}

func (WhatsAppAccountTag) TableName() string {
	return "whatsapp_account_tags"
}

// AccountWithTags 带标签的账号信息
type AccountWithTags struct {
	WhatsAppAccount
	Tags []AccountTag `json:"tags,omitempty"`
}

// TagStatistics 标签统计信息
type TagStatistics struct {
	TagID               uint    `json:"tag_id"`
	TagName             string  `json:"tag_name"`
	TagColor            string  `json:"tag_color"`
	AccountCount        int     `json:"account_count"`
	Percentage          float64 `json:"percentage"`            // 占比
	OnlineCount         int     `json:"online_count"`          // 在线账号数
	ActiveCount         int     `json:"active_count"`          // 活跃账号数（最近7天有连接）
	AverageMessageCount float64 `json:"average_message_count"` // 平均消息量
}

// TagTrendData 标签趋势数据
type TagTrendData struct {
	Date         string `json:"date"`          // 日期，格式 YYYY-MM-DD
	MessageCount int    `json:"message_count"` // 消息数量
	AccountCount int    `json:"account_count"` // 账号数量
}
