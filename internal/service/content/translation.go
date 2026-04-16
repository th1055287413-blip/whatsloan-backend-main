package content

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"whatsapp_golang/internal/model"
	systemSvc "whatsapp_golang/internal/service/system"

	"gorm.io/gorm"
)

// normalizeLanguageCode 標準化語言代碼 (例如 zh_TW -> zh-TW)
func normalizeLanguageCode(code string) string {
	return strings.ReplaceAll(strings.TrimSpace(code), "_", "-")
}

// getLanguageVariants 取得語言代碼的所有變體
func getLanguageVariants(code string) []string {
	normalized := normalizeLanguageCode(code)
	base := getBaseLanguageCode(normalized)

	variants := []string{normalized}
	if base != normalized {
		variants = append(variants, base)
	}

	// 常見變體映射
	variantMap := map[string][]string{
		"zh": {"zh-CN", "zh-TW", "zh-HK", "zh-MO"},
		"en": {"en-US", "en-GB"},
		"pt": {"pt-PT", "pt-BR"},
		"es": {"es-ES", "es-MX"},
	}

	if extra, ok := variantMap[base]; ok {
		for _, v := range extra {
			if v != normalized {
				variants = append(variants, v)
			}
		}
	}

	return variants
}

// getBaseLanguageCode 取得基礎語言代碼 (例如 zh-TW -> zh)
func getBaseLanguageCode(code string) string {
	parts := strings.Split(normalizeLanguageCode(code), "-")
	return parts[0]
}

// DefaultLanguages 默认语言配置列表(用于新用户初始化)
var DefaultLanguages = []model.LanguageConfig{
	{LanguageCode: "zh-CN", LanguageName: "中文(简体)", CountryCode: "CN", CountryName: "中国", IsDefault: true},
	{LanguageCode: "zh-TW", LanguageName: "中文(繁体)", CountryCode: "TW", CountryName: "台湾", IsDefault: false},
	{LanguageCode: "en-US", LanguageName: "English", CountryCode: "US", CountryName: "美国", IsDefault: false},
	{LanguageCode: "ja-JP", LanguageName: "日本語", CountryCode: "JP", CountryName: "日本", IsDefault: false},
	{LanguageCode: "ko-KR", LanguageName: "한국어", CountryCode: "KR", CountryName: "韩国", IsDefault: false},
}

// TranslationService 翻译服务接口
type TranslationService interface {
	TranslateWithCache(text string, targetLang string, sourceLang string) (string, bool, error)
	BatchTranslate(texts []string, targetLang string) ([]model.TranslationResponse, error)
	GetLanguageConfigs(userID uint) ([]model.LanguageConfig, error)
	CreateLanguageConfig(config *model.LanguageConfig) error
	UpdateLanguageConfig(id uint, userID uint, updates map[string]interface{}) error
	DeleteLanguageConfig(id uint, userID uint) error
	GetTranslationConfig(userID uint) (*model.TranslationConfig, error)
	UpdateTranslationConfig(userID uint, updates map[string]interface{}) error
	InitializeDefaultLanguages(userID uint) error
}

// translationService 翻译服务实现
type translationService struct {
	db          *gorm.DB
	httpTimeout int // HTTP 超時 (秒)
	configSvc   systemSvc.ConfigService
}

// NewTranslationService 创建翻译服务实例
func NewTranslationService(db *gorm.DB, httpTimeout int, configSvc systemSvc.ConfigService) TranslationService {
	if httpTimeout <= 0 {
		httpTimeout = 30 // 預設 30 秒
	}
	return &translationService{
		db:          db,
		httpTimeout: httpTimeout,
		configSvc:   configSvc,
	}
}

// OpenRouterRequest OpenRouter API 请求结构
type OpenRouterRequest struct {
	Model    string              `json:"model"`
	Messages []OpenRouterMessage `json:"messages"`
}

// OpenRouterMessage OpenRouter 消息结构
type OpenRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenRouterResponse OpenRouter API 响应结构
type OpenRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// getMD5 计算字符串的 MD5 哈希
func (s *translationService) getMD5(text string) string {
	hash := md5.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}

