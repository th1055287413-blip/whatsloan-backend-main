package channel

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"whatsapp_golang/internal/model"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ChannelService 渠道服务接口
type ChannelService interface {
	// 渠道管理
	CreateChannel(req *model.ChannelCreateRequest) (*model.Channel, error)
	UpdateChannel(id uint, req *model.ChannelUpdateRequest) error
	DeleteChannel(id uint, handleReq *model.ChannelDeleteRequest) error
	GetChannel(id uint) (*model.Channel, error)
	GetChannelList(query *model.ChannelListQuery) ([]model.ChannelListItem, int64, error)
	UpdateChannelStatus(id uint, status string) error

	// 渠道号相关
	GenerateChannelCode(lang string) (string, error)
	CheckChannelCodeExists(code string) (bool, error)
	GetChannelByCode(code string) (*model.Channel, error)

	// Ad Pixels
	GetChannelPixels(code string) (model.ChannelPixels, string, error)
	GetPixelsByDomain(host string) (model.ChannelPixels, string, error)

	// 渠道隔离配置
	GetChannelIsolationEnabled() (bool, error)
	SetChannelIsolationEnabled(enabled bool) error

	// Viewer Password
	SetViewerPassword(id uint, password string) error
}

// ConfigService 渠道服務需要的配置介面
type ConfigService interface {
	GetConfig(key string) (string, error)
	SetConfig(key, value, updatedBy string) error
}

type channelService struct {
	db            *gorm.DB
	configService ConfigService
}

// NewChannelService 创建渠道服务实例
func NewChannelService(db *gorm.DB, configService ConfigService) ChannelService {
	return &channelService{
		db:            db,
		configService: configService,
	}
}

// GenerateChannelCode 生成渠道号（6位随机字母数字）
// lang 参数用于未来可能的语言相关渠道号生成规则
func (s *channelService) GenerateChannelCode(lang string) (string, error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const length = 6
	maxRetries := 10

	for i := 0; i < maxRetries; i++ {
		code := make([]byte, length)
		for j := 0; j < length; j++ {
			num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
			if err != nil {
				return "", fmt.Errorf("生成随机数失败: %w", err)
			}
			code[j] = charset[num.Int64()]
		}

		codeStr := string(code)

		// 检查是否已存在
		exists, err := s.CheckChannelCodeExists(codeStr)
		if err != nil {
			return "", err
		}

		if !exists {
			return codeStr, nil
		}
	}

	return "", errors.New("生成渠道号失败：重试次数过多，请稍后再试")
}

// CheckChannelCodeExists 检查渠道号是否存在
func (s *channelService) CheckChannelCodeExists(code string) (bool, error) {
	var count int64
	err := s.db.Model(&model.Channel{}).
		Where("channel_code = ? AND deleted_at IS NULL", code).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateChannel 创建渠道
func (s *channelService) CreateChannel(req *model.ChannelCreateRequest) (*model.Channel, error) {
	// 验证渠道名称
	if strings.TrimSpace(req.ChannelName) == "" {
		return nil, errors.New("渠道名称不能为空")
	}

	// 验证推廣域名
	if req.PromotionDomainID == 0 {
		return nil, errors.New("請選擇推廣域名")
	}

	// 檢查推廣域名是否存在
	var domain model.PromotionDomain
	if err := s.db.Where("id = ? AND deleted_at IS NULL", req.PromotionDomainID).First(&domain).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("推廣域名不存在")
		}
		return nil, err
	}

	// 生成或验证渠道号
	var channelCode string
	var err error

	if req.ChannelCode != "" {
		// 用户提供了渠道号，需要验证
		channelCode = strings.ToUpper(strings.TrimSpace(req.ChannelCode))

		// 验证格式
		if len(channelCode) != 6 {
			return nil, errors.New("渠道号必须为6位字符")
		}

		// 验证字符（只允许字母和数字）
		for _, c := range channelCode {
			if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
				return nil, errors.New("渠道号只能包含大写字母和数字")
			}
		}

		// 检查是否已存在
		exists, err := s.CheckChannelCodeExists(channelCode)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, errors.New("渠道号已存在")
		}
	} else {
		// 自动生成渠道号
		channelCode, err = s.GenerateChannelCode(req.Lang)
		if err != nil {
			return nil, err
		}
	}

	// 驗證 workgroup（如有指定）
	if req.WorkgroupID != nil && *req.WorkgroupID > 0 {
		if err := s.validateWorkgroupActive(*req.WorkgroupID); err != nil {
			return nil, err
		}
	}

	// 创建渠道
	promotionDomainID := req.PromotionDomainID
	channel := &model.Channel{
		ChannelCode:       channelCode,
		ChannelName:       strings.TrimSpace(req.ChannelName),
		Lang:              req.Lang,
		LoanType:          req.LoanType,
		PromotionDomainID: &promotionDomainID,
		Status:            "enabled",
		Pixels:            req.Pixels,
		Remark:            strings.TrimSpace(req.Remark),
		WorkgroupID:       req.WorkgroupID,
	}

	if err := s.db.Create(channel).Error; err != nil {
		return nil, fmt.Errorf("创建渠道失败: %w", err)
	}

	return channel, nil
}

