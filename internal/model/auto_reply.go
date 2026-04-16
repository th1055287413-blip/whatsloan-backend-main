package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// AutoReplyMatchType 匹配类型
type AutoReplyMatchType string

const (
	AutoReplyMatchTypeExact AutoReplyMatchType = "exact" // 精确匹配
	AutoReplyMatchTypeFuzzy AutoReplyMatchType = "fuzzy" // 模糊匹配
)

// AutoReplyStatus 状态
type AutoReplyStatus string

const (
	AutoReplyStatusActive   AutoReplyStatus = "active"   // 激活
	AutoReplyStatusInactive AutoReplyStatus = "inactive" // 停用
)

// AutoReplyKeywordType 关键词类型
type AutoReplyKeywordType string

const (
	AutoReplyKeywordTypeNormal  AutoReplyKeywordType = "normal"  // 普通关键词
	AutoReplyKeywordTypeWelcome AutoReplyKeywordType = "welcome" // 欢迎语
	AutoReplyKeywordTypeDefault AutoReplyKeywordType = "default" // 默认回复
)

// AutoReplyLanguage 语言
type AutoReplyLanguage string

const (
	AutoReplyLanguageZhCN AutoReplyLanguage = "zh-CN" // 简体中文
	AutoReplyLanguageZhTW AutoReplyLanguage = "zh-TW" // 繁体中文
	AutoReplyLanguageEN   AutoReplyLanguage = "en"    // 英文
	AutoReplyLanguageID   AutoReplyLanguage = "id"    // 印尼语
	AutoReplyLanguageMS   AutoReplyLanguage = "ms"    // 马来语
)

// AutoReplyKeywordList 关键词列表（用于JSON存储）
type AutoReplyKeywordList []string

// Value 实现 driver.Valuer 接口
func (k AutoReplyKeywordList) Value() (driver.Value, error) {
	return json.Marshal(k)
}

// Scan 实现 sql.Scanner 接口
func (k *AutoReplyKeywordList) Scan(value interface{}) error {
	if value == nil {
		*k = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, k)
}

// AutoReplyKeyword 自动回复关键词表
type AutoReplyKeyword struct {
	ID          uint                 `gorm:"primaryKey" json:"id"`
	Keywords    AutoReplyKeywordList `gorm:"type:jsonb;not null" json:"keywords"`                   // 关键词列表（支持多个同义词）
	Reply       string               `gorm:"type:text;not null" json:"reply"`                       // 回复内容
	Priority    int                  `gorm:"default:5;not null" json:"priority"`                    // 优先级 (1-10)
	MatchType   AutoReplyMatchType   `gorm:"size:20;default:'fuzzy';not null" json:"match_type"`    // 匹配模式
	Status      AutoReplyStatus      `gorm:"size:20;default:'active';not null" json:"status"`       // 状态
	Language    AutoReplyLanguage    `gorm:"size:10;default:'zh-CN';not null" json:"language"`      // 语言
	KeywordType AutoReplyKeywordType `gorm:"size:20;default:'normal';not null" json:"keyword_type"` // 关键词类型
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

// TableName 设置表名
func (AutoReplyKeyword) TableName() string {
	return "auto_reply_keywords"
}
