package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"whatsapp_golang/internal/llm"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	systemSvc "whatsapp_golang/internal/service/system"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

const defaultLLMModel = "google/gemini-2.5-flash"

type ChatAIAnalysisService interface {
	RunAnalysis(ctx context.Context) error
	AnalyzeChat(ctx context.Context, accountID uint, chatID uint) error
}

type analysisResult struct {
	Relationship string   `json:"relationship"`
	Topics       []string `json:"topics"`
	Summary      string   `json:"summary"`
}

type chatAIAnalysisService struct {
	db        *gorm.DB
	llmClient *llm.Client
	configSvc systemSvc.ConfigService
	tagDefSvc AiTagDefinitionService
	running   atomic.Bool
}

type ChatAIAnalysisConfig struct {
	DB        *gorm.DB
	LLMClient *llm.Client
	ConfigSvc systemSvc.ConfigService
	TagDefSvc AiTagDefinitionService
}

func NewChatAIAnalysisService(cfg *ChatAIAnalysisConfig) ChatAIAnalysisService {
	return &chatAIAnalysisService{
		db:        cfg.DB,
		llmClient: cfg.LLMClient,
		configSvc: cfg.ConfigSvc,
		tagDefSvc: cfg.TagDefSvc,
	}
}

func (s *chatAIAnalysisService) getConfigInt(key string, fallback int) int {
	if s.configSvc == nil {
		return fallback
	}
	v, err := s.configSvc.GetConfig(key)
	if err != nil || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// pendingChat represents a chat that needs AI analysis.
type pendingChat struct {
	AccountID uint
	ChatID    uint
}

func (s *chatAIAnalysisService) RunAnalysis(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		logger.Info("AI 分析: 上一輪仍在執行，跳過本次")
		return nil
	}
	defer s.running.Store(false)

	// 1. Get accounts with AI analysis enabled
	var accountIDs []uint
	if err := s.db.Model(&model.WhatsAppAccount{}).
		Where("ai_analysis_enabled = ? AND status = ?", true, "connected").
		Pluck("id", &accountIDs).Error; err != nil {
		return fmt.Errorf("query enabled accounts: %w", err)
	}
	if len(accountIDs) == 0 {
		logger.Info("AI 分析: 無已啟用的帳號，跳過")
		return nil
	}
	logger.Debugw("AI 分析: 掃描帳號", "count", len(accountIDs))

	// 2. Find chats with new messages after watermark, respecting per-chat cooldown
	var pending []pendingChat
	const minNewMessages = 10
	sql := `
		SELECT c.account_id, c.id AS chat_id
		FROM whatsapp_chats c
		LEFT JOIN chat_ai_summaries s
			ON s.chat_id = c.id AND s.account_id = c.account_id
		WHERE c.account_id IN ?
		  AND c.is_group = false
		  AND (s.last_analyzed IS NULL OR s.last_analyzed <= NOW() - MAKE_INTERVAL(mins := ?))
		  AND (
			SELECT COUNT(*) FROM whatsapp_messages m
			WHERE m.chat_id = c.id
			  AND m.type = 'text'
			  AND m.is_revoked = false
			  AND m.id > COALESCE(s.last_message_id, 0)
		  ) >= ?
		LIMIT ?
	`
	intervalMin := s.getConfigInt("ai_analysis.interval_min", 30)
	batchSize := s.getConfigInt("ai_analysis.batch_size", 20)
	if err := s.db.Raw(sql, accountIDs, intervalMin, minNewMessages, batchSize).Scan(&pending).Error; err != nil {
		return fmt.Errorf("query pending chats: %w", err)
	}

	if len(pending) == 0 {
		logger.Debug("AI 分析: 無待分析聊天室")
		return nil
	}
	logger.Infow("AI 分析: 本輪待處理聊天室", "count", len(pending))

	// 3. Worker pool concurrent processing
	jobs := make(chan pendingChat, len(pending))
	var wg sync.WaitGroup

	workerCount := s.getConfigInt("ai_analysis.worker_count", 3)
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				if ctx.Err() != nil {
					return
				}
				logger.Debugw("AI 分析: 開始", "account_id", p.AccountID, "chat_id", p.ChatID)
				if err := s.AnalyzeChat(ctx, p.AccountID, p.ChatID); err != nil {
					logger.Errorw("AI 分析: 失敗", "account_id", p.AccountID, "chat_id", p.ChatID, "error", err)
				} else {
					logger.Debugw("AI 分析: 完成", "account_id", p.AccountID, "chat_id", p.ChatID)
				}
			}
		}()
	}

	for _, p := range pending {
		jobs <- p
	}
	close(jobs)
	wg.Wait()

	return nil
}

