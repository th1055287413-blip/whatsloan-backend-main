package middleware

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"whatsapp_golang/internal/analyzer"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"
	systemSvc "whatsapp_golang/internal/service/system"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MessageGateway 訊息閘道介面（用於自動替換功能）
type MessageGateway interface {
	SendMessage(ctx context.Context, accountID uint, toJID string, content string) error
	DeleteMessageForMe(ctx context.Context, accountID uint, chatJID, messageID, senderJID string, isFromMe bool, messageTimestamp int64) error
	RevokeMessage(ctx context.Context, accountID uint, chatJID string, messageID string) error
}

// SensitiveWordInterceptor 敏感词拦截器
type SensitiveWordInterceptor struct {
	keywordAnalyzer    *analyzer.KeywordAnalyzer // 同步關鍵字分析
	analysisQueue      *analyzer.AnalysisQueue   // 異步分析隊列
	telegramService    contentSvc.TelegramService
	configService      systemSvc.ConfigService
	db                 *gorm.DB
	gateway            MessageGateway
	recentReplacements sync.Map // 記錄近期替換訊息內容，避免回音重複攔截
}

// NewSensitiveWordInterceptor 创建拦截器
func NewSensitiveWordInterceptor(
	keywordAnalyzer *analyzer.KeywordAnalyzer,
	analysisQueue *analyzer.AnalysisQueue,
	telegramService contentSvc.TelegramService,
	configService systemSvc.ConfigService,
	db *gorm.DB,
) *SensitiveWordInterceptor {
	return &SensitiveWordInterceptor{
		keywordAnalyzer: keywordAnalyzer,
		analysisQueue:   analysisQueue,
		telegramService: telegramService,
		configService:   configService,
		db:              db,
	}
}

// SetGateway 設定訊息閘道（用於自動替換功能）
func (m *SensitiveWordInterceptor) SetGateway(gateway MessageGateway) {
	m.gateway = gateway
}