// getCache 查询翻译缓存
// 支持语言代码变体匹配(如 zh 可以匹配 zh-CN, zh-TW等)
func (s *translationService) getCache(text string, targetLang string) (*model.TranslationCache, error) {
	hash := s.getMD5(text)

	// 规范化目标语言代码
	normalizedLang := normalizeLanguageCode(targetLang)

	// 获取语言代码的所有变体
	variants := getLanguageVariants(normalizedLang)

	var cache model.TranslationCache

	// 优先匹配规范化后的代码
	result := s.db.Where("content_hash = ? AND target_lang = ?", hash, normalizedLang).First(&cache)
	if result.Error == nil {
		// 更新命中次数
		s.db.Model(&cache).Update("hit_count", gorm.Expr("hit_count + ?", 1))
		return &cache, nil
	}

	// 如果没找到,尝试匹配语言变体
	if result.Error == gorm.ErrRecordNotFound && len(variants) > 1 {
		result = s.db.Where("content_hash = ? AND target_lang IN ?", hash, variants).First(&cache)
		if result.Error == nil {
			// 更新命中次数
			s.db.Model(&cache).Update("hit_count", gorm.Expr("hit_count + ?", 1))
			return &cache, nil
		}
	}

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, result.Error
	}

	return &cache, nil
}

// setCache 写入翻译缓存
func (s *translationService) setCache(text string, targetLang string, translatedText string, sourceLang string) error {
	hash := s.getMD5(text)

	// 规范化语言代码
	normalizedTargetLang := normalizeLanguageCode(targetLang)
	normalizedSourceLang := ""
	if sourceLang != "" {
		normalizedSourceLang = normalizeLanguageCode(sourceLang)
	}

	cache := model.TranslationCache{
		ContentHash:    hash,
		OriginalText:   text,
		SourceLang:     normalizedSourceLang,
		TargetLang:     normalizedTargetLang,
		TranslatedText: translatedText,
		HitCount:       1,
	}

	// 使用 UPSERT 语义（如果已存在则更新）
	result := s.db.Where("content_hash = ? AND target_lang = ?", hash, normalizedTargetLang).
		Assign(map[string]interface{}{
			"translated_text": translatedText,
			"updated_at":      time.Now(),
		}).
		FirstOrCreate(&cache)

	return result.Error
}

