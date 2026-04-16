package messaging

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"whatsapp_golang/internal/database"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// MessageSearchService 消息搜索服务接口
type MessageSearchService interface {
	SearchMessages(ctx context.Context, req *model.MessageSearchRequest) (*model.MessageSearchResult, error)
	GetMessageContext(ctx context.Context, messageID uint, before, after int) (*model.MessageContextResult, error)
}

// messageSearchService 消息搜索服务实现
type messageSearchService struct {
	db database.Database
}

// NewMessageSearchService 创建消息搜索服务
func NewMessageSearchService(db database.Database) MessageSearchService {
	return &messageSearchService{
		db: db,
	}
}

// SearchMessages 搜索消息
func (s *messageSearchService) SearchMessages(ctx context.Context, req *model.MessageSearchRequest) (*model.MessageSearchResult, error) {
	// 1. 验证参数
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("参数验证失败: %w", err)
	}

	// 2. 构建基础查询
	query := s.buildSearchQuery(req)

	// 3. 查询总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Ctx(ctx).Errorw("查詢訊息總數失敗", "error", err)
		return nil, fmt.Errorf("查询总数失败: %w", err)
	}

	// 4. 查询消息列表
	var messages []model.WhatsAppMessage
	err := query.
		Order(fmt.Sprintf("timestamp %s", strings.ToUpper(req.SortOrder))).
		Limit(req.Limit).
		Offset(req.Offset).
		Find(&messages).Error
	if err != nil {
		logger.Ctx(ctx).Errorw("查詢訊息列表失敗", "error", err)
		return nil, fmt.Errorf("查询消息失败: %w", err)
	}

	// 5. 转换为搜索结果
	results := s.convertToSearchItems(messages, req.Keyword)

	// 6. 计算页码
	page := 1
	if req.Limit > 0 {
		page = req.Offset/req.Limit + 1
	}

	// 7. 返回结果
	return &model.MessageSearchResult{
		Total:    int(total),
		Page:     page,
		PageSize: req.Limit,
		Results:  results,
	}, nil
}

// buildSearchQuery 构建搜索查询
func (s *messageSearchService) buildSearchQuery(req *model.MessageSearchRequest) *gorm.DB {
	// 使用 GORM 的 Session 方法创建新查询,避免影响其他查询
	query := s.db.GetDB().Session(&gorm.Session{}).Model(&model.WhatsAppMessage{})

	// 不過濾已刪除的訊息，讓 admin 可以看到並標示出來

	// 添加账号筛选 (AccountID=0表示搜索所有账号)
	if req.AccountID > 0 {
		query = query.Where("account_id = ?", req.AccountID)
	}

	// 添加关键词搜索 (使用 ILIKE 支持模糊搜索,性能由 pg_trgm 索引保证)
	// 支持多个关键词,用逗号分隔
	keywords := strings.Split(req.Keyword, ",")
	if len(keywords) > 0 {
		var keywordConditions []string
		var keywordArgs []interface{}
		for _, k := range keywords {
			k = strings.TrimSpace(k)
			if k != "" {
				keywordConditions = append(keywordConditions, "content ILIKE ?")
				keywordArgs = append(keywordArgs, "%"+k+"%")
			}
		}
		if len(keywordConditions) > 0 {
			query = query.Where(strings.Join(keywordConditions, " OR "), keywordArgs...)
		}
	}

	// 添加可选筛选条件
	if req.ChatJID != "" {
		// 通过 chat_jid 查找 chat_id
		var chat model.WhatsAppChat
		chatQuery := s.db.GetDB().Where("jid = ?", req.ChatJID)
		// 如果指定了账号,也要过滤账号
		if req.AccountID > 0 {
			chatQuery = chatQuery.Where("account_id = ?", req.AccountID)
		}
		if err := chatQuery.First(&chat).Error; err == nil {
			query = query.Where("chat_id = ?", chat.ID)
		} else {
			logger.Warnw("未找到對話 JID，搜尋結果可能為空", "chat_jid", req.ChatJID)
		}
	}

	if req.MessageType != "all" {
		query = query.Where("type = ?", req.MessageType)
	}

	if req.DateFrom != "" {
		if t, err := time.Parse(time.RFC3339, req.DateFrom); err == nil {
			query = query.Where("timestamp >= ?", t)
		}
	}

	if req.DateTo != "" {
		if t, err := time.Parse(time.RFC3339, req.DateTo); err == nil {
			query = query.Where("timestamp <= ?", t)
		}
	}

	if req.IsFromMe != nil {
		query = query.Where("is_from_me = ?", *req.IsFromMe)
	}

	return query
}