// UpdateChannel 更新渠道
func (s *channelService) UpdateChannel(id uint, req *model.ChannelUpdateRequest) error {
	// 检查渠道是否存在
	var channel model.Channel
	if err := s.db.First(&channel, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("渠道不存在")
		}
		return err
	}

	// 检查是否已删除
	if channel.DeletedAt != nil {
		return errors.New("渠道已删除，无法修改")
	}

	// 构建更新数据
	updates := make(map[string]interface{})

	if req.ChannelName != "" {
		updates["channel_name"] = strings.TrimSpace(req.ChannelName)
	}

	if req.ChannelCode != "" {
		// 验证新渠道号
		newCode := strings.ToUpper(strings.TrimSpace(req.ChannelCode))

		// 验证格式
		if len(newCode) != 6 {
			return errors.New("渠道号必须为6位字符")
		}

		// 验证字符
		for _, c := range newCode {
			if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
				return errors.New("渠道号只能包含大写字母和数字")
			}
		}

		// 如果渠道号变化了，检查新渠道号是否已存在
		if newCode != channel.ChannelCode {
			var count int64
			if err := s.db.Model(&model.Channel{}).
				Where("channel_code = ? AND id != ? AND deleted_at IS NULL", newCode, id).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errors.New("渠道号已存在")
			}
			updates["channel_code"] = newCode
		}
	}

	if req.Remark != "" || req.Remark == "" {
		updates["remark"] = strings.TrimSpace(req.Remark)
	}

	// 更新语言
	if req.Lang != "" {
		updates["lang"] = req.Lang
	}

	// 更新贷款类型（nil 指针表示不更新，空字符串表示清除）
	if req.LoanType != nil {
		updates["loan_type"] = *req.LoanType
	}

	// 更新推廣域名
	if req.PromotionDomainID != nil && *req.PromotionDomainID > 0 {
		// 檢查推廣域名是否存在
		var domain model.PromotionDomain
		if err := s.db.Where("id = ? AND deleted_at IS NULL", *req.PromotionDomainID).First(&domain).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("推廣域名不存在")
			}
			return err
		}
		updates["promotion_domain_id"] = *req.PromotionDomainID
	}

	// 更新 pixels
	if req.Pixels != nil {
		updates["pixels"] = *req.Pixels
	}

	// 更新工作組綁定
	if req.ClearWorkgroup {
		updates["workgroup_id"] = nil
	} else if req.WorkgroupID != nil && *req.WorkgroupID > 0 {
		if err := s.validateWorkgroupActive(*req.WorkgroupID); err != nil {
			return err
		}
		updates["workgroup_id"] = *req.WorkgroupID
	}

	// 执行更新
	if len(updates) > 0 {
		if err := s.db.Model(&channel).Updates(updates).Error; err != nil {
			return fmt.Errorf("更新渠道失败: %w", err)
		}
	}

	return nil
}

