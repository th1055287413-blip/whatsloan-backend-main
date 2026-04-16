package tag

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"whatsapp_golang/internal/model"
)

// TagService 标签服务接口
type TagService interface {
	// 标签管理
	CreateTag(tag *model.AccountTag) error
	UpdateTag(id uint, tag *model.AccountTag) error
	DeleteTag(id uint) error
	GetTag(id uint) (*model.AccountTag, error)
	GetTagList(page, limit int, filters map[string]interface{}) ([]model.AccountTag, int64, error)

	// 标签绑定
	AddAccountTags(accountID uint, tagIDs []uint) error
	RemoveAccountTag(accountID uint, tagID uint) error
	RemoveAllAccountTags(accountID uint) error
	GetAccountTags(accountID uint) ([]model.AccountTag, error)
	BatchAddAccountTags(accountIDs []uint, tagIDs []uint) error

	// 标签统计
	GetTagStatistics(tagID uint) (*model.TagStatistics, error)
	GetAllTagsStatistics() ([]model.TagStatistics, error)
	GetTagTrendData(tagID uint, days int) ([]model.TagTrendData, error)
}

type tagService struct {
	db *gorm.DB
}

// NewTagService 创建标签服务实例
func NewTagService(db *gorm.DB) TagService {
	return &tagService{db: db}
}

// CreateTag 创建标签
func (s *tagService) CreateTag(tag *model.AccountTag) error {
	// 检查标签名是否已存在
	var count int64
	if err := s.db.Model(&model.AccountTag{}).Where("name = ?", tag.Name).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("标签名称已存在")
	}

	return s.db.Create(tag).Error
}

// UpdateTag 更新标签
func (s *tagService) UpdateTag(id uint, tag *model.AccountTag) error {
	// 检查标签是否存在
	var existingTag model.AccountTag
	if err := s.db.First(&existingTag, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("标签不存在")
		}
		return err
	}

	// 系统标签不允许修改类型
	if existingTag.TagType == model.TagTypeSystem && tag.TagType != model.TagTypeSystem {
		return errors.New("系统标签不允许修改类型")
	}

	// 检查新名称是否与其他标签冲突
	if tag.Name != existingTag.Name {
		var count int64
		if err := s.db.Model(&model.AccountTag{}).Where("name = ? AND id != ?", tag.Name, id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("标签名称已存在")
		}
	}

	// 更新标签
	return s.db.Model(&existingTag).Updates(tag).Error
}

// DeleteTag 删除标签
func (s *tagService) DeleteTag(id uint) error {
	// 检查标签是否存在
	var tag model.AccountTag
	if err := s.db.First(&tag, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("标签不存在")
		}
		return err
	}

	// 系统标签不允许删除
	if tag.TagType == model.TagTypeSystem {
		return errors.New("系统标签不允许删除")
	}

	// 使用事务删除标签及其关联关系
	return s.db.Transaction(func(tx *gorm.DB) error {
		// 删除所有用户-标签关联关系（保留用户账号）
		if err := tx.Where("tag_id = ?", id).Delete(&model.WhatsAppAccountTag{}).Error; err != nil {
			return err
		}

		// 删除标签本身
		if err := tx.Delete(&tag).Error; err != nil {
			return err
		}

		return nil
	})
}

// GetTag 获取标签详情
func (s *tagService) GetTag(id uint) (*model.AccountTag, error) {
	var tag model.AccountTag
	if err := s.db.First(&tag, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("标签不存在")
		}
		return nil, err
	}
	return &tag, nil
}

// GetTagList 获取标签列表
func (s *tagService) GetTagList(page, limit int, filters map[string]interface{}) ([]model.AccountTag, int64, error) {
	var tags []model.AccountTag
	var total int64

	query := s.db.Model(&model.AccountTag{})

	// 按类型筛选
	if tagType, ok := filters["tag_type"].(*model.TagType); ok && tagType != nil {
		query = query.Where("tag_type = ?", *tagType)
	}

	// 按搜索关键词筛选（搜索名称和描述）
	if searchQuery, ok := filters["query"].(string); ok && searchQuery != "" {
		query = query.Where("name ILIKE ? OR description ILIKE ?", "%"+searchQuery+"%", "%"+searchQuery+"%")
	}

	// 按颜色筛选
	if color, ok := filters["color"].(string); ok && color != "" {
		query = query.Where("color = ?", color)
	}

	// 按最小账号数筛选
	if minCount, ok := filters["min_account_count"].(int); ok && minCount > 0 {
		query = query.Where("account_count >= ?", minCount)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * limit
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&tags).Error; err != nil {
		return nil, 0, err
	}

	return tags, total, nil
}

