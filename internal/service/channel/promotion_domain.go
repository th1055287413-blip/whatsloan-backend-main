package channel

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// PromotionDomainService 推廣域名服務接口
type PromotionDomainService interface {
	// 域名管理
	CreateDomain(req *model.PromotionDomainCreateRequest) (*model.PromotionDomain, error)
	UpdateDomain(id uint, req *model.PromotionDomainUpdateRequest) error
	DeleteDomain(id uint) error
	GetDomain(id uint) (*model.PromotionDomain, error)
	GetDomainList(query *model.PromotionDomainListQuery) ([]model.PromotionDomainListItem, int64, error)
	UpdateDomainStatus(id uint, status string) error

	// 域名選項（用於下拉選擇）
	GetEnabledDomains() ([]model.PromotionDomainSimple, error)
}

type promotionDomainService struct {
	db *gorm.DB
}

// NewPromotionDomainService 創建推廣域名服務實例
func NewPromotionDomainService(db *gorm.DB) PromotionDomainService {
	return &promotionDomainService{db: db}
}

// validateDomain 驗證域名格式
func validateDomain(domain string) error {
	// 移除 https:// 或 http:// 前綴
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimSuffix(domain, "/")

	if domain == "" {
		return errors.New("域名不能為空")
	}

	// 簡單的域名格式驗證
	domainRegex := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
	if !domainRegex.MatchString(domain) {
		return errors.New("域名格式不正確")
	}

	return nil
}

// cleanDomain 清理域名格式
func cleanDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimSuffix(domain, "/")
	return strings.ToLower(domain)
}

// CreateDomain 創建域名
func (s *promotionDomainService) CreateDomain(req *model.PromotionDomainCreateRequest) (*model.PromotionDomain, error) {
	// 驗證名稱
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("域名名稱不能為空")
	}

	// 清理和驗證域名
	domain := cleanDomain(req.Domain)
	if err := validateDomain(domain); err != nil {
		return nil, err
	}

	// 檢查域名是否已存在
	var count int64
	if err := s.db.Model(&model.PromotionDomain{}).
		Where("domain = ? AND deleted_at IS NULL", domain).
		Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, errors.New("域名已存在")
	}

	// 創建域名
	promotionDomain := &model.PromotionDomain{
		Name:   strings.TrimSpace(req.Name),
		Domain: domain,
		Status: "enabled",
		Pixels: req.Pixels,
		Remark: strings.TrimSpace(req.Remark),
	}

	if err := s.db.Create(promotionDomain).Error; err != nil {
		return nil, fmt.Errorf("創建域名失敗: %w", err)
	}

	return promotionDomain, nil
}

// UpdateDomain 更新域名
func (s *promotionDomainService) UpdateDomain(id uint, req *model.PromotionDomainUpdateRequest) error {
	// 檢查域名是否存在
	var domain model.PromotionDomain
	if err := s.db.First(&domain, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("域名不存在")
		}
		return err
	}

	// 檢查是否已刪除
	if domain.DeletedAt != nil {
		return errors.New("域名已刪除，無法修改")
	}

	// 構建更新數據
	updates := make(map[string]interface{})

	if req.Name != "" {
		updates["name"] = strings.TrimSpace(req.Name)
	}

	if req.Domain != "" {
		newDomain := cleanDomain(req.Domain)
		if err := validateDomain(newDomain); err != nil {
			return err
		}

		// 如果域名變化了，檢查新域名是否已存在
		if newDomain != domain.Domain {
			var count int64
			if err := s.db.Model(&model.PromotionDomain{}).
				Where("domain = ? AND id != ? AND deleted_at IS NULL", newDomain, id).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errors.New("域名已存在")
			}
			updates["domain"] = newDomain
		}
	}

	// 備註可以更新為空值
	updates["remark"] = strings.TrimSpace(req.Remark)

	// 更新 pixels
	if req.Pixels != nil {
		updates["pixels"] = *req.Pixels
	}

	// 執行更新
	if len(updates) > 0 {
		if err := s.db.Model(&domain).Updates(updates).Error; err != nil {
			return fmt.Errorf("更新域名失敗: %w", err)
		}
	}

	return nil
}

