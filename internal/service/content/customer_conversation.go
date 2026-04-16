package content

import (
	"time"
	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// CustomerConversationService 客户咨询对话服务接口
type CustomerConversationService interface {
	// 记录对话
	RecordConversation(conversation *model.CustomerConversation) error

	// 获取对话列表
	GetConversationList(page, limit int, filters map[string]interface{}) ([]model.CustomerConversation, int64, error)

	// 获取会话详情（某个用户的所有对话）
	GetSessionConversations(sessionID string) ([]model.CustomerConversation, error)

	// 获取统计数据
	GetStats(startDate, endDate *time.Time) (*model.CustomerConversationStats, error)

	// 获取关键词匹配统计
	GetKeywordMatchStats(startDate, endDate *time.Time, limit int) ([]model.KeywordMatchStats, error)

	// 获取活跃会话列表
	GetActiveSessions(limit, offset int) ([]model.CustomerSession, int64, error)

	// 管理员回复
	AdminReply(sessionID, content string, adminID uint, adminName string) (*model.CustomerConversation, error)
}

type customerConversationService struct {
	db *gorm.DB
}

// NewCustomerConversationService 创建客户咨询对话服务实例
func NewCustomerConversationService(db *gorm.DB) CustomerConversationService {
	return &customerConversationService{db: db}
}

// RecordConversation 记录对话
func (s *customerConversationService) RecordConversation(conversation *model.CustomerConversation) error {
	return s.db.Create(conversation).Error
}

// GetConversationList 获取对话列表
func (s *customerConversationService) GetConversationList(page, limit int, filters map[string]interface{}) ([]model.CustomerConversation, int64, error) {
	var conversations []model.CustomerConversation
	var total int64

	query := s.db.Model(&model.CustomerConversation{})

	// 按用户标识筛选
	if userIdentifier, ok := filters["user_identifier"].(string); ok && userIdentifier != "" {
		query = query.Where("user_identifier = ?", userIdentifier)
	}

	// 按会话ID筛选
	if sessionID, ok := filters["session_id"].(string); ok && sessionID != "" {
		query = query.Where("session_id = ?", sessionID)
	}

	// 按是否匹配筛选
	if isMatched, ok := filters["is_matched"].(bool); ok {
		query = query.Where("is_matched = ?", isMatched)
	}

	// 按关键词ID筛选
	if keywordID, ok := filters["keyword_id"].(uint); ok && keywordID > 0 {
		query = query.Where("matched_keyword_id = ?", keywordID)
	}

	// 按时间范围筛选
	if startDate, ok := filters["start_date"].(time.Time); ok {
		query = query.Where("created_at >= ?", startDate)
	}
	if endDate, ok := filters["end_date"].(time.Time); ok {
		query = query.Where("created_at <= ?", endDate)
	}

	// 搜索用户消息
	if search, ok := filters["search"].(string); ok && search != "" {
		query = query.Where("user_message ILIKE ? OR bot_reply ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * limit
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&conversations).Error; err != nil {
		return nil, 0, err
	}

	return conversations, total, nil
}

// GetSessionConversations 获取会话详情
func (s *customerConversationService) GetSessionConversations(sessionID string) ([]model.CustomerConversation, error) {
	var conversations []model.CustomerConversation

	err := s.db.Where("session_id = ?", sessionID).
		Order("created_at ASC").
		Find(&conversations).Error

	return conversations, err
}

// GetStats 获取统计数据
func (s *customerConversationService) GetStats(startDate, endDate *time.Time) (*model.CustomerConversationStats, error) {
	stats := &model.CustomerConversationStats{}

	query := s.db.Model(&model.CustomerConversation{})

	// 应用时间范围
	if startDate != nil {
		query = query.Where("created_at >= ?", startDate)
	}
	if endDate != nil {
		query = query.Where("created_at <= ?", endDate)
	}

	// 总对话数
	if err := query.Count(&stats.TotalConversations).Error; err != nil {
		return nil, err
	}

	// 咨询人数（去重）
	if err := query.Distinct("user_identifier").Count(&stats.TotalUsers).Error; err != nil {
		return nil, err
	}

	// 匹配数量
	var matchedCount int64
	if err := query.Where("is_matched = ?", true).Count(&matchedCount).Error; err != nil {
		return nil, err
	}

	// 匹配率
	if stats.TotalConversations > 0 {
		stats.MatchedRate = float64(matchedCount) / float64(stats.TotalConversations) * 100
	}

	// 今日对话数
	today := time.Now().Truncate(24 * time.Hour)
	if err := s.db.Model(&model.CustomerConversation{}).
		Where("created_at >= ?", today).
		Count(&stats.TodayConversations).Error; err != nil {
		return nil, err
	}

	// 今日咨询人数
	if err := s.db.Model(&model.CustomerConversation{}).
		Where("created_at >= ?", today).
		Distinct("user_identifier").
		Count(&stats.TodayUsers).Error; err != nil {
		return nil, err
	}

	return stats, nil
}

// GetKeywordMatchStats 获取关键词匹配统计
func (s *customerConversationService) GetKeywordMatchStats(startDate, endDate *time.Time, limit int) ([]model.KeywordMatchStats, error) {
	var stats []model.KeywordMatchStats

	query := s.db.Table("customer_conversations").
		Select("matched_keyword_id as keyword_id, COUNT(*) as match_count").
		Where("is_matched = ? AND matched_keyword_id IS NOT NULL", true).
		Group("matched_keyword_id")

	// 应用时间范围
	if startDate != nil {
		query = query.Where("created_at >= ?", startDate)
	}
	if endDate != nil {
		query = query.Where("created_at <= ?", endDate)
	}

	query = query.Order("match_count DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Scan(&stats).Error; err != nil {
		return nil, err
	}

	// 获取总匹配数用于计算百分比
	var totalMatched int64
	countQuery := s.db.Model(&model.CustomerConversation{}).
		Where("is_matched = ? AND matched_keyword_id IS NOT NULL", true)

	if startDate != nil {
		countQuery = countQuery.Where("created_at >= ?", startDate)
	}
	if endDate != nil {
		countQuery = countQuery.Where("created_at <= ?", endDate)
	}

	if err := countQuery.Count(&totalMatched).Error; err != nil {
		return nil, err
	}

	// 获取关键词名称并计算百分比
	for i := range stats {
		var keyword model.AutoReplyKeyword
		if err := s.db.First(&keyword, stats[i].KeywordID).Error; err == nil {
			if len(keyword.Keywords) > 0 {
				stats[i].KeywordName = keyword.Keywords[0]
			}
		}

		if totalMatched > 0 {
			stats[i].Percentage = float64(stats[i].MatchCount) / float64(totalMatched) * 100
		}
	}

	return stats, nil
}

// GetActiveSessions 获取活跃会话列表
func (s *customerConversationService) GetActiveSessions(limit, offset int) ([]model.CustomerSession, int64, error) {
	var sessions []model.CustomerSession

	subQuery := s.db.Model(&model.CustomerConversation{}).
		Select("session_id, MAX(created_at) as last_time").
		Group("session_id")

	baseQuery := s.db.Table("customer_conversations as c").
		Select(`
			c.session_id,
			c.user_identifier,
			CASE
				WHEN c.is_admin_reply THEN c.bot_reply
				ELSE c.user_message
			END as last_message,
			c.created_at as last_time,
			(SELECT COUNT(*) FROM customer_conversations WHERE session_id = c.session_id) as message_count,
			COALESCE((SELECT MAX(created_at) FROM customer_conversations WHERE session_id = c.session_id AND is_admin_reply = true), '1970-01-01') >
				COALESCE((SELECT MAX(created_at) FROM customer_conversations WHERE session_id = c.session_id AND is_admin_reply = false AND user_message != ''), '1970-01-01') as has_admin_reply,
			EXISTS(SELECT 1 FROM customer_conversations WHERE session_id = c.session_id AND needs_human = true) as needs_human
		`).
		Joins("INNER JOIN (?) as latest ON c.session_id = latest.session_id AND c.created_at = latest.last_time", subQuery).
		Order("has_admin_reply ASC, c.created_at DESC")

	var total int64
	if err := s.db.Table("(?) as t", baseQuery).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query := baseQuery
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Scan(&sessions).Error; err != nil {
		return nil, 0, err
	}

	return sessions, total, nil
}

// AdminReply 管理员回复
func (s *customerConversationService) AdminReply(sessionID, content string, adminID uint, adminName string) (*model.CustomerConversation, error) {
	// 获取该会话的用户标识
	var existingConv model.CustomerConversation
	if err := s.db.Where("session_id = ?", sessionID).First(&existingConv).Error; err != nil {
		return nil, err
	}

	// 创建管理员回复记录
	conversation := &model.CustomerConversation{
		SessionID:      sessionID,
		UserIdentifier: existingConv.UserIdentifier,
		UserMessage:    "",
		BotReply:       content,
		IsAdminReply:   true,
		AdminID:        &adminID,
		AdminName:      adminName,
		IsMatched:      false,
	}

	if err := s.db.Create(conversation).Error; err != nil {
		return nil, err
	}

	return conversation, nil
}