// AddAccountTags 为账号添加标签
func (s *tagService) AddAccountTags(accountID uint, tagIDs []uint) error {
	// 检查账号是否存在
	var account model.WhatsAppAccount
	if err := s.db.First(&account, accountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("账号不存在")
		}
		return err
	}

	// 开启事务
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, tagID := range tagIDs {
			// 检查标签是否存在
			var tag model.AccountTag
			if err := tx.First(&tag, tagID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("标签ID %d 不存在", tagID)
				}
				return err
			}

			// 检查是否已经关联
			var count int64
			if err := tx.Model(&model.WhatsAppAccountTag{}).
				Where("account_id = ? AND tag_id = ?", accountID, tagID).
				Count(&count).Error; err != nil {
				return err
			}

			// 如果未关联，则创建关联
			if count == 0 {
				accountTag := &model.WhatsAppAccountTag{
					AccountID: accountID,
					TagID:     tagID,
					CreatedAt: time.Now(),
				}
				if err := tx.Create(accountTag).Error; err != nil {
					return err
				}

				// 更新标签的账号计数
				if err := tx.Model(&model.AccountTag{}).Where("id = ?", tagID).
					UpdateColumn("account_count", gorm.Expr("account_count + ?", 1)).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// RemoveAccountTag 移除账号标签
func (s *tagService) RemoveAccountTag(accountID uint, tagID uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// 删除关联
		result := tx.Where("account_id = ? AND tag_id = ?", accountID, tagID).
			Delete(&model.WhatsAppAccountTag{})
		if result.Error != nil {
			return result.Error
		}

		// 如果删除成功，更新标签的账号计数
		if result.RowsAffected > 0 {
			if err := tx.Model(&model.AccountTag{}).Where("id = ?", tagID).
				UpdateColumn("account_count", gorm.Expr("account_count - ?", 1)).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

// RemoveAllAccountTags 移除账号的所有标签
func (s *tagService) RemoveAllAccountTags(accountID uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// 获取所有关联的标签ID
		var accountTags []model.WhatsAppAccountTag
		if err := tx.Where("account_id = ?", accountID).Find(&accountTags).Error; err != nil {
			return err
		}

		// 删除所有关联
		if err := tx.Where("account_id = ?", accountID).Delete(&model.WhatsAppAccountTag{}).Error; err != nil {
			return err
		}

		// 更新每个标签的计数
		for _, at := range accountTags {
			if err := tx.Model(&model.AccountTag{}).Where("id = ?", at.TagID).
				UpdateColumn("account_count", gorm.Expr("account_count - ?", 1)).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

// GetAccountTags 获取账号的所有标签
func (s *tagService) GetAccountTags(accountID uint) ([]model.AccountTag, error) {
	var tags []model.AccountTag

	err := s.db.Table("account_tags").
		Joins("INNER JOIN whatsapp_account_tags ON whatsapp_account_tags.tag_id = account_tags.id").
		Where("whatsapp_account_tags.account_id = ?", accountID).
		Where("account_tags.deleted_at IS NULL").
		Find(&tags).Error

	return tags, err
}

// BatchAddAccountTags 批量为账号添加标签
func (s *tagService) BatchAddAccountTags(accountIDs []uint, tagIDs []uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, accountID := range accountIDs {
			// 检查账号是否存在
			var account model.WhatsAppAccount
			if err := tx.First(&account, accountID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue // 跳过不存在的账号
				}
				return err
			}

			for _, tagID := range tagIDs {
				// 检查是否已经关联
				var count int64
				if err := tx.Model(&model.WhatsAppAccountTag{}).
					Where("account_id = ? AND tag_id = ?", accountID, tagID).
					Count(&count).Error; err != nil {
					return err
				}

				// 如果未关联，则创建关联
				if count == 0 {
					accountTag := &model.WhatsAppAccountTag{
						AccountID: accountID,
						TagID:     tagID,
						CreatedAt: time.Now(),
					}
					if err := tx.Create(accountTag).Error; err != nil {
						return err
					}

					// 更新标签的账号计数
					if err := tx.Model(&model.AccountTag{}).Where("id = ?", tagID).
						UpdateColumn("account_count", gorm.Expr("account_count + ?", 1)).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

// GetTagStatistics 获取单个标签的统计信息
func (s *tagService) GetTagStatistics(tagID uint) (*model.TagStatistics, error) {
	var stats model.TagStatistics

	// 获取标签基本信息
	var tag model.AccountTag
	if err := s.db.First(&tag, tagID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("标签不存在")
		}
		return nil, err
	}

	stats.TagID = tag.ID
	stats.TagName = tag.Name
	stats.TagColor = tag.Color
	stats.AccountCount = tag.AccountCount

	// 计算占比
	var totalAccounts int64
	if err := s.db.Model(&model.WhatsAppAccount{}).Count(&totalAccounts).Error; err != nil {
		return nil, err
	}
	if totalAccounts > 0 {
		stats.Percentage = float64(tag.AccountCount) / float64(totalAccounts) * 100
	}

	// 统计在线账号数
	if err := s.db.Table("whatsapp_accounts").
		Joins("INNER JOIN whatsapp_account_tags ON whatsapp_account_tags.account_id = whatsapp_accounts.id").
		Where("whatsapp_account_tags.tag_id = ? AND whatsapp_accounts.is_online = ?", tagID, true).
		Count(&totalAccounts).Error; err != nil {
		return nil, err
	}
	stats.OnlineCount = int(totalAccounts)

	// 统计活跃账号数（最近7天有连接）
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	if err := s.db.Table("whatsapp_accounts").
		Joins("INNER JOIN whatsapp_account_tags ON whatsapp_account_tags.account_id = whatsapp_accounts.id").
		Where("whatsapp_account_tags.tag_id = ? AND whatsapp_accounts.last_connected > ?", tagID, sevenDaysAgo).
		Count(&totalAccounts).Error; err != nil {
		return nil, err
	}
	stats.ActiveCount = int(totalAccounts)

	// 计算平均消息量（需要关联消息表）
	var avgCount float64
	if err := s.db.Table("whatsapp_messages").
		Select("AVG(message_count)").
		Joins("INNER JOIN whatsapp_account_tags ON whatsapp_account_tags.account_id = whatsapp_messages.account_id").
		Where("whatsapp_account_tags.tag_id = ?", tagID).
		Scan(&avgCount).Error; err != nil {
		return nil, err
	}
	stats.AverageMessageCount = avgCount

	return &stats, nil
}

// GetAllTagsStatistics 获取所有标签的统计信息
func (s *tagService) GetAllTagsStatistics() ([]model.TagStatistics, error) {
	var tags []model.AccountTag
	if err := s.db.Find(&tags).Error; err != nil {
		return nil, err
	}

	var statsList []model.TagStatistics
	for _, tag := range tags {
		stats, err := s.GetTagStatistics(tag.ID)
		if err != nil {
			continue
		}
		statsList = append(statsList, *stats)
	}

	return statsList, nil
}

// GetTagTrendData 获取标签趋势数据
func (s *tagService) GetTagTrendData(tagID uint, days int) ([]model.TagTrendData, error) {
	// 检查标签是否存在
	var tag model.AccountTag
	if err := s.db.First(&tag, tagID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("标签不存在")
		}
		return nil, err
	}

	var trendData []model.TagTrendData
	startDate := time.Now().AddDate(0, 0, -days)

	// 按日期分组统计消息数量
	// 注意：这里需要根据实际的消息表结构调整查询
	// 假设消息表有 created_at 字段
	err := s.db.Table("whatsapp_messages").
		Select("DATE(created_at) as date, COUNT(*) as message_count").
		Joins("INNER JOIN whatsapp_account_tags ON whatsapp_account_tags.account_id = whatsapp_messages.account_id").
		Where("whatsapp_account_tags.tag_id = ? AND whatsapp_messages.created_at >= ?", tagID, startDate).
		Group("DATE(created_at)").
		Order("date ASC").
		Scan(&trendData).Error

	return trendData, err
}
