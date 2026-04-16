package analyzer

import (
	"context"
	"regexp"
	"strings"
	"sync"

	"whatsapp_golang/internal/logger"
)

// safeLogInfow 安全的日誌記錄（避免 logger 未初始化時 panic）
func safeLogInfow(msg string, keysAndValues ...interface{}) {
	if logger.Logger != nil {
		logger.Infow(msg, keysAndValues...)
	}
}

// safeLogErrorw 安全的錯誤日誌記錄
func safeLogErrorw(msg string, keysAndValues ...interface{}) {
	if logger.Logger != nil {
		logger.Errorw(msg, keysAndValues...)
	}
}

// KeywordAnalyzer 關鍵字分析器
type KeywordAnalyzer struct {
	cache      []Word
	cacheMutex sync.RWMutex
}

// NewKeywordAnalyzer 建立關鍵字分析器
func NewKeywordAnalyzer() *KeywordAnalyzer {
	return &KeywordAnalyzer{}
}

// Name 返回分析器名稱
func (a *KeywordAnalyzer) Name() string {
	return "keyword"
}

// Analyze 分析內容，返回匹配結果
func (a *KeywordAnalyzer) Analyze(ctx context.Context, content string) ([]Result, error) {
	a.cacheMutex.RLock()
	defer a.cacheMutex.RUnlock()

	var results []Result
	contentLower := strings.ToLower(content)

	for _, word := range a.cache {
		if !word.Enabled {
			continue
		}

		matched := false
		switch word.MatchType {
		case "exact":
			// 精確匹配（不區分大小寫）
			matched = strings.Contains(contentLower, strings.ToLower(word.Word))
		case "fuzzy":
			// 模糊匹配（忽略大小寫和空格）
			cleanContent := strings.ReplaceAll(contentLower, " ", "")
			cleanWord := strings.ReplaceAll(strings.ToLower(word.Word), " ", "")
			matched = strings.Contains(cleanContent, cleanWord)
		case "regex":
			// 正則表達式匹配
			if re, err := regexp.Compile(word.Word); err == nil {
				matched = re.MatchString(content)
			} else {
				safeLogErrorw("正則表達式編譯失敗", "word", word.Word, "error", err)
			}
		}

		if matched {
			results = append(results, Result{
				Word:        word.Word,
				Category:    word.Category,
				MatchType:   word.MatchType,
				Priority:    word.Priority,
				ReplaceText: word.ReplaceText,
				Source:      "keyword",
				Confidence:  1.0, // 關鍵字匹配信心度為 1.0
			})
		}
	}

	return results, nil
}

// RefreshCache 刷新快取
func (a *KeywordAnalyzer) RefreshCache(words []Word) error {
	a.cacheMutex.Lock()
	a.cache = words
	a.cacheMutex.Unlock()

	safeLogInfow("KeywordAnalyzer 快取已刷新", "count", len(words))
	return nil
}

// GetCacheSize 取得快取大小
func (a *KeywordAnalyzer) GetCacheSize() int {
	a.cacheMutex.RLock()
	defer a.cacheMutex.RUnlock()
	return len(a.cache)
}