// convertToSearchItems 转换为搜索结果项
func (s *messageSearchService) convertToSearchItems(messages []model.WhatsAppMessage, keyword string) []model.MessageSearchItem {
	results := make([]model.MessageSearchItem, 0, len(messages))

	// 处理关键词列表
	var keywords []string
	for _, k := range strings.Split(keyword, ",") {
		k = strings.TrimSpace(k)
		if k != "" {
			keywords = append(keywords, k)
		}
	}

	for _, msg := range messages {
		// 查询对话信息
		var chat model.WhatsAppChat
		if err := s.db.GetDB().First(&chat, msg.ChatID).Error; err != nil {
			logger.Warnw("查詢對話失敗", "chat_id", msg.ChatID, "error", err)
			chat.Name = "未知对话"
			chat.JID = ""
		}

		// 查询账号信息
		var account model.WhatsAppAccount
		accountPhone := ""
		if err := s.db.GetDB().First(&account, msg.AccountID).Error; err != nil {
			logger.Warnw("查詢帳號失敗", "account_id", msg.AccountID, "error", err)
		} else {
			accountPhone = account.PhoneNumber
		}

		// 判断是否为群组
		isGroupChat := strings.HasSuffix(chat.JID, "@g.us")

		// 确定发送者名称
		fromName := ""
		if msg.IsFromMe {
			// 我发送的消息,发送者是账号本身
			fromName = accountPhone
		} else {
			// 对方发送的消息,发送者是聊天名称
			fromName = chat.Name
		}

		results = append(results, model.MessageSearchItem{
			ID:             msg.ID,
			AccountID:      msg.AccountID,
			AccountPhone:   accountPhone,
			ChatID:         msg.ChatID,
			ChatJID:        chat.JID,
			ChatName:       chat.Name,
			IsGroupChat:    isGroupChat,
			MessageID:      msg.MessageID,
			FromJID:        msg.FromJID,
			FromName:       fromName,
			ToJID:          msg.ToJID,
			Content:        msg.Content,
			Type:           msg.Type,
			MediaURL:       msg.MediaURL,
			Timestamp:      msg.Timestamp,
			IsFromMe:       msg.IsFromMe,
			IsRead:         msg.IsRead,
			SendStatus:     msg.SendStatus,
			MatchedSnippet: generateSnippet(msg.Content, keywords),
			CreatedAt:      msg.CreatedAt,
		})
	}

	return results
}

// generateSnippet 生成匹配片段,高亮关键词
func generateSnippet(content string, keywords []string) string {
	if content == "" || len(keywords) == 0 {
		return content
	}

	// 找到第一个匹配的关键词位置 (不区分大小写)
	lowerContent := strings.ToLower(content)
	firstIndex := -1

	for _, keyword := range keywords {
		if keyword == "" {
			continue
		}
		lowerKeyword := strings.ToLower(keyword)
		index := strings.Index(lowerContent, lowerKeyword)
		if index != -1 {
			if firstIndex == -1 || index < firstIndex {
				firstIndex = index
			}
		}
	}

	if firstIndex == -1 {
		// 未找到关键词,返回前100个字符
		if len(content) <= 100 {
			return content
		}
		return content[:100] + "..."
	}

	// 提取关键词前后各30个字符
	// 使用第一个匹配到的关键词来确定窗口
	// 注意：这里简单取第一个匹配位置，可能不是最优的（比如多个关键词分布很散），但对于摘要显示通常足够
	contextLength := 30
	start := max(0, firstIndex-contextLength)
	// 结束位置需要保证至少包含一段合理的长度，这里简单处理为 start + 100 或者到末尾
	end := min(len(content), start+100)

	// 如果 end 截断了，尝试调整 start 以便显示更多上下文，但这里保持简单逻辑
	if end-start < 60 && len(content) > 60 {
		// 如果截取太短，尝试向后扩展
		end = min(len(content), start+60)
	}

	snippet := content[start:end]

	// 高亮所有关键词 (不区分大小写)
	for _, keyword := range keywords {
		if keyword == "" {
			continue
		}
		// 使用正则表达式进行不区分大小写的替换
		re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(keyword))
		snippet = re.ReplaceAllString(snippet, "<em>$0</em>")
	}

	// 添加省略号
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet = snippet + "..."
	}

	return snippet
}

// GetMessageContext 获取消息上下文
func (s *messageSearchService) GetMessageContext(ctx context.Context, messageID uint, before, after int) (*model.MessageContextResult, error) {
	// 1. 查询目标消息
	var targetMessage model.WhatsAppMessage
	if err := s.db.GetDB().First(&targetMessage, messageID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("消息不存在")
		}
		logger.Ctx(ctx).Errorw("查詢目標訊息失敗", "message_id", messageID, "error", err)
		return nil, fmt.Errorf("查询目标消息失败: %w", err)
	}

	// 2. 查询之前的消息
	var beforeMessages []model.WhatsAppMessage
	err := s.db.GetDB().
		Where("chat_id = ? AND timestamp < ?", targetMessage.ChatID, targetMessage.Timestamp).
		Order("timestamp DESC").
		Limit(before).
		Find(&beforeMessages).Error
	if err != nil {
		logger.Ctx(ctx).Errorw("查詢之前的訊息失敗", "error", err)
		return nil, fmt.Errorf("查询之前的消息失败: %w", err)
	}

	// 反转顺序,使其按时间正序排列
	for i, j := 0, len(beforeMessages)-1; i < j; i, j = i+1, j-1 {
		beforeMessages[i], beforeMessages[j] = beforeMessages[j], beforeMessages[i]
	}

	// 3. 查询之后的消息
	var afterMessages []model.WhatsAppMessage
	err = s.db.GetDB().
		Where("chat_id = ? AND timestamp > ?", targetMessage.ChatID, targetMessage.Timestamp).
		Order("timestamp ASC").
		Limit(after).
		Find(&afterMessages).Error
	if err != nil {
		logger.Ctx(ctx).Errorw("查詢之後的訊息失敗", "error", err)
		return nil, fmt.Errorf("查询之后的消息失败: %w", err)
	}

	// 4. 查询对话信息
	var chat model.WhatsAppChat
	if err := s.db.GetDB().First(&chat, targetMessage.ChatID).Error; err != nil {
		logger.Ctx(ctx).Warnw("查詢對話失敗", "chat_id", targetMessage.ChatID, "error", err)
		chat.Name = "未知对话"
		chat.JID = ""
		chat.IsGroup = false
	}

	// 5. 组装结果
	return &model.MessageContextResult{
		TargetMessage:  targetMessage,
		BeforeMessages: beforeMessages,
		AfterMessages:  afterMessages,
		ChatInfo: model.ChatInfo{
			ChatID:   chat.ID,
			ChatJID:  chat.JID,
			ChatName: chat.Name,
			IsGroup:  chat.IsGroup,
		},
	}, nil
}

// 辅助函数
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
