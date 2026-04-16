package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// ReferralService 推荐码服务
type ReferralService struct {
	db        *gorm.DB
	generator *ReferralCodeGenerator
}

// NewReferralService 创建推荐码服务实例
func NewReferralService(db *gorm.DB) *ReferralService {
	return &ReferralService{
		db:        db,
		generator: NewReferralCodeGenerator(),
	}
}

// GenerateReferralCode 为用户生成推荐码
func (s *ReferralService) GenerateReferralCode(ctx context.Context, accountID uint, promotionDomainID *uint, landingPath string, adminID *uint) (*model.ReferralCode, error) {
	// 检查是否已存在推荐码
	var existing model.ReferralCode
	err := s.db.WithContext(ctx).Where("account_id = ?", accountID).First(&existing).Error
	if err == nil {
		// 已存在，更新配置
		return s.UpdateReferralCodeConfig(ctx, accountID, promotionDomainID, landingPath)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("failed to check existing referral code: %w", err)
	}

	// 生成新推荐码（带重试机制）
	var code string
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		code, err = s.generator.GenerateCode(accountID)
		if err != nil {
			return nil, fmt.Errorf("failed to generate code: %w", err)
		}

		// 检查唯一性
		var count int64
		if err := s.db.WithContext(ctx).Model(&model.ReferralCode{}).Where("referral_code = ?", code).Count(&count).Error; err != nil {
			return nil, fmt.Errorf("failed to check code uniqueness: %w", err)
		}
		if count == 0 {
			break
		}

		if i == maxRetries-1 {
			return nil, errors.New("failed to generate unique referral code after retries")
		}
	}

	// 获取推广域名
	var domain *model.PromotionDomain
	if promotionDomainID != nil {
		domain = &model.PromotionDomain{}
		if err := s.db.WithContext(ctx).First(domain, *promotionDomainID).Error; err != nil {
			return nil, fmt.Errorf("failed to get promotion domain: %w", err)
		}
	}

	// 构建分享链接
	shareURL := s.buildShareURL(domain, landingPath, code)

	// 创建推荐码记录
	referralCode := &model.ReferralCode{
		AccountID:         accountID,
		ReferralCode:      code,
		PromotionDomainID: promotionDomainID,
		LandingPath:       landingPath,
		ShareURL:          shareURL,
		CreatedByAdminID:  adminID,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	// 使用事务
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 创建推荐码记录
		if err := tx.Create(referralCode).Error; err != nil {
			return fmt.Errorf("failed to create referral code: %w", err)
		}

		// 更新 whatsapp_accounts 表的 referral_code 字段
		if err := tx.Model(&model.WhatsAppAccount{}).Where("id = ?", accountID).Update("referral_code", code).Error; err != nil {
			return fmt.Errorf("failed to update account referral code: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return referralCode, nil
}

// UpdateReferralCodeConfig 更新推荐码配置（不改变推荐码本身）
func (s *ReferralService) UpdateReferralCodeConfig(ctx context.Context, accountID uint, promotionDomainID *uint, landingPath string) (*model.ReferralCode, error) {
	var referralCode model.ReferralCode
	if err := s.db.WithContext(ctx).Where("account_id = ?", accountID).First(&referralCode).Error; err != nil {
		return nil, fmt.Errorf("referral code not found: %w", err)
	}

	// 获取推广域名
	var domain *model.PromotionDomain
	if promotionDomainID != nil {
		domain = &model.PromotionDomain{}
		if err := s.db.WithContext(ctx).First(domain, *promotionDomainID).Error; err != nil {
			return nil, fmt.Errorf("failed to get promotion domain: %w", err)
		}
	}

	// 更新配置
	shareURL := s.buildShareURL(domain, landingPath, referralCode.ReferralCode)
	updates := map[string]interface{}{
		"promotion_domain_id": promotionDomainID,
		"landing_path":        landingPath,
		"share_url":           shareURL,
		"updated_at":          time.Now(),
	}

	if err := s.db.WithContext(ctx).Model(&referralCode).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update referral code config: %w", err)
	}

	return &referralCode, nil
}

// buildShareURL 构建分享链接
func (s *ReferralService) buildShareURL(domain *model.PromotionDomain, landingPath, code string) string {
	baseURL := "https://shop.whatswoo.org" // 默认域名
	if domain != nil && domain.Domain != "" {
		baseURL = fmt.Sprintf("https://%s", domain.Domain)
	}

	if landingPath == "" {
		landingPath = "/"
	}

	return fmt.Sprintf("%s%s?ref=%s", baseURL, landingPath, code)
}

// GetReferralProfile 获取用户的推荐信息
func (s *ReferralService) GetReferralProfile(ctx context.Context, accountID uint) (*ReferralProfileResponse, error) {
	var referralCode model.ReferralCode
	if err := s.db.WithContext(ctx).Where("account_id = ?", accountID).First(&referralCode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 自动创建推荐码
			createdCode, createErr := s.GenerateReferralCode(ctx, accountID, nil, "/", nil)
			if createErr != nil {
				return nil, fmt.Errorf("failed to auto-generate referral code: %w", createErr)
			}
			referralCode = *createdCode
		} else {
			return nil, fmt.Errorf("failed to get referral code: %w", err)
		}
	}

	// 获取统计数据
	stats, err := s.GetReferralStats(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	// 获取最近的推荐记录（不包含关联数据）
	var recentRegistrations []model.ReferralRegistration
	err = s.db.WithContext(ctx).
		Where("source_account_id = ?", accountID).
		Order("registered_at DESC").
		Limit(10).
		Find(&recentRegistrations).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get recent registrations: %w", err)
	}

	// 动态重建分享链接（确保使用最新域名）
	var domain *model.PromotionDomain
	if referralCode.PromotionDomainID != nil {
		domain = &model.PromotionDomain{}
		s.db.WithContext(ctx).First(domain, *referralCode.PromotionDomainID)
	}
	shareURL := s.buildShareURL(domain, referralCode.LandingPath, referralCode.ReferralCode)

	return &ReferralProfileResponse{
		ReferralCode:        referralCode.ReferralCode,
		ShareURL:            shareURL,
		QRCodeURL:           referralCode.QRCodeURL,
		Stats:               *stats,
		RecentRegistrations: recentRegistrations,
	}, nil
}

