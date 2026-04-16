package content

import (
	"errors"
	"strings"
	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// AutoReplyService 自动回复服务接口
type AutoReplyService interface {
	// 关键词管理
	CreateKeyword(keyword *model.AutoReplyKeyword) error
	UpdateKeyword(id uint, keyword *model.AutoReplyKeyword) error
	DeleteKeyword(id uint) error
	GetKeyword(id uint) (*model.AutoReplyKeyword, error)
	GetKeywordList(page, limit int, filters map[string]interface{}) ([]model.AutoReplyKeyword, int64, error)
	UpdateKeywordStatus(id uint, status model.AutoReplyStatus) error
	BatchDeleteKeywords(ids []uint) error

	// 公开接口（供客户端使用）
	GetActiveKeywords(language string, pageSize int) ([]model.AutoReplyKeyword, error)
	GetWelcomeMessage(language string) (string, error)
	MatchKeyword(userMessage, language string) (*model.AutoReplyKeyword, string, error)
}

type autoReplyService struct {
	db *gorm.DB
}

// NewAutoReplyService 创建自动回复服务实例
func NewAutoReplyService(db *gorm.DB) AutoReplyService {
	return &autoReplyService{db: db}
}

// CreateKeyword 创建关键词
func (s *autoReplyService) CreateKeyword(keyword *model.AutoReplyKeyword) error {
	if len(keyword.Keywords) == 0 {
		return errors.New("关键词列表不能为空")
	}
	if keyword.Reply == "" {
		return errors.New("回复内容不能为空")
	}

	return s.db.Create(keyword).Error
}

// UpdateKeyword 更新关键词
func (s *autoReplyService) UpdateKeyword(id uint, keyword *model.AutoReplyKeyword) error {
	var existingKeyword model.AutoReplyKeyword
	if err := s.db.First(&existingKeyword, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("关键词不存在")
		}
		return err
	}

	if len(keyword.Keywords) == 0 {
		return errors.New("关键词列表不能为空")
	}
	if keyword.Reply == "" {
		return errors.New("回复内容不能为空")
	}

	return s.db.Model(&existingKeyword).Updates(keyword).Error
}

// DeleteKeyword 删除关键词
func (s *autoReplyService) DeleteKeyword(id uint) error {
	result := s.db.Delete(&model.AutoReplyKeyword{}, id)
	if result.RowsAffected == 0 {
		return errors.New("关键词不存在")
	}
	return result.Error
}

// GetKeyword 获取关键词详情
func (s *autoReplyService) GetKeyword(id uint) (*model.AutoReplyKeyword, error) {
	var keyword model.AutoReplyKeyword
	if err := s.db.First(&keyword, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("关键词不存在")
		}
		return nil, err
	}
	return &keyword, nil
}

