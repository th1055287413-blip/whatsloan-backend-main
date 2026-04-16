package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// ProxyConfigService 代理配置服務介面
type ProxyConfigService interface {
	CreateProxyConfig(ctx context.Context, req *model.ProxyConfigCreateRequest) (*model.ProxyConfig, error)
	UpdateProxyConfig(ctx context.Context, id uint, req *model.ProxyConfigUpdateRequest) error
	DeleteProxyConfig(ctx context.Context, id uint) error
	GetProxyConfig(ctx context.Context, id uint) (*model.ProxyConfig, error)
	GetProxyConfigList(ctx context.Context, query *model.ProxyConfigListQuery) ([]model.ProxyConfigListItem, int64, error)
	GetEnabledProxyConfigs(ctx context.Context) ([]model.ProxyConfig, error)
	UpdateProxyConfigStatus(ctx context.Context, id uint, status string) error
}

type proxyConfigService struct {
	db *gorm.DB
}

// NewProxyConfigService 建立服務實例
func NewProxyConfigService(db *gorm.DB) ProxyConfigService {
	return &proxyConfigService{db: db}
}

// CreateProxyConfig 創建代理配置
func (s *proxyConfigService) CreateProxyConfig(ctx context.Context, req *model.ProxyConfigCreateRequest) (*model.ProxyConfig, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("名稱不能為空")
	}
	if strings.TrimSpace(req.Host) == "" {
		return nil, errors.New("代理主機不能為空")
	}

	// 設定預設值
	proxyType := req.Type
	if proxyType == "" {
		proxyType = model.ProxyTypeSocks5
	}

	config := &model.ProxyConfig{
		Name:     strings.TrimSpace(req.Name),
		Host:     strings.TrimSpace(req.Host),
		Port:     req.Port,
		Type:     proxyType,
		Username: strings.TrimSpace(req.Username),
		Password: req.Password,
		Status:   "enabled",
		Remark:   strings.TrimSpace(req.Remark),
	}

	if err := s.db.WithContext(ctx).Create(config).Error; err != nil {
		return nil, fmt.Errorf("創建代理配置失敗: %w", err)
	}

	logger.Infow("代理配置已創建", "proxy_config_id", config.ID, "name", config.Name)
	return config, nil
}

// UpdateProxyConfig 更新代理配置
func (s *proxyConfigService) UpdateProxyConfig(ctx context.Context, id uint, req *model.ProxyConfigUpdateRequest) error {
	// 檢查是否存在
	var config model.ProxyConfig
	if err := s.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("代理配置不存在")
		}
		return fmt.Errorf("查詢代理配置失敗: %w", err)
	}

	// 準備更新
	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = strings.TrimSpace(req.Name)
	}
	if req.Host != "" {
		updates["host"] = strings.TrimSpace(req.Host)
	}
	if req.Port > 0 {
		updates["port"] = req.Port
	}
	if req.Type != "" {
		updates["type"] = req.Type
	}
	if req.Username != "" {
		updates["username"] = strings.TrimSpace(req.Username)
	}
	if req.Password != "" {
		updates["password"] = req.Password
	}
	if req.Remark != "" {
		updates["remark"] = strings.TrimSpace(req.Remark)
	}

	if len(updates) == 0 {
		return nil
	}

	updates["updated_at"] = time.Now()

	if err := s.db.WithContext(ctx).Model(&model.ProxyConfig{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("更新代理配置失敗: %w", err)
	}

	logger.Infow("代理配置已更新", "proxy_config_id", id)
	return nil
}

// DeleteProxyConfig 刪除代理配置
func (s *proxyConfigService) DeleteProxyConfig(ctx context.Context, id uint) error {
	// 檢查是否有 Connector 正在使用此代理
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.ConnectorConfig{}).
		Where("proxy_config_id = ? AND deleted_at IS NULL", id).
		Count(&count).Error; err != nil {
		return fmt.Errorf("檢查關聯 Connector 失敗: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("有 %d 個 Connector 正在使用此代理，請先解除綁定", count)
	}

	// 軟刪除
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&model.ProxyConfig{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"deleted_at": &now,
			"status":     "disabled",
		}).Error; err != nil {
		return fmt.Errorf("刪除代理配置失敗: %w", err)
	}

	logger.Infow("代理配置已刪除", "proxy_config_id", id)
	return nil
}

// GetProxyConfig 取得單一代理配置
func (s *proxyConfigService) GetProxyConfig(ctx context.Context, id uint) (*model.ProxyConfig, error) {
	var config model.ProxyConfig
	if err := s.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("代理配置不存在")
		}
		return nil, fmt.Errorf("查詢代理配置失敗: %w", err)
	}
	return &config, nil
}

// GetProxyConfigList 取得代理配置列表
func (s *proxyConfigService) GetProxyConfigList(ctx context.Context, query *model.ProxyConfigListQuery) ([]model.ProxyConfigListItem, int64, error) {
	db := s.db.WithContext(ctx).Model(&model.ProxyConfig{}).Where("deleted_at IS NULL")

	// 狀態篩選
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}

	// 關鍵字搜尋
	if query.Keyword != "" {
		keyword := "%" + query.Keyword + "%"
		db = db.Where("name LIKE ? OR host LIKE ?", keyword, keyword)
	}

	// 計算總數
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("計算總數失敗: %w", err)
	}

	// 分頁
	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	// 查詢
	var configs []model.ProxyConfig
	if err := db.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&configs).Error; err != nil {
		return nil, 0, fmt.Errorf("查詢列表失敗: %w", err)
	}

	// 查詢每個代理的 Connector 數量
	items := make([]model.ProxyConfigListItem, len(configs))
	for i, cfg := range configs {
		var connectorCount int64
		s.db.WithContext(ctx).Model(&model.ConnectorConfig{}).
			Where("proxy_config_id = ? AND deleted_at IS NULL", cfg.ID).
			Count(&connectorCount)

		items[i] = model.ProxyConfigListItem{
			ProxyConfig:    cfg,
			ConnectorCount: connectorCount,
		}
	}

	return items, total, nil
}

// GetEnabledProxyConfigs 取得所有啟用的代理配置
func (s *proxyConfigService) GetEnabledProxyConfigs(ctx context.Context) ([]model.ProxyConfig, error) {
	var configs []model.ProxyConfig
	if err := s.db.WithContext(ctx).
		Where("status = 'enabled' AND deleted_at IS NULL").
		Order("name ASC").
		Find(&configs).Error; err != nil {
		return nil, fmt.Errorf("查詢代理配置失敗: %w", err)
	}
	return configs, nil
}

// UpdateProxyConfigStatus 更新代理狀態
func (s *proxyConfigService) UpdateProxyConfigStatus(ctx context.Context, id uint, status string) error {
	if status != "enabled" && status != "disabled" {
		return errors.New("無效的狀態")
	}

	if err := s.db.WithContext(ctx).Model(&model.ProxyConfig{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": time.Now(),
		}).Error; err != nil {
		return fmt.Errorf("更新狀態失敗: %w", err)
	}

	logger.Infow("代理配置狀態已更新", "proxy_config_id", id, "status", status)
	return nil
}