// GetReferralStats 获取推荐统计数据
func (s *ReferralService) GetReferralStats(ctx context.Context, accountID uint) (*ReferralStatsResponse, error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekStart := today.AddDate(0, 0, -int(today.Weekday()))
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	stats := &ReferralStatsResponse{}

	// 总推荐数
	if err := s.db.WithContext(ctx).Model(&model.ReferralRegistration{}).
		Where("source_account_id = ?", accountID).
		Count(&stats.TotalReferrals).Error; err != nil {
		return nil, fmt.Errorf("failed to count total referrals: %w", err)
	}

	// 今日推荐数
	if err := s.db.WithContext(ctx).Model(&model.ReferralRegistration{}).
		Where("source_account_id = ? AND registered_at >= ?", accountID, today).
		Count(&stats.TodayReferrals).Error; err != nil {
		return nil, fmt.Errorf("failed to count today referrals: %w", err)
	}

	// 本周推荐数
	if err := s.db.WithContext(ctx).Model(&model.ReferralRegistration{}).
		Where("source_account_id = ? AND registered_at >= ?", accountID, weekStart).
		Count(&stats.ThisWeekReferrals).Error; err != nil {
		return nil, fmt.Errorf("failed to count week referrals: %w", err)
	}

	// 本月推荐数
	if err := s.db.WithContext(ctx).Model(&model.ReferralRegistration{}).
		Where("source_account_id = ? AND registered_at >= ?", accountID, monthStart).
		Count(&stats.ThisMonthReferrals).Error; err != nil {
		return nil, fmt.Errorf("failed to count month referrals: %w", err)
	}

	return stats, nil
}

// GetAllReferralStats 获取所有账号的汇总推荐统计
func (s *ReferralService) GetAllReferralStats(ctx context.Context) (*ReferralStatsResponse, error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekStart := today.AddDate(0, 0, -int(today.Weekday()))
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	stats := &ReferralStatsResponse{}

	// 总推荐数
	if err := s.db.WithContext(ctx).Model(&model.ReferralRegistration{}).
		Count(&stats.TotalReferrals).Error; err != nil {
		return nil, fmt.Errorf("failed to count total referrals: %w", err)
	}

	// 今日推荐数
	if err := s.db.WithContext(ctx).Model(&model.ReferralRegistration{}).
		Where("registered_at >= ?", today).
		Count(&stats.TodayReferrals).Error; err != nil {
		return nil, fmt.Errorf("failed to count today referrals: %w", err)
	}

	// 本周推荐数
	if err := s.db.WithContext(ctx).Model(&model.ReferralRegistration{}).
		Where("registered_at >= ?", weekStart).
		Count(&stats.ThisWeekReferrals).Error; err != nil {
		return nil, fmt.Errorf("failed to count week referrals: %w", err)
	}

	// 本月推荐数
	if err := s.db.WithContext(ctx).Model(&model.ReferralRegistration{}).
		Where("registered_at >= ?", monthStart).
		Count(&stats.ThisMonthReferrals).Error; err != nil {
		return nil, fmt.Errorf("failed to count month referrals: %w", err)
	}

	return stats, nil
}