// DeleteChannel 删除渠道
func (s *channelService) DeleteChannel(id uint, handleReq *model.ChannelDeleteRequest) error {
	// 检查渠道是否存在
	var channel model.Channel
	if err := s.db.First(&channel, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("渠道不存在")
		}
		return err
	}

	// 检查是否已删除
	if channel.DeletedAt != nil {
		return errors.New("渠道已删除")
	}

	// 开启事务
	return s.db.Transaction(func(tx *gorm.DB) error {
		// 查询关联的授权用户数量（排除已刪除）
		var userCount int64
		if err := tx.Model(&model.WhatsAppAccount{}).
			Where("channel_id = ? AND status != ?", id, "deleted").
			Count(&userCount).Error; err != nil {
			return err
		}

		// 查询关联的后管用户数量
		var adminUserCount int64
		if err := tx.Model(&model.AdminUser{}).
			Where("channel_id = ?", id).
			Count(&adminUserCount).Error; err != nil {
			return err
		}

		// 如果有关联用户，需要处理
		if userCount > 0 || adminUserCount > 0 {
			if handleReq == nil {
				return fmt.Errorf("该渠道下还有 %d 个授权用户和 %d 个后管用户，请先处理关联用户", userCount, adminUserCount)
			}

			switch handleReq.UserHandleType {
			case "transfer":
				// 转移到其他渠道
				if handleReq.TargetChannelID == nil {
					return errors.New("转移用户时必须指定目标渠道")
				}

				// 检查目标渠道是否存在且未删除
				var targetChannel model.Channel
				if err := tx.Where("id = ? AND deleted_at IS NULL", *handleReq.TargetChannelID).
					First(&targetChannel).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return errors.New("目标渠道不存在")
					}
					return err
				}

				// 转移授权用户
				if userCount > 0 {
					if err := tx.Model(&model.WhatsAppAccount{}).
						Where("channel_id = ?", id).
						Update("channel_id", *handleReq.TargetChannelID).Error; err != nil {
						return fmt.Errorf("转移授权用户失败: %w", err)
					}
				}

				// 转移后管用户
				if adminUserCount > 0 {
					if err := tx.Model(&model.AdminUser{}).
						Where("channel_id = ?", id).
						Update("channel_id", *handleReq.TargetChannelID).Error; err != nil {
						return fmt.Errorf("转移后管用户失败: %w", err)
					}
				}

			case "none":
				// 设置为无渠道（NULL）
				if userCount > 0 {
					if err := tx.Model(&model.WhatsAppAccount{}).
						Where("channel_id = ?", id).
						Update("channel_id", nil).Error; err != nil {
						return fmt.Errorf("清除授权用户渠道关联失败: %w", err)
					}
				}

				if adminUserCount > 0 {
					if err := tx.Model(&model.AdminUser{}).
						Where("channel_id = ?", id).
						Update("channel_id", nil).Error; err != nil {
						return fmt.Errorf("清除后管用户渠道关联失败: %w", err)
					}
				}

			default:
				return fmt.Errorf("无效的用户处理类型: %s", handleReq.UserHandleType)
			}
		}

		// 软删除渠道
		if err := tx.Delete(&channel).Error; err != nil {
			return fmt.Errorf("删除渠道失败: %w", err)
		}

		return nil
	})
}

// GetChannel 获取渠道详情
func (s *channelService) GetChannel(id uint) (*model.Channel, error) {
	var channel model.Channel
	if err := s.db.First(&channel, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("渠道不存在")
		}
		return nil, err
	}

	// 检查是否已删除
	if channel.DeletedAt != nil {
		return nil, errors.New("渠道已删除")
	}

	return &channel, nil
}

// GetChannelList 获取渠道列表
func (s *channelService) GetChannelList(query *model.ChannelListQuery) ([]model.ChannelListItem, int64, error) {
	// 设置默认分页参数
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}

	var items []model.ChannelListItem
	var total int64

	// 构建基础查询
	baseQuery := s.db.Model(&model.Channel{})

	// 状态筛选
	if query.Status != "" {
		baseQuery = baseQuery.Where("status = ?", query.Status)
	}

	// 关键词搜索（搜索渠道名称和渠道号）
	if query.Keyword != "" {
		keyword := "%" + query.Keyword + "%"
		baseQuery = baseQuery.Where("channel_name ILIKE ? OR channel_code ILIKE ?", keyword, keyword)
	}

	// 获取总数
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询渠道基本信息（預載入推廣域名）
	var channels []model.Channel
	offset := (query.Page - 1) * query.PageSize
	if err := baseQuery.Preload("PromotionDomain").
		Order("created_at DESC").
		Offset(offset).
		Limit(query.PageSize).
		Find(&channels).Error; err != nil {
		return nil, 0, err
	}

	// 为每个渠道补充统计信息
	for _, channel := range channels {
		item := model.ChannelListItem{
			Channel: channel,
		}

		// 统计关联的授权用户数量
		var userCount int64
		if err := s.db.Model(&model.WhatsAppAccount{}).
			Where("channel_id = ?", channel.ID).
			Count(&userCount).Error; err != nil {
			return nil, 0, err
		}
		item.UserCount = userCount

		// 统计关联的后管用户数量
		var adminUserCount int64
		if err := s.db.Model(&model.AdminUser{}).
			Where("channel_id = ?", channel.ID).
			Count(&adminUserCount).Error; err != nil {
			return nil, 0, err
		}
		item.AdminUserCount = adminUserCount

		// 生成推广链接（使用綁定的域名）
		item.PromotionURL = channel.GetPromotionURL()

		// 推廣域名名稱
		if channel.PromotionDomain != nil {
			item.PromotionDomainName = channel.PromotionDomain.Name
		}

		item.HasViewerPassword = channel.ViewerPassword != ""

		// 工作組名稱
		if channel.WorkgroupID != nil {
			var wgName string
			if err := s.db.Table("workgroups").Select("name").Where("id = ?", *channel.WorkgroupID).Scan(&wgName).Error; err == nil {
				item.WorkgroupName = wgName
			}
		}

		items = append(items, item)
	}

	return items, total, nil
}

