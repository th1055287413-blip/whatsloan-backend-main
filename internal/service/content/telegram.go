package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
)

// TelegramService Telegram 服务接口
type TelegramService interface {
	SendAlert(alert *model.SensitiveWordAlert) error
	TestConnection() error
	GetBotInfo() (*BotInfo, error)
}

// BotInfo Bot 信息
type BotInfo struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

// ConfigGetter 配置获取接口（避免循环依赖）
type ConfigGetter interface {
	GetConfig(key string) (string, error)
}

// telegramService 实现
type telegramService struct {
	configGetter ConfigGetter
	client       *http.Client
}

// NewTelegramService 创建服务实例
func NewTelegramService(configGetter ConfigGetter, timeoutSec int) TelegramService {
	if timeoutSec <= 0 {
		timeoutSec = 10 // 預設 10 秒
	}
	return &telegramService{
		configGetter: configGetter,
		client: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
	}
}

// getConfig 动态获取 Telegram 配置
func (s *telegramService) getConfig() (botToken, chatID string) {
	if s.configGetter == nil {
		return "", ""
	}
	botToken, _ = s.configGetter.GetConfig("telegram.bot_token")
	chatID, _ = s.configGetter.GetConfig("telegram.chat_id")
	return botToken, chatID
}

// SendAlert 发送告警
func (s *telegramService) SendAlert(alert *model.SensitiveWordAlert) error {
	botToken, chatID := s.getConfig()
	if botToken == "" || chatID == "" {
		return fmt.Errorf("Telegram 配置未完成: botToken=%v, chatID=%v", botToken != "", chatID != "")
	}

	// 使用 MatchedWords 如果有值，否则使用 MatchedWord
	matchedWordsDisplay := alert.MatchedWords
	if matchedWordsDisplay == "" {
		matchedWordsDisplay = alert.MatchedWord
	}

	// 构建消息
	message := fmt.Sprintf(
		"🚨 <b>敏感词告警</b>\n\n"+
			"📱 <b>发送者:</b> %s\n"+
			"📲 <b>接收账号:</b> %s\n"+
			"⚠️ <b>敏感词:</b> <code>%s</code>\n"+
			"📝 <b>消息内容:</b>\n<pre>%s</pre>\n"+
			"🕐 <b>时间:</b> %s",
		escapeHTML(alert.SenderName),
		escapeHTML(alert.ReceiverName),
		escapeHTML(matchedWordsDisplay),
		escapeHTML(truncateText(alert.MessageContent, 200)),
		alert.CreatedAt.Format("2006-01-02 15:04:05"),
	)

	return s.sendMessageWithConfig(message, botToken, chatID)
}

// sendMessageWithConfig 使用指定配置发送消息
func (s *telegramService) sendMessageWithConfig(text, botToken, chatID string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := s.client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		return fmt.Errorf("Telegram API 返回错误状态码: %d, 响应: %v", resp.StatusCode, result)
	}

	logger.Info("Telegram 通知發送成功")
	return nil
}

// TestConnection 测试连接
func (s *telegramService) TestConnection() error {
	_, err := s.GetBotInfo()
	return err
}

// GetBotInfo 获取 Bot 信息
func (s *telegramService) GetBotInfo() (*BotInfo, error) {
	botToken, _ := s.getConfig()
	if botToken == "" {
		return nil, fmt.Errorf("Bot Token 未配置")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", botToken)

	resp, err := s.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool    `json:"ok"`
		Result BotInfo `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.OK {
		return nil, fmt.Errorf("Telegram API 返回失败")
	}

	return &result.Result, nil
}

// escapeHTML HTML 转义
func escapeHTML(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

// truncateText 截断文本
func truncateText(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
}
