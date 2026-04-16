package content

import (
	"regexp"
	"strings"
	"sync"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// CacheRefreshCallback 快取刷新回調函式
type CacheRefreshCallback func(words []model.SensitiveWord)

// SensitiveWordService 敏感词服务接口
type SensitiveWordService interface {
	// CRUD 操作
	CreateWord(word *model.SensitiveWord) error
	UpdateWord(id uint, word *model.SensitiveWord) error
	DeleteWord(id uint) error
	GetWord(id uint) (*model.SensitiveWord, error)
	ListWords(page, pageSize int, filter map[string]interface{}) ([]*model.SensitiveWord, int64, error)

	// 批量操作
	BatchImport(words []*model.SensitiveWord) error
	BatchDelete(ids []uint) error

	// 检测功能（保留向後相容）
	CheckMessage(content string) ([]MatchResult, error)

	// 缓存刷新
	RefreshCache() error

	// 註冊快取刷新回調
	OnCacheRefresh(callback CacheRefreshCallback)

	// 取得當前快取
	GetCache() []model.SensitiveWord
}

// MatchResult 匹配结果
type MatchResult struct {
	Word        string
	MatchType   string
	Priority    int
	ReplaceText *string // 替換文字，nil 表示僅告警不替換
	Category    string
}

// sensitiveWordService 实现
type sensitiveWordService struct {
	db              *gorm.DB
	cache           []model.SensitiveWord
	cacheMutex      sync.RWMutex
	refreshCallback CacheRefreshCallback
}

// NewSensitiveWordService 创建服务实例
func NewSensitiveWordService(db *gorm.DB) SensitiveWordService {
	svc := &sensitiveWordService{
		db: db,
	}

	// 初始化缓存
	if err := svc.RefreshCache(); err != nil {
		logger.Errorw("初始化敏感詞快取失敗", "error", err)
	}

	return svc
}

// CreateWord 创建敏感词
func (s *sensitiveWordService) CreateWord(word *model.SensitiveWord) error {
	if err := s.db.Create(word).Error; err != nil {
		return err
	}
	return s.RefreshCache()
}

// UpdateWord 更新敏感词
func (s *sensitiveWordService) UpdateWord(id uint, word *model.SensitiveWord) error {
	// Select 明確列出可更新欄位，避免 GORM 跳過 bool zero-value (enabled=false)
	if err := s.db.Model(&model.SensitiveWord{}).Where("id = ?", id).
		Select("word", "match_type", "category", "enabled", "priority", "description", "replace_text").
		Updates(word).Error; err != nil {
		return err
	}
	return s.RefreshCache()
}

// DeleteWord 删除敏感词
func (s *sensitiveWordService) DeleteWord(id uint) error {
	if err := s.db.Delete(&model.SensitiveWord{}, id).Error; err != nil {
		return err
	}
	return s.RefreshCache()
}

// GetWord 获取敏感词
func (s *sensitiveWordService) GetWord(id uint) (*model.SensitiveWord, error) {
	var word model.SensitiveWord
	if err := s.db.First(&word, id).Error; err != nil {
		return nil, err
	}
	return &word, nil
}

// ListWords 列出敏感词
func (s *sensitiveWordService) ListWords(page, pageSize int, filter map[string]interface{}) ([]*model.SensitiveWord, int64, error) {
	var words []*model.SensitiveWord
	var total int64

	query := s.db.Model(&model.SensitiveWord{})

	// 应用筛选
	if category, ok := filter["category"].(string); ok && category != "" {
		query = query.Where("category = ?", category)
	}
	if matchType, ok := filter["matchType"].(string); ok && matchType != "" {
		query = query.Where("match_type = ?", matchType)
	}
	if enabled, ok := filter["enabled"].(bool); ok {
		query = query.Where("enabled = ?", enabled)
	}

	// 统计总数
	query.Count(&total)

	// 分页查询
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&words).Error; err != nil {
		return nil, 0, err
	}

	return words, total, nil
}

// BatchImport 批量导入
func (s *sensitiveWordService) BatchImport(words []*model.SensitiveWord) error {
	if err := s.db.CreateInBatches(words, 100).Error; err != nil {
		return err
	}
	return s.RefreshCache()
}

// BatchDelete 批量删除
func (s *sensitiveWordService) BatchDelete(ids []uint) error {
	if err := s.db.Delete(&model.SensitiveWord{}, ids).Error; err != nil {
		return err
	}
	return s.RefreshCache()
}

// CheckMessage 检测消息
func (s *sensitiveWordService) CheckMessage(content string) ([]MatchResult, error) {
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()

	var results []MatchResult
	contentLower := strings.ToLower(content)

	for _, word := range s.cache {
		if !word.Enabled {
			continue
		}

		matched := false
		switch word.MatchType {
		case "exact":
			// 精确匹配
			matched = strings.Contains(contentLower, strings.ToLower(word.Word))
		case "fuzzy":
			// 模糊匹配（忽略大小写和空格）
			cleanContent := strings.ReplaceAll(contentLower, " ", "")
			cleanWord := strings.ReplaceAll(strings.ToLower(word.Word), " ", "")
			matched = strings.Contains(cleanContent, cleanWord)
		case "regex":
			// 正则表达式匹配
			if re, err := regexp.Compile(word.Word); err == nil {
				matched = re.MatchString(content)
			} else {
				logger.Errorw("正則表達式編譯失敗", "word", word.Word, "error", err)
			}
		}

		if matched {
			results = append(results, MatchResult{
				Word:        word.Word,
				MatchType:   word.MatchType,
				Priority:    word.Priority,
				ReplaceText: word.ReplaceText,
				Category:    word.Category,
			})
		}
	}

	return results, nil
}

// RefreshCache 刷新缓存
func (s *sensitiveWordService) RefreshCache() error {
	var words []model.SensitiveWord
	if err := s.db.Where("enabled = ?", true).Find(&words).Error; err != nil {
		return err
	}

	s.cacheMutex.Lock()
	s.cache = words
	s.cacheMutex.Unlock()

	logger.Infow("敏感詞快取已刷新", "count", len(words))

	// 通知回調
	if s.refreshCallback != nil {
		s.refreshCallback(words)
	}

	return nil
}

// OnCacheRefresh 註冊快取刷新回調
func (s *sensitiveWordService) OnCacheRefresh(callback CacheRefreshCallback) {
	s.refreshCallback = callback
	// 立即觸發一次回調，確保初始資料同步
	s.cacheMutex.RLock()
	words := make([]model.SensitiveWord, len(s.cache))
	copy(words, s.cache)
	s.cacheMutex.RUnlock()
	if callback != nil && len(words) > 0 {
		callback(words)
	}
}

// GetCache 取得當前快取
func (s *sensitiveWordService) GetCache() []model.SensitiveWord {
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()
	words := make([]model.SensitiveWord, len(s.cache))
	copy(words, s.cache)
	return words
}