// GetKeywordList 获取关键词列表
func (s *autoReplyService) GetKeywordList(page, limit int, filters map[string]interface{}) ([]model.AutoReplyKeyword, int64, error) {
	var keywords []model.AutoReplyKeyword
	var total int64

	query := s.db.Model(&model.AutoReplyKeyword{})

	// 按搜索关键词筛选
	if searchQuery, ok := filters["query"].(string); ok && searchQuery != "" {
		query = query.Where("reply ILIKE ?", "%"+searchQuery+"%")
	}

	// 按匹配类型筛选
	if matchType, ok := filters["match_type"].(model.AutoReplyMatchType); ok && matchType != "" {
		query = query.Where("match_type = ?", matchType)
	}

	// 按状态筛选
	if status, ok := filters["status"].(model.AutoReplyStatus); ok && status != "" {
		query = query.Where("status = ?", status)
	}

	// 按语言筛选
	if language, ok := filters["language"].(string); ok && language != "" {
		query = query.Where("language = ?", language)
	}

	// 按关键词类型筛选
	if keywordType, ok := filters["keyword_type"].(string); ok && keywordType != "" {
		query = query.Where("keyword_type = ?", keywordType)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * limit
	if err := query.Order("priority DESC, created_at DESC").Offset(offset).Limit(limit).Find(&keywords).Error; err != nil {
		return nil, 0, err
	}

	return keywords, total, nil
}

// UpdateKeywordStatus 更新关键词状态
func (s *autoReplyService) UpdateKeywordStatus(id uint, status model.AutoReplyStatus) error {
	result := s.db.Model(&model.AutoReplyKeyword{}).Where("id = ?", id).Update("status", status)
	if result.RowsAffected == 0 {
		return errors.New("关键词不存在")
	}
	return result.Error
}

// BatchDeleteKeywords 批量删除关键词
func (s *autoReplyService) BatchDeleteKeywords(ids []uint) error {
	if len(ids) == 0 {
		return errors.New("未选择要删除的关键词")
	}

	return s.db.Where("id IN ?", ids).Delete(&model.AutoReplyKeyword{}).Error
}

// GetActiveKeywords 获取激活的关键词（供客户端使用）
func (s *autoReplyService) GetActiveKeywords(language string, pageSize int) ([]model.AutoReplyKeyword, error) {
	var keywords []model.AutoReplyKeyword

	query := s.db.Where("status = ?", model.AutoReplyStatusActive)

	// 如果指定语言，则筛选
	if language != "" {
		query = query.Where("language = ?", language)
	}

	// 按优先级排序，限制数量
	if pageSize > 0 {
		query = query.Limit(pageSize)
	}

	if err := query.Order("priority DESC, created_at DESC").Find(&keywords).Error; err != nil {
		return nil, err
	}

	return keywords, nil
}

// GetWelcomeMessage 获取欢迎语（根据语言）
func (s *autoReplyService) GetWelcomeMessage(language string) (string, error) {
	var welcomeKeyword model.AutoReplyKeyword

	// 先尝试匹配指定语言的欢迎语
	err := s.db.Where("status = ? AND keyword_type = ? AND language = ?",
		model.AutoReplyStatusActive,
		model.AutoReplyKeywordTypeWelcome,
		language).
		Order("priority DESC").
		First(&welcomeKeyword).Error

	if err == nil {
		return welcomeKeyword.Reply, nil
	}

	// 如果没有匹配到，尝试获取任意语言的欢迎语
	err = s.db.Where("status = ? AND keyword_type = ?",
		model.AutoReplyStatusActive,
		model.AutoReplyKeywordTypeWelcome).
		Order("priority DESC").
		First(&welcomeKeyword).Error

	if err == nil {
		return welcomeKeyword.Reply, nil
	}

	// 没有配置欢迎语
	return "", nil
}

// MatchKeyword 匹配关键词并返回回复（不区分语言）
func (s *autoReplyService) MatchKeyword(userMessage, language string) (*model.AutoReplyKeyword, string, error) {
	if userMessage == "" {
		return nil, "", nil
	}

	normalizedMessage := strings.ToLower(strings.TrimSpace(userMessage))

	// 优先检测人工客服关键词（多语言支持）
	humanKeywords := []string{
		"人工", "客服", "转人工", "人工客服", "真人", "manual", "agent", "human",
		"customer service", "representative", "operator", "support",
		"manusia", "layanan pelanggan", "petugas", // 印尼语
		"perkhidmatan pelanggan", "ejen", // 马来语
	}

	for _, kw := range humanKeywords {
		if strings.Contains(normalizedMessage, strings.ToLower(kw)) {
			// 检测到人工关键词，返回特殊标记
			// 这里返回 nil keyword，但 reply 包含特殊前缀
			humanReply := getHumanTransferMessage(language)
			return nil, "NEEDS_HUMAN:" + humanReply, nil
		}
	}

	// 查询所有激活的关键词（不区分语言和类型）
	var keywords []model.AutoReplyKeyword
	query := s.db.Where("status = ? AND keyword_type IN ?",
		model.AutoReplyStatusActive,
		[]model.AutoReplyKeywordType{model.AutoReplyKeywordTypeNormal, model.AutoReplyKeywordTypeWelcome})

	if err := query.Order("priority DESC, created_at DESC").Find(&keywords).Error; err != nil {
		return nil, "", err
	}

	// 匹配关键词
	for _, keyword := range keywords {
		for _, kw := range keyword.Keywords {
			normalizedKeyword := strings.ToLower(kw)

			if keyword.MatchType == model.AutoReplyMatchTypeExact {
				if normalizedMessage == normalizedKeyword {
					return &keyword, keyword.Reply, nil
				}
			} else {
				if strings.Contains(normalizedMessage, normalizedKeyword) {
					return &keyword, keyword.Reply, nil
				}
			}
		}
	}

	// 没有匹配到，查找数据库中的默认回复（根据语言）
	var defaultKeyword model.AutoReplyKeyword
	err := s.db.Where("status = ? AND keyword_type = ? AND language = ?",
		model.AutoReplyStatusActive,
		model.AutoReplyKeywordTypeDefault,
		language).
		First(&defaultKeyword).Error

	if err == nil {
		return nil, defaultKeyword.Reply, nil
	}

	// 如果指定语言没有默认回复，尝试获取任意语言的默认回复
	err = s.db.Where("status = ? AND keyword_type = ?",
		model.AutoReplyStatusActive,
		model.AutoReplyKeywordTypeDefault).
		First(&defaultKeyword).Error

	if err == nil {
		return nil, defaultKeyword.Reply, nil
	}

	// 数据库中完全没有默认回复配置
	return nil, "", nil
}

// getHumanTransferMessage 根据语言返回转人工提示消息
func getHumanTransferMessage(language string) string {
	messages := map[string]string{
		"zh-CN": "已为您转接人工客服，请稍候...",
		"zh-TW": "已為您轉接人工客服，請稍候...",
		"en":    "Transferring you to a customer service agent, please wait...",
		"id":    "Menghubungkan Anda ke layanan pelanggan, mohon tunggu...",
		"ms":    "Menghubungkan anda kepada ejen perkhidmatan pelanggan, sila tunggu...",
	}

	if msg, ok := messages[language]; ok {
		return msg
	}
	return messages["en"] // 默认英文
}