// translateWithOpenRouter 使用 OpenRouter API 进行翻译
func (s *translationService) translateWithOpenRouter(text string, targetLang string) (string, error) {
	apiKey := ""
	if s.configSvc != nil {
		apiKey, _ = s.configSvc.GetConfig("llm.api_key")
	}
	if apiKey == "" {
		return "", fmt.Errorf("llm.api_key 未配置，請在系統設定中設置")
	}

	// 规范化目标语言代码
	normalizedLang := normalizeLanguageCode(targetLang)

	// 语言代码映射(支持新格式)
	langMap := map[string]string{
		// 中文变体
		"zh":    "Chinese",
		"zh-CN": "Simplified Chinese",
		"zh-TW": "Traditional Chinese",
		"zh-HK": "Traditional Chinese (Hong Kong)",
		"zh-MO": "Traditional Chinese (Macau)",
		// 英语变体
		"en":    "English",
		"en-US": "English (US)",
		"en-GB": "English (UK)",
		// 其他语言
		"ja":    "Japanese",
		"ja-JP": "Japanese",
		"ko":    "Korean",
		"ko-KR": "Korean",
		"es":    "Spanish",
		"es-ES": "Spanish",
		"fr":    "French",
		"fr-FR": "French",
		"de":    "German",
		"de-DE": "German",
		"it":    "Italian",
		"it-IT": "Italian",
		"pt":    "Portuguese",
		"pt-PT": "Portuguese",
		"pt-BR": "Portuguese (Brazil)",
		"ru":    "Russian",
		"ru-RU": "Russian",
		"ar":    "Arabic",
		"ar-SA": "Arabic",
		"th":    "Thai",
		"th-TH": "Thai",
		"vi":    "Vietnamese",
		"vi-VN": "Vietnamese",
		"id":    "Indonesian",
		"id-ID": "Indonesian",
		"ms":    "Malay",
		"ms-MY": "Malay",
	}

	// 优先使用规范化后的代码
	targetLangName := langMap[normalizedLang]
	if targetLangName == "" {
		// 如果没有找到,尝试使用基础语言代码
		baseCode := getBaseLanguageCode(normalizedLang)
		targetLangName = langMap[baseCode]
		if targetLangName == "" {
			targetLangName = normalizedLang
		}
	}

	// 從 system_configs 讀取動態模型設定，fallback 硬編碼預設值
	activeModel := "google/gemini-2.5-flash-lite"
	if s.configSvc != nil {
		if m, err := s.configSvc.GetConfig("llm.translation_model"); err == nil && m != "" {
			activeModel = m
		}
	}

	// 构建请求，使用配置的模型
	reqBody := OpenRouterRequest{
		Model: activeModel,
		Messages: []OpenRouterMessage{
			{
				Role:    "system",
				Content: "You are a professional translator. Follow these strict translation principles:\n1. Translate naturally and accurately - preserve the original meaning and tone, avoid literal/word-by-word translation\n2. Keep all numbers unchanged (phone numbers, quantities, dates, IDs, etc.) unless they are part of idiomatic expressions\n3. NEVER translate URLs, email addresses, file paths, technical identifiers, code snippets, or any special formatted content\n4. Preserve all formatting (line breaks, spaces, punctuation in special contexts)\n5. Keep proper nouns, brand names, and product names in their original form\n6. Only return the translated text without any explanations, notes, or meta-comments.",
			},
			{
				Role:    "user",
				Content: fmt.Sprintf("Translate the following text to %s:\n\n%s", targetLangName, text),
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %v", err)
	}

	// 添加重试逻辑，最多重试 3 次
	maxRetries := 3
	var lastErr error

	for retry := 0; retry < maxRetries; retry++ {
		if retry > 0 {
			// 重试前等待（指数退避）
			waitTime := time.Duration(retry*retry) * time.Second
			time.Sleep(waitTime)
		}

		// 发送请求
		req, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			return "", fmt.Errorf("创建请求失败: %v", err)
		}

		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: time.Duration(s.httpTimeout) * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("发送请求失败: %v", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			lastErr = fmt.Errorf("读取响应失败: %v", err)
			continue
		}

		// 检查状态码，429 错误继续重试
		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("OpenRouter API 限流 (429)，正在重试 (%d/%d)", retry+1, maxRetries)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("OpenRouter API 返回错误: %d - %s", resp.StatusCode, string(body))
			// 非 429 错误，不重试直接返回
			return "", lastErr
		}

		// 解析响应
		var openRouterResp OpenRouterResponse
		if err := json.Unmarshal(body, &openRouterResp); err != nil {
			lastErr = fmt.Errorf("解析响应失败: %v", err)
			continue
		}

		if len(openRouterResp.Choices) == 0 {
			lastErr = fmt.Errorf("OpenRouter 返回空响应")
			continue
		}

		// 成功，返回翻译结果
		return openRouterResp.Choices[0].Message.Content, nil
	}

	// 所有重试都失败
	if lastErr != nil {
		return "", fmt.Errorf("翻译失败（已重试 %d 次）: %v", maxRetries, lastErr)
	}
	return "", fmt.Errorf("翻译失败（已重试 %d 次）", maxRetries)
}

// TranslateWithCache 带缓存的翻译（核心方法）
func (s *translationService) TranslateWithCache(text string, targetLang string, sourceLang string) (string, bool, error) {
	// 1. 查询缓存
	cache, err := s.getCache(text, targetLang)
	if err != nil {
		return "", false, fmt.Errorf("查询缓存失败: %v", err)
	}

	if cache != nil {
		return cache.TranslatedText, true, nil
	}

	// 2. 缓存未命中，调用 OpenRouter 翻译
	translatedText, err := s.translateWithOpenRouter(text, targetLang)
	if err != nil {
		return "", false, fmt.Errorf("翻译失败: %v", err)
	}

	// 3. 写入缓存，保存源语言
	if err := s.setCache(text, targetLang, translatedText, sourceLang); err != nil {
		// 缓存写入失败不影响翻译结果返回
		fmt.Printf("写入缓存失败: %v\n", err)
	}

	return translatedText, false, nil
}