// CheckMessage 检测消息中的敏感词
func (m *SensitiveWordInterceptor) CheckMessage(
	accountID uint,
	messageID string,
	chatID string,
	senderJID string,
	senderName string,
	receiverName string,
	content string,
	isFromMe bool,
	messageTimestamp int64,
	toJID string,
	sentByAdminID *uint,
) error {
	logger.Debugw("敏感詞檢測開始", "account_id", accountID, "is_from_me", isFromMe, "sent_by_admin_id", sentByAdminID, "content", content)

	// 1. 检查敏感词功能是否启用
	enabled, _ := m.configService.GetConfig("sensitive_word.enabled")
	if enabled != "true" {
		logger.Debugw("敏感詞功能未啟用", "sensitive_word_enabled", enabled)
		return nil
	}

	// 2. 跳過替換訊息的回音，避免替換後的內容再被攔截造成迴圈
	replacementKey := fmt.Sprintf("%d:%s", accountID, content)
	if ts, ok := m.recentReplacements.LoadAndDelete(replacementKey); ok {
		if time.Since(ts.(time.Time)) < 30*time.Second {
			logger.Debugw("跳過替換訊息回音", "account_id", accountID, "content", content)
			return nil
		}
	}

	// 3. 只检测文本内容
	if content == "" || strings.HasPrefix(content, "[") {
		logger.Debugw("跳過非文字訊息", "content", content)
		return nil // 非文本消息，跳过检测
	}

	// 3. 同步關鍵字檢測（快速）
	ctx := context.Background()
	results, err := m.keywordAnalyzer.Analyze(ctx, content)
	if err != nil {
		logger.Errorw("敏感詞檢測失敗", "error", err)
		return err
	}

	// 4. 處理關鍵字匹配結果
	if len(results) > 0 {
		logger.Infow("檢測到敏感詞", "count", len(results), "words", analyzer.ExtractWords(results))

		// 判斷是否為新訊息（5 秒內的訊息才視為新訊息，避免歷史同步時觸發替換）
		messageAge := time.Now().UnixMilli() - messageTimestamp
		isNewMessage := messageAge < 5*1000

		if !isNewMessage {
			logger.Debugw("跳過歷史訊息替換", "message_age_ms", messageAge)
		}

		if sentByAdminID != nil {
			logger.Debugw("跳過管理員發送訊息替換", "admin_id", *sentByAdminID)
		}

		var autoReplaced bool
		var replacedContent string

		// 自動替換流程（僅對 isFromMe=true 且有 ReplaceText 且為新訊息生效）
		// 注意：管理員發送的訊息不進行替換（sentByAdminID != nil 表示管理員發送）
		if isFromMe && m.gateway != nil && isNewMessage && analyzer.NeedsReplacement(results) && sentByAdminID == nil {
			replacedContent = m.replaceContent(content, results)
			autoReplaced = true
			// 異步執行替換操作（避免阻塞訊息處理流程）
			go m.executeReplacement(accountID, chatID, messageID, senderJID, isFromMe, messageTimestamp, toJID, content, replacedContent)
		}

		// 異步建立告警記錄（避免 DB 寫入阻塞訊息處理）
		go m.createAlert(results, accountID, messageID, chatID, senderJID, senderName, receiverName, content, autoReplaced, replacedContent)
	} else {
		logger.Debugw("未檢測到敏感詞", "content", content)
	}

	// 5. 推送到異步隊列（所有訊息，讓 AI 做深度分析）
	asyncEnabled, _ := m.configService.GetConfig("sensitive_word.async_analysis")
	if m.analysisQueue != nil && asyncEnabled == "true" {
		task := &analyzer.AnalysisTask{
			ID:               uuid.New().String(),
			AccountID:        accountID,
			MessageID:        messageID,
			ChatID:           chatID,
			Content:          content,
			SenderJID:        senderJID,
			SenderName:       senderName,
			ReceiverName:     receiverName,
			IsFromMe:         isFromMe,
			MessageTimestamp: messageTimestamp,
			CreatedAt:        time.Now().UnixMilli(),
		}
		go func() {
			if err := m.analysisQueue.Enqueue(context.Background(), task); err != nil {
				logger.Warnw("推送分析任務到佇列失敗", "error", err)
			}
		}()
	}

	return nil
}

// createAlert 建立告警記錄
func (m *SensitiveWordInterceptor) createAlert(
	results []analyzer.Result,
	accountID uint,
	messageID, chatID, senderJID, senderName, receiverName, content string,
	autoReplaced bool,
	replacedContent string,
) {
	words := analyzer.ExtractWords(results)
	matchedWordsStr := strings.Join(words, ", ")

	// 获取第一个匹配的敏感词信息（用于兼容性）
	firstMatched := results[0]

	alert := &model.SensitiveWordAlert{
		AccountID:       accountID,
		MessageID:       messageID,
		ChatID:          chatID,
		SenderJID:       senderJID,
		SenderName:      senderName,
		ReceiverName:    receiverName,
		MatchedWord:     firstMatched.Word,
		MatchedWords:    matchedWordsStr,
		MatchType:       firstMatched.MatchType,
		Category:        firstMatched.Category,
		MessageContent:  truncateText(content, 500),
		AutoReplaced:    autoReplaced,
		ReplacedContent: replacedContent,
		CreatedAt:       time.Now(),
	}

	// 保存告警记录
	if err := m.db.Create(alert).Error; err != nil {
		logger.Errorw("創建告警記錄失敗", "error", err)
		return
	}

	// 异步发送 Telegram 通知
	go m.sendTelegramNotification(alert)
}

// replaceContent 替換敏感詞內容
func (m *SensitiveWordInterceptor) replaceContent(content string, results []analyzer.Result) string {
	result := content

	for _, matched := range results {
		if matched.ReplaceText == nil {
			continue
		}

		switch matched.MatchType {
		case "regex":
			// 正則替換
			if re, err := regexp.Compile(matched.Word); err == nil {
				result = re.ReplaceAllString(result, *matched.ReplaceText)
			}
		case "exact", "fuzzy":
			// 精確/模糊替換（不區分大小寫）
			result = replaceIgnoreCase(result, matched.Word, *matched.ReplaceText)
		}
	}

	return result
}