// DeleteDomain 刪除域名
func (s *promotionDomainService) DeleteDomain(id uint) error {
	// 檢查域名是否存在
	var domain model.PromotionDomain
	if err := s.db.First(&domain, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("域名不存在")
		}
		return err
	}

	// 檢查是否已刪除
	if domain.DeletedAt != nil {
		return errors.New("域名已刪除")
	}

	// 檢查是否有關聯的渠道
	var channelCount int64
	if err := s.db.Model(&model.Channel{}).
		Where("promotion_domain_id = ? AND deleted_at IS NULL", id).
		Count(&channelCount).Error; err != nil {
		return err
	}

	if channelCount > 0 {
		return fmt.Errorf("該域名下還有 %d 個渠道，請先處理", channelCount)
	}

	// 軟刪除域名
	if err := s.db.Delete(&domain).Error; err != nil {
		return fmt.Errorf("刪除域名失敗: %w", err)
	}

	return nil
}

// GetDomain 獲取域名詳情
func (s *promotionDomainService) GetDomain(id uint) (*model.PromotionDomain, error) {
	var domain model.PromotionDomain
	if err := s.db.First(&domain, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("域名不存在")
		}
		return nil, err
	}

	// 檢查是否已刪除
	if domain.DeletedAt != nil {
		return nil, errors.New("域名已刪除")
	}

	return &domain, nil
}

// GetDomainList 獲取域名列表
func (s *promotionDomainService) GetDomainList(query *model.PromotionDomainListQuery) ([]model.PromotionDomainListItem, int64, error) {
	// 設置默認分頁參數
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}

	var items []model.PromotionDomainListItem
	var total int64

	// 構建基礎查詢
	baseQuery := s.db.Model(&model.PromotionDomain{})

	// 狀態篩選
	if query.Status != "" {
		baseQuery = baseQuery.Where("status = ?", query.Status)
	}

	// 關鍵詞搜索（搜索名稱和域名）
	if query.Keyword != "" {
		keyword := "%" + query.Keyword + "%"
		baseQuery = baseQuery.Where("name ILIKE ? OR domain ILIKE ?", keyword, keyword)
	}

	// 獲取總數
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分頁查詢基本信息
	var domains []model.PromotionDomain
	offset := (query.Page - 1) * query.PageSize
	if err := baseQuery.Order("created_at DESC").
		Offset(offset).
		Limit(query.PageSize).
		Find(&domains).Error; err != nil {
		return nil, 0, err
	}

	// 為每個域名補充統計信息
	for _, domain := range domains {
		item := model.PromotionDomainListItem{
			PromotionDomain: domain,
		}

		// 統計關聯的渠道數量
		var channelCount int64
		if err := s.db.Model(&model.Channel{}).
			Where("promotion_domain_id = ? AND deleted_at IS NULL", domain.ID).
			Count(&channelCount).Error; err != nil {
			return nil, 0, err
		}
		item.ChannelCount = channelCount

		items = append(items, item)
	}

	return items, total, nil
}

// UpdateDomainStatus 更新域名狀態
func (s *promotionDomainService) UpdateDomainStatus(id uint, status string) error {
	// 驗證狀態值
	if status != "enabled" && status != "disabled" {
		return errors.New("無效的狀態值，只能是 enabled 或 disabled")
	}

	// 檢查域名是否存在
	var domain model.PromotionDomain
	if err := s.db.First(&domain, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("域名不存在")
		}
		return err
	}

	// 檢查是否已刪除
	if domain.DeletedAt != nil {
		return errors.New("域名已刪除，無法修改狀態")
	}

	// 更新狀態
	if err := s.db.Model(&domain).Update("status", status).Error; err != nil {
		return fmt.Errorf("更新域名狀態失敗: %w", err)
	}

	return nil
}

// GetEnabledDomains 獲取啟用的域名列表（用於下拉選擇）
func (s *promotionDomainService) GetEnabledDomains() ([]model.PromotionDomainSimple, error) {
	var domains []model.PromotionDomain
	if err := s.db.Where("status = ? AND deleted_at IS NULL", "enabled").
		Order("name ASC").
		Find(&domains).Error; err != nil {
		return nil, err
	}

	result := make([]model.PromotionDomainSimple, len(domains))
	for i, d := range domains {
		result[i] = model.PromotionDomainSimple{
			ID:     d.ID,
			Name:   d.Name,
			Domain: d.Domain,
		}
	}

	return result, nil
}