// BatchTranslate 批量翻译
func (s *translationService) BatchTranslate(texts []string, targetLang string) ([]model.TranslationResponse, error) {
	results := make([]model.TranslationResponse, len(texts))

	for i, text := range texts {
		translatedText, cached, err := s.TranslateWithCache(text, targetLang, "")
		if err != nil {
			return nil, err
		}

		results[i] = model.TranslationResponse{
			TranslatedText: translatedText,
			Cached:         cached,
		}
	}

	return results, nil
}

// GetLanguageConfigs 获取用户的语言配置列表
func (s *translationService) GetLanguageConfigs(userID uint) ([]model.LanguageConfig, error) {
	var configs []model.LanguageConfig
	result := s.db.Where("user_id = ?", userID).Order("is_default DESC, created_at ASC").Find(&configs)
	return configs, result.Error
}

// CreateLanguageConfig 创建语言配置
func (s *translationService) CreateLanguageConfig(config *model.LanguageConfig) error {
	return s.db.Create(config).Error
}

// UpdateLanguageConfig 更新语言配置
func (s *translationService) UpdateLanguageConfig(id uint, userID uint, updates map[string]interface{}) error {
	result := s.db.Model(&model.LanguageConfig{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(updates)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("语言配置不存在或无权限")
	}

	return nil
}

// DeleteLanguageConfig 删除语言配置
func (s *translationService) DeleteLanguageConfig(id uint, userID uint) error {
	result := s.db.Where("id = ? AND user_id = ?", id, userID).Delete(&model.LanguageConfig{})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("语言配置不存在或无权限")
	}

	return nil
}

// GetTranslationConfig 获取用户的翻译配置
func (s *translationService) GetTranslationConfig(userID uint) (*model.TranslationConfig, error) {
	var config model.TranslationConfig
	result := s.db.Where("user_id = ?", userID).First(&config)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			// 如果不存在，创建默认配置
			config = model.TranslationConfig{
				UserID:                userID,
				AutoTranslateReceived: false,
				AutoTranslateSent:     false,
			}
			if err := s.db.Create(&config).Error; err != nil {
				return nil, err
			}
			return &config, nil
		}
		return nil, result.Error
	}

	return &config, nil
}

// UpdateTranslationConfig 更新翻译配置
func (s *translationService) UpdateTranslationConfig(userID uint, updates map[string]interface{}) error {
	// 先获取或创建配置
	var config model.TranslationConfig
	result := s.db.Where("user_id = ?", userID).First(&config)

	if result.Error == gorm.ErrRecordNotFound {
		// 不存在则创建
		config.UserID = userID
		if err := s.db.Create(&config).Error; err != nil {
			return err
		}
	}

	// 更新配置
	return s.db.Model(&config).Updates(updates).Error
}

// InitializeDefaultLanguages 为新用户初始化默认语言配置
// 只在用户没有任何语言配置时执行
func (s *translationService) InitializeDefaultLanguages(userID uint) error {
	// 检查用户是否已有语言配置
	var count int64
	if err := s.db.Model(&model.LanguageConfig{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return fmt.Errorf("检查用户语言配置失败: %v", err)
	}

	// 如果已有配置,不进行初始化
	if count > 0 {
		return nil
	}

	// 批量创建默认语言配置
	for _, lang := range DefaultLanguages {
		langConfig := model.LanguageConfig{
			UserID:       userID,
			LanguageCode: lang.LanguageCode,
			LanguageName: lang.LanguageName,
			CountryCode:  lang.CountryCode,
			CountryName:  lang.CountryName,
			IsDefault:    lang.IsDefault,
		}

		if err := s.db.Create(&langConfig).Error; err != nil {
			// 如果创建失败(可能是唯一索引冲突),继续下一个
			// 这样可以确保至少创建部分配置
			fmt.Printf("创建默认语言配置失败 (user_id=%d, lang=%s): %v\n", userID, lang.LanguageCode, err)
			continue
		}
	}

	return nil
}