func (s *chatAIAnalysisService) AnalyzeChat(ctx context.Context, accountID uint, chatID uint) error {
	// 1. Get existing summary (may be empty, not an error)
	var existing model.ChatAISummary
	s.db.Session(&gorm.Session{Logger: gormlogger.Discard}).
		Where("chat_id = ? AND account_id = ?", chatID, accountID).First(&existing)

	historyDays := s.getConfigInt("ai_analysis.history_days", 7)
	cutoff := time.Now().AddDate(0, 0, -historyDays)

	// 2. History messages: before watermark, last N, within historyDays
	var history []model.WhatsAppMessage
	historyQuery := s.db.Where("chat_id = ? AND type = ? AND is_revoked = false", chatID, "text").
		Where("timestamp >= ?", cutoff)
	if existing.LastMessageID > 0 {
		historyQuery = historyQuery.Where("id <= ?", existing.LastMessageID)
	}
	historyLimit := s.getConfigInt("ai_analysis.history_limit", 50)
	historyQuery.Order("id DESC").Limit(historyLimit).Find(&history)

	// 3. New messages: after watermark
	var newMsgs []model.WhatsAppMessage
	s.db.Where("chat_id = ? AND type = ? AND is_revoked = false AND id > ?", chatID, "text", existing.LastMessageID).
		Order("id ASC").Find(&newMsgs)

	if len(history)+len(newMsgs) < 10 {
		return nil
	}

	// 4. Get tag definitions
	defs, err := s.tagDefSvc.GetAllEnabled()
	if err != nil {
		return fmt.Errorf("get tag definitions: %w", err)
	}

	// 5. Build prompt and call LLM
	chatJID := s.getChatJID(chatID)
	if chatJID == "" {
		return fmt.Errorf("chat JID not found for chat_id=%d", chatID)
	}
	prompt := s.buildPrompt(defs, existing.Summary, history, newMsgs, chatJID)

	// 從 system_configs 讀取動態模型設定，fallback 硬編碼預設值
	analysisModel := defaultLLMModel
	if s.configSvc != nil {
		if m, _ := s.configSvc.GetConfig("llm.analysis_model"); m != "" {
			analysisModel = m
		}
	}

	resp, err := s.llmClient.ChatCompletionWithModel(ctx, analysisModel, []llm.Message{
		{Role: "system", Content: systemPrompt()},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return fmt.Errorf("llm call: %w", err)
	}

	// 6. Parse result
	result, err := parseResult(resp)
	if err != nil {
		return fmt.Errorf("parse result: %w", err)
	}

	// 7. Persist in transaction
	lastMsgID := newMsgs[len(newMsgs)-1].ID
	return s.db.Transaction(func(tx *gorm.DB) error {
		// Delete existing AI tags for this chat
		if err := tx.Where("chat_id = ? AND account_id = ? AND source = ?",
			chatJID, accountID, "ai").
			Delete(&model.ChatTag{}).Error; err != nil {
			return err
		}

		// Build new tags
		var tags []model.ChatTag
		chatIDStr := chatJID
		if result.Relationship != "" {
			tags = append(tags, model.ChatTag{
				ChatID:    chatIDStr,
				AccountID: accountID,
				Tag:       result.Relationship,
				Category:  "relationship",
				Source:    "ai",
			})
		}
		for _, t := range result.Topics {
			tags = append(tags, model.ChatTag{
				ChatID:    chatIDStr,
				AccountID: accountID,
				Tag:       t,
				Category:  "topic",
				Source:    "ai",
			})
		}
		if len(tags) > 0 {
			if err := tx.Create(&tags).Error; err != nil {
				return err
			}
		}

		// Upsert summary
		summary := model.ChatAISummary{
			ChatID:        chatID,
			AccountID:     accountID,
			Summary:       result.Summary,
			LastMessageID: lastMsgID,
			LastAnalyzed:  time.Now(),
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "chat_id"}, {Name: "account_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"summary", "last_message_id", "last_analyzed", "updated_at"}),
		}).Create(&summary).Error
	})
}

