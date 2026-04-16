package analyzer

import "context"

// Analyzer 分析器介面
type Analyzer interface {
	// Analyze 分析內容，返回匹配結果
	Analyze(ctx context.Context, content string) ([]Result, error)

	// Name 分析器名稱（用於日誌和追蹤）
	Name() string
}

// AsyncAnalyzer 支援異步分析的介面
type AsyncAnalyzer interface {
	Analyzer

	// AnalyzeAsync 異步分析，結果透過 callback 返回
	AnalyzeAsync(ctx context.Context, task *AnalysisTask) error
}

// CacheRefresher 支援快取刷新的分析器
type CacheRefresher interface {
	// RefreshCache 刷新快取
	RefreshCache(words []Word) error
}

// Word 敏感詞定義（從 model 抽離，避免循環依賴）
type Word struct {
	ID          uint
	Word        string
	MatchType   string
	Category    string
	Enabled     bool
	Priority    int
	ReplaceText *string
}
