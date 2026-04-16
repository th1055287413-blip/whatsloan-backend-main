package analyzer

import (
	"context"
)

// AIAnalyzer AI 分析器介面（預留）
// 實作時需要注入 prompt manager 和 AI client
type AIAnalyzer struct {
	// promptManager *prompt.Manager  // 未來實作
	// client        *openai.Client   // 未來實作
	enabled bool
}

// NewAIAnalyzer 建立 AI 分析器
func NewAIAnalyzer() *AIAnalyzer {
	return &AIAnalyzer{
		enabled: false, // 預設停用
	}
}

// Name 返回分析器名稱
func (a *AIAnalyzer) Name() string {
	return "ai"
}

// Analyze 分析內容（預留實作）
func (a *AIAnalyzer) Analyze(ctx context.Context, content string) ([]Result, error) {
	if !a.enabled {
		return nil, nil
	}

	// 未來實作:
	// 1. 使用 promptManager 取得 prompt template
	// 2. 呼叫 AI API
	// 3. 解析回應
	// 4. 返回 Result

	return nil, nil
}

// AnalyzeAsync 異步分析（預留實作）
func (a *AIAnalyzer) AnalyzeAsync(ctx context.Context, task *AnalysisTask) error {
	if !a.enabled {
		return nil
	}

	// 未來實作:
	// 1. 建立分析請求
	// 2. 呼叫 AI API（可能需要限流）
	// 3. 處理回應
	// 4. 透過 callback 返回結果

	return nil
}

// SetEnabled 設定是否啟用
func (a *AIAnalyzer) SetEnabled(enabled bool) {
	a.enabled = enabled
}

// IsEnabled 檢查是否啟用
func (a *AIAnalyzer) IsEnabled() bool {
	return a.enabled
}
