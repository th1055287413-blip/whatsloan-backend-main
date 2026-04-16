package model

import (
	"time"
)

// TranslationCache 翻译缓存表 - 核心优化，避免重复翻译
type TranslationCache struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ContentHash    string    `gorm:"size:32;not null;uniqueIndex:idx_cache_unique,composite:content_target" json:"content_hash"` // MD5(原文)
	OriginalText   string    `gorm:"type:text;not null" json:"original_text"`                                                      // 原文
	SourceLang     string    `gorm:"size:10" json:"source_lang"`                                                                   // 源语言
	TargetLang     string    `gorm:"size:10;not null;uniqueIndex:idx_cache_unique,composite:content_target" json:"target_lang"`  // 目标语言
	TranslatedText string    `gorm:"type:text;not null" json:"translated_text"`                                                   // 译文
	HitCount       int       `gorm:"default:1" json:"hit_count"`                                                                   // 缓存命中次数
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// LanguageConfig 语言配置表
type LanguageConfig struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UserID       uint      `gorm:"not null;uniqueIndex:idx_user_lang,composite:user_language" json:"user_id"` // 用户ID
	LanguageCode string    `gorm:"size:10;not null;uniqueIndex:idx_user_lang,composite:user_language" json:"language_code"` // ISO 639-1: zh, en, ja
	LanguageName string    `gorm:"size:50;not null" json:"language_name"` // 中文, English, 日本語
	CountryCode  string    `gorm:"size:10;not null" json:"country_code"`  // ISO 3166: CN, US, JP
	CountryName  string    `gorm:"size:50;not null" json:"country_name"`  // 中国, USA, Japan
	IsDefault    bool      `gorm:"default:false" json:"is_default"`       // 是否为默认语言
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TranslationConfig 翻译配置表
type TranslationConfig struct {
	ID                      uint      `gorm:"primaryKey" json:"id"`
	UserID                  uint      `gorm:"uniqueIndex;not null" json:"user_id"`                   // 用户ID
	AutoTranslateReceived   bool      `gorm:"default:false" json:"auto_translate_received"`          // 自动翻译接收消息
	AutoTranslateSent       bool      `gorm:"default:false" json:"auto_translate_sent"`              // 自动翻译发送消息
	DefaultTargetLanguageID *uint     `json:"default_target_language_id"`                            // 默认目标语言ID
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

// TableName 设置表名
func (TranslationCache) TableName() string {
	return "translation_cache"
}

func (LanguageConfig) TableName() string {
	return "language_configs"
}

func (TranslationConfig) TableName() string {
	return "translation_configs"
}

// TranslationRequest 翻译请求
type TranslationRequest struct {
	Text           string `json:"text" binding:"required"`
	TargetLanguage string `json:"target_language" binding:"required"`
	SourceLanguage string `json:"source_language"`
}

// TranslationResponse 翻译响应
type TranslationResponse struct {
	TranslatedText string `json:"translated_text"`
	SourceLanguage string `json:"source_language,omitempty"`
	Cached         bool   `json:"cached"`
}

// BatchTranslationRequest 批量翻译请求
type BatchTranslationRequest struct {
	Texts          []string `json:"texts" binding:"required"`
	TargetLanguage string   `json:"target_language" binding:"required"`
}

// BatchTranslationResponse 批量翻译响应
type BatchTranslationResponse struct {
	Translations []struct {
		Original   string `json:"original"`
		Translated string `json:"translated"`
		Cached     bool   `json:"cached"`
	} `json:"translations"`
}