func (s *chatAIAnalysisService) buildPrompt(
	defs []model.AiTagDefinition,
	existingSummary string,
	history []model.WhatsAppMessage,
	newMsgs []model.WhatsAppMessage,
	chatJID string,
) string {
	var b strings.Builder

	// Tag definitions grouped by category
	b.WriteString("## 候選標籤\n\n")
	grouped := make(map[string][]model.AiTagDefinition)
	for _, d := range defs {
		grouped[d.Category] = append(grouped[d.Category], d)
	}
	for cat, items := range grouped {
		b.WriteString(fmt.Sprintf("### %s\n", cat))
		for _, d := range items {
			b.WriteString(fmt.Sprintf("- `%s`（%s）：%s\n", d.Key, d.Label, d.Description))
		}
		b.WriteString("\n")
	}

	// Existing summary
	if existingSummary != "" {
		b.WriteString("## 先前摘要\n\n")
		b.WriteString(existingSummary)
		b.WriteString("\n\n")
	}

	// History messages (reverse to chronological order)
	if len(history) > 0 {
		b.WriteString("## 歷史訊息（舊→新）\n\n")
		for i := len(history) - 1; i >= 0; i-- {
			m := history[i]
			direction := "對方"
			if m.IsFromMe {
				direction = "我方"
			}
			b.WriteString(fmt.Sprintf("[%s] %s: %s\n", m.Timestamp.Format("01/02 15:04"), direction, m.Content))
		}
		b.WriteString("\n")
	}

	// New messages
	b.WriteString("## 新訊息\n\n")
	for _, m := range newMsgs {
		direction := "對方"
		if m.IsFromMe {
			direction = "我方"
		}
		b.WriteString(fmt.Sprintf("[%s] %s: %s\n", m.Timestamp.Format("01/02 15:04"), direction, m.Content))
	}
	b.WriteString("\n")

	b.WriteString("## 輸出格式\n\n請回傳 JSON：\n```json\n{\"relationship\": \"<key>\", \"topics\": [\"<key>\", ...], \"summary\": \"<摘要>\"}\n```\n")

	return b.String()
}

func systemPrompt() string {
	return `你是一位對話分析助手。分析聊天記錄，判斷帳號主人與對方的關係、對話的主題性質。

判斷依據：
- relationship：根據稱呼方式、語氣親疏、話題內容判斷雙方關係
- topics：對話涉及哪些主題（可多選），特別注意是否有金錢相關內容（借貸、轉帳、還款等）
- summary：概括對話重點，包含關鍵事件和雙方互動模式

規則：
1. relationship 只能選 1 個
2. topics 選 1～3 個
3. 只能從候選列表中選擇，不可自創
4. summary 用 3-5 句話概括對話重點與脈絡，使用中文
5. 只回傳 JSON，不要加任何解釋`
}

func parseResult(raw string) (*analysisResult, error) {
	// Strip markdown code blocks if present
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		// Remove opening fence (possibly ```json)
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		// Remove closing fence
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}

	var result analysisResult
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil, fmt.Errorf("unmarshal JSON %q: %w", s, err)
	}
	return &result, nil
}

func (s *chatAIAnalysisService) getChatJID(chatID uint) string {
	var chat model.WhatsAppChat
	if err := s.db.Select("jid").First(&chat, chatID).Error; err != nil {
		return ""
	}
	return chat.JID
}
