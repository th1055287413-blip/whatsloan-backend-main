package analyzer

// Result 分析結果
type Result struct {
	Word        string         // 匹配的詞
	Category    string         // 分類
	MatchType   string         // 匹配類型: exact, fuzzy, regex, ai
	Priority    int            // 優先級
	Confidence  float64        // 信心度 (AI 分析用, 0-1)
	ReplaceText *string        // 替換文字 (nil = 僅告警)
	Source      string         // 來源: "keyword" | "ai"
	Metadata    map[string]any // 額外資訊
}

// AnalysisTask 分析任務
type AnalysisTask struct {
	ID               string // 任務 ID
	AccountID        uint   // 帳號 ID
	MessageID        string // 訊息 ID
	ChatID           string // 聊天 ID
	Content          string // 訊息內容
	SenderJID        string // 發送者
	SenderName       string // 發送者名稱
	ReceiverName     string // 接收者名稱
	IsFromMe         bool   // 是否自己發送
	MessageTimestamp int64  // 訊息時間戳
	CreatedAt        int64  // 建立時間
}

// HasReplacement 檢查結果是否需要替換
func (r *Result) HasReplacement() bool {
	return r.ReplaceText != nil
}

// NeedsReplacement 檢查結果列表是否有任何需要替換的項目
func NeedsReplacement(results []Result) bool {
	for _, r := range results {
		if r.HasReplacement() {
			return true
		}
	}
	return false
}

// ExtractWords 從結果列表提取所有匹配的詞
func ExtractWords(results []Result) []string {
	words := make([]string, len(results))
	for i, r := range results {
		words[i] = r.Word
	}
	return words
}