// ValidateReferralCode 验证推荐码
func (s *ReferralService) ValidateReferralCode(ctx context.Context, code string) (*ValidateReferralCodeResponse, error) {
	var referralCode model.ReferralCode
	err := s.db.WithContext(ctx).
		Where("referral_code = ?", code).
		First(&referralCode).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &ValidateReferralCodeResponse{Valid: false}, nil
		}
		return nil, fmt.Errorf("failed to validate code: %w", err)
	}

	// 如果需要返回来源用户信息，手动查询
	var sourceAccount model.WhatsAppAccount
	if err := s.db.WithContext(ctx).First(&sourceAccount, referralCode.AccountID).Error; err != nil {
		return nil, fmt.Errorf("failed to get source account: %w", err)
	}

	return &ValidateReferralCodeResponse{
		Valid:             true,
		SourceAccountID:   referralCode.AccountID,
		PromotionDomainID: referralCode.PromotionDomainID,
		SourceAccount:     &sourceAccount,
	}, nil
}

// Response types
type ReferralProfileResponse struct {
	ReferralCode        string                       `json:"referral_code"`
	ShareURL            string                       `json:"share_url"`
	QRCodeURL           *string                      `json:"qr_code_url,omitempty"`
	Stats               ReferralStatsResponse        `json:"stats"`
	RecentRegistrations []model.ReferralRegistration `json:"recent_registrations"`
}

type ReferralStatsResponse struct {
	TotalReferrals     int64 `json:"total_referrals"`
	TodayReferrals     int64 `json:"today_referrals"`
	ThisWeekReferrals  int64 `json:"this_week_referrals"`
	ThisMonthReferrals int64 `json:"this_month_referrals"`
}

type ValidateReferralCodeResponse struct {
	Valid             bool                   `json:"valid"`
	SourceAccountID   uint                   `json:"source_account_id"`
	PromotionDomainID *uint                  `json:"promotion_domain_id,omitempty"`
	SourceAccount     *model.WhatsAppAccount `json:"source_account,omitempty"`
}

type ReferralRegistrationsQueryParams struct {
	SourceAccountID   *uint
	OperatorAdminID   *uint
	PromotionDomainID *uint
	StartDate         *time.Time
	EndDate           *time.Time
	Page              int
	PageSize          int
}

type ReferralRegistrationsResponse struct {
	Items      []model.ReferralRegistration `json:"items"`
	Total      int64                        `json:"total"`
	Page       int                          `json:"page"`
	PageSize   int                          `json:"page_size"`
	TotalPages int                          `json:"total_pages"`
}

// GetReferralRegistrations 查询裂变注册记录
func (s *ReferralService) GetReferralRegistrations(ctx context.Context, params ReferralRegistrationsQueryParams) (*ReferralRegistrationsResponse, error) {
	query := s.db.WithContext(ctx).Model(&model.ReferralRegistration{})

	// 应用过滤条件
	if params.SourceAccountID != nil {
		query = query.Where("source_account_id = ?", *params.SourceAccountID)
	}
	if params.OperatorAdminID != nil {
		query = query.Where("operator_admin_id = ?", *params.OperatorAdminID)
	}
	if params.PromotionDomainID != nil {
		query = query.Where("promotion_domain_id = ?", *params.PromotionDomainID)
	}
	if params.StartDate != nil {
		query = query.Where("registered_at >= ?", *params.StartDate)
	}
	if params.EndDate != nil {
		query = query.Where("registered_at <= ?", *params.EndDate)
	}

	// 获取总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("failed to count registrations: %w", err)
	}

	// 分页查询
	var registrations []model.ReferralRegistration
	offset := (params.Page - 1) * params.PageSize
	if err := query.Order("registered_at DESC").
		Limit(params.PageSize).
		Offset(offset).
		Find(&registrations).Error; err != nil {
		return nil, fmt.Errorf("failed to query registrations: %w", err)
	}

	// 计算总页数
	totalPages := int(total) / params.PageSize
	if int(total)%params.PageSize > 0 {
		totalPages++
	}

	return &ReferralRegistrationsResponse{
		Items:      registrations,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
	}, nil
}