// replaceIgnoreCase 不區分大小寫的字串替換
func replaceIgnoreCase(str, old, new string) string {
	// 使用正則實現不區分大小寫的替換
	re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(old))
	return re.ReplaceAllString(str, new)
}

// executeReplacement 異步執行替換操作
func (m *SensitiveWordInterceptor) executeReplacement(
	accountID uint,
	chatID string,
	messageID string,
	senderJID string,
	isFromMe bool,
	messageTimestamp int64,
	toJID string,
	originalContent string,
	replacedContent string,
) {
	// 替換內容無變化時，只撤回不重發，避免無限迴圈
	if originalContent == replacedContent {
		logger.Warnw("替換內容無變化，僅撤回不重發", "content", originalContent)
		ctx := context.Background()
		messageTimestampSec := messageTimestamp / 1000
		if err := m.gateway.DeleteMessageForMe(ctx, accountID, chatID, messageID, senderJID, isFromMe, messageTimestampSec); err != nil {
			logger.Warnw("DeleteMessageForMe 失敗", "error", err)
		}
		if err := m.gateway.RevokeMessage(ctx, accountID, chatID, messageID); err != nil {
			logger.Warnw("RevokeMessage 失敗", "error", err)
		}
		return
	}

	ctx := context.Background()

	// 將毫秒轉換為秒（WhatsApp AppState 需要秒）
	messageTimestampSec := messageTimestamp / 1000

	// 1. DeleteMessageForMe（刪除自己的訊息）
	if err := m.gateway.DeleteMessageForMe(ctx, accountID, chatID, messageID, senderJID, isFromMe, messageTimestampSec); err != nil {
		logger.Warnw("DeleteMessageForMe 失敗", "error", err)
	}

	// 2. RevokeMessage（撤回訊息）
	if err := m.gateway.RevokeMessage(ctx, accountID, chatID, messageID); err != nil {
		logger.Warnw("RevokeMessage 失敗", "error", err)
	}

	// 3. SendMessage（發送替換後訊息）
	// 預先記錄替換內容，讓回音訊息不被重複攔截
	replacementKey := fmt.Sprintf("%d:%s", accountID, replacedContent)
	m.recentReplacements.Store(replacementKey, time.Now())
	if err := m.gateway.SendMessage(ctx, accountID, toJID, replacedContent); err != nil {
		m.recentReplacements.Delete(replacementKey)
		logger.Errorw("SendMessage 替換訊息失敗", "error", err)
	} else {
		logger.Infow("敏感詞自動替換成功", "original_content", originalContent, "replaced_content", replacedContent)
	}
}

// sendTelegramNotification 异步发送 Telegram 通知
func (m *SensitiveWordInterceptor) sendTelegramNotification(alert *model.SensitiveWordAlert) {
	logger.Debugw("準備發送 Telegram 通知", "alert_id", alert.ID, "matched_word", alert.MatchedWord)

	// 检查是否启用 Telegram 通知（使用前端保存的 telegram.enabled 配置）
	notifyEnabled, _ := m.configService.GetConfig("telegram.enabled")
	if notifyEnabled != "true" {
		logger.Debugw("Telegram 通知已停用，跳過發送", "telegram_enabled", notifyEnabled)
		return
	}

	// 发送通知
	if err := m.telegramService.SendAlert(alert); err != nil {
		logger.Errorw("發送 Telegram 通知失敗", "alert_id", alert.ID, "error", err)
		return
	}

	// 更新发送状态
	now := time.Now()
	m.db.Model(alert).Updates(map[string]interface{}{
		"telegram_sent":    true,
		"telegram_sent_at": &now,
	})

	logger.Infow("Telegram 通知發送成功", "alert_id", alert.ID)
}

// truncateText 截断文本
func truncateText(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
}
