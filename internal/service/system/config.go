package system

import (
	"sync"
	"time"

	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const configCacheTTL = 5 * time.Minute

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

// ConfigService 配置服务接口
type ConfigService interface {
	GetConfig(key string) (string, error)
	SetConfig(key, value, updatedBy string) error
	GetAllConfigs() (map[string]string, error)
	GetAllConfigItems() ([]model.SystemConfig, error)
	IsSecretKey(key string) (bool, error)
	GetTelegramConfig() (*TelegramConfig, error)
}

// TelegramConfig Telegram 配置
type TelegramConfig struct {
	BotToken      string `json:"botToken"`
	ChatID        string `json:"chatId"`
	Enabled       bool   `json:"enabled"`
	NotifyEnabled bool   `json:"notifyEnabled"`
}

// configService 实现
type configService struct {
	db    *gorm.DB
	cache map[string]cacheEntry
	mu    sync.RWMutex
}

// NewConfigService 创建服务实例
func NewConfigService(db *gorm.DB) ConfigService {
	return &configService{
		db:    db,
		cache: make(map[string]cacheEntry),
	}
}

// GetConfig 获取配置（帶記憶體快取，TTL 5 分鐘）
func (s *configService) GetConfig(key string) (string, error) {
	s.mu.RLock()
	if entry, ok := s.cache[key]; ok && time.Now().Before(entry.expiresAt) {
		s.mu.RUnlock()
		return entry.value, nil
	}
	s.mu.RUnlock()

	var config model.SystemConfig
	if err := s.db.Where("config_key = ?", key).First(&config).Error; err != nil {
		return "", err
	}

	s.mu.Lock()
	s.cache[key] = cacheEntry{value: config.ConfigValue, expiresAt: time.Now().Add(configCacheTTL)}
	s.mu.Unlock()

	return config.ConfigValue, nil
}

// SetConfig 设置配置 (upsert)，寫入後清除快取
func (s *configService) SetConfig(key, value, updatedBy string) error {
	config := model.SystemConfig{
		ConfigKey:   key,
		ConfigValue: value,
		UpdatedBy:   updatedBy,
		UpdatedAt:   time.Now(),
	}
	err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "config_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"config_value", "updated_by", "updated_at"}),
	}).Create(&config).Error

	if err == nil {
		s.mu.Lock()
		delete(s.cache, key)
		s.mu.Unlock()
	}
	return err
}

// GetAllConfigs 获取所有配置
func (s *configService) GetAllConfigs() (map[string]string, error) {
	var configs []model.SystemConfig
	if err := s.db.Find(&configs).Error; err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, config := range configs {
		result[config.ConfigKey] = config.ConfigValue
	}

	return result, nil
}

// GetAllConfigItems 回傳完整 SystemConfig 列表（供 API handler 使用）
func (s *configService) GetAllConfigItems() ([]model.SystemConfig, error) {
	var configs []model.SystemConfig
	if err := s.db.Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

// IsSecretKey 檢查指定 key 是否為 secret
func (s *configService) IsSecretKey(key string) (bool, error) {
	var config model.SystemConfig
	if err := s.db.Where("config_key = ?", key).First(&config).Error; err != nil {
		return false, err
	}
	return config.IsSecret, nil
}

// GetTelegramConfig 获取 Telegram 配置
func (s *configService) GetTelegramConfig() (*TelegramConfig, error) {
	configs, err := s.GetAllConfigs()
	if err != nil {
		return nil, err
	}

	return &TelegramConfig{
		BotToken:      configs["telegram.bot_token"],
		ChatID:        configs["telegram.chat_id"],
		Enabled:       configs["sensitive_word.enabled"] == "true",
		NotifyEnabled: configs["telegram.enabled"] == "true", // 使用前端保存的 telegram.enabled 配置
	}, nil
}