// UpdateChannelStatus 更新渠道状态
func (s *channelService) UpdateChannelStatus(id uint, status string) error {
	// 验证状态值
	if status != "enabled" && status != "disabled" {
		return errors.New("无效的状态值，只能是 enabled 或 disabled")
	}

	// 检查渠道是否存在
	var channel model.Channel
	if err := s.db.First(&channel, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("渠道不存在")
		}
		return err
	}

	// 检查是否已删除
	if channel.DeletedAt != nil {
		return errors.New("渠道已删除，无法修改状态")
	}

	// 更新状态
	if err := s.db.Model(&channel).Update("status", status).Error; err != nil {
		return fmt.Errorf("更新渠道状态失败: %w", err)
	}

	return nil
}

// GetChannelByCode 根据渠道号获取渠道
func (s *channelService) GetChannelByCode(code string) (*model.Channel, error) {
	var channel model.Channel
	if err := s.db.Where("channel_code = ? AND deleted_at IS NULL", code).
		First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("渠道不存在")
		}
		return nil, err
	}

	// 检查渠道状态
	if channel.Status != "enabled" {
		return nil, errors.New("渠道已禁用")
	}

	return &channel, nil
}

// GetChannelPixels 根據渠道號取得 pixels 配置
// 以 promotion domain pixels 為底，channel pixels 按 platform 覆蓋或新增
func (s *channelService) GetChannelPixels(code string) (model.ChannelPixels, string, error) {
	var channel model.Channel
	if err := s.db.Select("pixels", "promotion_domain_id", "loan_type").
		Where("channel_code = ? AND deleted_at IS NULL AND status = 'enabled'", code).
		First(&channel).Error; err != nil {
		return model.ChannelPixels{}, "", nil
	}

	// 取得 domain pixels
	var domainPixels model.ChannelPixels
	if channel.PromotionDomainID != nil {
		var domain model.PromotionDomain
		if err := s.db.Select("pixels").First(&domain, *channel.PromotionDomainID).Error; err == nil {
			domainPixels = domain.Pixels
		}
	}

	// 渠道自身的 loan_type
	loanType := channel.LoanType

	// 無 channel pixels 時直接回傳 domain pixels
	if len(channel.Pixels) == 0 {
		return domainPixels, loanType, nil
	}

	// 無 domain pixels 時直接回傳 channel pixels
	if len(domainPixels) == 0 {
		return channel.Pixels, loanType, nil
	}

	// 合併：domain 為底，channel 按 platform 覆蓋
	merged := make(map[string]model.ChannelPixel)
	for _, p := range domainPixels {
		merged[p.Platform] = p
	}
	for _, p := range channel.Pixels {
		merged[p.Platform] = p
	}

	result := make(model.ChannelPixels, 0, len(merged))
	for _, p := range merged {
		result = append(result, p)
	}
	return result, loanType, nil
}

// GetPixelsByDomain 根據 request host 比對 promotion domain 取得預設 pixels
func (s *channelService) GetPixelsByDomain(host string) (model.ChannelPixels, string, error) {
	// 移除 port
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	if host == "" {
		return model.ChannelPixels{}, "", nil
	}

	var domain model.PromotionDomain
	if err := s.db.Select("pixels").
		Where("domain = ? AND deleted_at IS NULL AND status = 'enabled'", host).
		First(&domain).Error; err != nil {
		return model.ChannelPixels{}, "", nil
	}

	return domain.Pixels, "", nil
}

// GetChannelIsolationEnabled 获取渠道隔离开关状态
func (s *channelService) GetChannelIsolationEnabled() (bool, error) {
	value, err := s.configService.GetConfig("channel_isolation_enabled")
	if err != nil {
		// 如果配置不存在，返回默认值 true
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return true, nil
		}
		return false, err
	}

	return value == "true", nil
}

// SetChannelIsolationEnabled 设置渠道隔离开关
func (s *channelService) SetChannelIsolationEnabled(enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}

	return s.configService.SetConfig("channel_isolation_enabled", value, "system")
}

// validateWorkgroupActive 驗證工作組存在且為 active 狀態
func (s *channelService) validateWorkgroupActive(workgroupID uint) error {
	var wg struct {
		Status string
	}
	if err := s.db.Table("workgroups").Select("status").Where("id = ? AND deleted_at IS NULL", workgroupID).Scan(&wg).Error; err != nil {
		return errors.New("工作組不存在")
	}
	if wg.Status == "" {
		return errors.New("工作組不存在")
	}
	if wg.Status != "active" {
		return errors.New("工作組非啟用狀態")
	}
	return nil
}

// SetViewerPassword 設定渠道 viewer password
func (s *channelService) SetViewerPassword(id uint, password string) error {
	var channel model.Channel
	if err := s.db.First(&channel, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("渠道不存在")
		}
		return err
	}

	if channel.DeletedAt != nil {
		return errors.New("渠道已刪除")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("密碼雜湊失敗: %w", err)
	}

	return s.db.Model(&channel).Update("viewer_password", string(hash)).Error
}
