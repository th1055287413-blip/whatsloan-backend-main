package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"whatsapp_golang/internal/gateway"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

// ConnectorStatus Connector 運行狀態（從 Redis 心跳讀取）
type ConnectorStatus struct {
	ID           string        `json:"id"`
	AccountCount int           `json:"account_count"`
	AccountIDs   []uint        `json:"account_ids"`
	Uptime       time.Duration `json:"uptime"`
	StartTime    time.Time     `json:"start_time"`
}

// ConnectorConfigService Connector 配置服務介面
type ConnectorConfigService interface {
	// CRUD 操作
	CreateConnectorConfig(ctx context.Context, req *model.ConnectorConfigCreateRequest) (*model.ConnectorConfig, error)
	UpdateConnectorConfig(ctx context.Context, id string, req *model.ConnectorConfigUpdateRequest) error
	DeleteConnectorConfig(ctx context.Context, id string) error
	GetConnectorConfig(ctx context.Context, id string) (*model.ConnectorConfig, error)
	GetConnectorConfigList(ctx context.Context, query *model.ConnectorConfigListQuery) ([]model.ConnectorConfigListItem, int64, error)

	// 代理綁定
	BindProxy(ctx context.Context, id string, proxyConfigID uint) error
	UnbindProxy(ctx context.Context, id string) error

	// Connector 操作（透過管理命令發送到 Connector 服務）
	StartConnector(ctx context.Context, id string) error
	StopConnector(ctx context.Context, id string) error
	RestartConnector(ctx context.Context, id string) error

	// 狀態查詢（從 Redis 心跳讀取）
	GetConnectorStatus(ctx context.Context, id string) (*ConnectorStatus, error)
}

type connectorConfigService struct {
	db      *gorm.DB
	gateway *gateway.ConnectorGateway
	redis   *redis.Client
}

// NewConnectorConfigService 建立服務實例
func NewConnectorConfigService(db *gorm.DB, gw *gateway.ConnectorGateway, redisClient *redis.Client) ConnectorConfigService {
	return &connectorConfigService{
		db:      db,
		gateway: gw,
		redis:   redisClient,
	}
}

// CreateConnectorConfig 創建 Connector 配置
func (s *connectorConfigService) CreateConnectorConfig(ctx context.Context, req *model.ConnectorConfigCreateRequest) (*model.ConnectorConfig, error) {
	// 驗證 ID 格式
	if strings.TrimSpace(req.ID) == "" {
		return nil, errors.New("ID 不能為空")
	}

	// 檢查 ID 是否已存在
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.ConnectorConfig{}).
		Where("id = ? AND deleted_at IS NULL", req.ID).
		Count(&count).Error; err != nil {
		return nil, fmt.Errorf("檢查 ID 失敗: %w", err)
	}
	if count > 0 {
		return nil, errors.New("該 ID 已存在")
	}

	// 如果指定了代理，檢查代理是否存在且啟用
	if req.ProxyConfigID != nil && *req.ProxyConfigID > 0 {
		var proxy model.ProxyConfig
		if err := s.db.WithContext(ctx).
			Where("id = ? AND deleted_at IS NULL", *req.ProxyConfigID).
			First(&proxy).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errors.New("指定的代理配置不存在")
			}
			return nil, fmt.Errorf("查詢代理配置失敗: %w", err)
		}
		if !proxy.IsEnabled() {
			return nil, errors.New("指定的代理配置已停用")
		}
	}

	// 建立配置
	config := &model.ConnectorConfig{
		ID:            strings.TrimSpace(req.ID),
		Name:          strings.TrimSpace(req.Name),
		ProxyConfigID: req.ProxyConfigID,
		CountryCodes:  req.CountryCodes,
		Status:        model.ConnectorStatusStopped,
	}

	if err := s.db.WithContext(ctx).Create(config).Error; err != nil {
		return nil, fmt.Errorf("創建配置失敗: %w", err)
	}

	// 重新載入關聯
	s.db.WithContext(ctx).Preload("ProxyConfig").First(config, "id = ?", config.ID)

	logger.Infow("Connector 配置已創建", "connector_id", config.ID)

	// 如果要求自動啟動
	if req.AutoStart {
		if err := s.StartConnector(ctx, config.ID); err != nil {
			// 啟動失敗，配置已創建，重新載入最新狀態
			s.db.WithContext(ctx).Preload("ProxyConfig").First(config, "id = ?", config.ID)
			return config, fmt.Errorf("配置已創建，但啟動失敗: %w", err)
		}
		// 重新載入啟動後的狀態
		s.db.WithContext(ctx).Preload("ProxyConfig").First(config, "id = ?", config.ID)
	}

	return config, nil
}

// UpdateConnectorConfig 更新 Connector 配置
func (s *connectorConfigService) UpdateConnectorConfig(ctx context.Context, id string, req *model.ConnectorConfigUpdateRequest) error {
	// 取得現有配置
	config, err := s.GetConnectorConfig(ctx, id)
	if err != nil {
		return err
	}

	// 如果正在運行，不允許更新代理綁定
	if config.IsRunning() && req.ProxyConfigID != nil {
		return errors.New("Connector 正在運行，請先停止後再更改代理綁定")
	}

	// 準備更新
	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = strings.TrimSpace(req.Name)
	}

	// 處理 accept_new_device 更新
	if req.AcceptNewDevice != nil {
		updates["accept_new_device"] = *req.AcceptNewDevice
	}

	// 處理國碼更新
	if req.CountryCodes != nil {
		updates["country_codes"] = pq.StringArray(req.CountryCodes)
	}

	// 處理代理綁定更新
	if req.ProxyConfigID != nil {
		if *req.ProxyConfigID == 0 {
			// 解除綁定
			updates["proxy_config_id"] = nil
		} else {
			// 檢查新代理是否存在且啟用
			var proxy model.ProxyConfig
			if err := s.db.WithContext(ctx).
				Where("id = ? AND deleted_at IS NULL", *req.ProxyConfigID).
				First(&proxy).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errors.New("指定的代理配置不存在")
				}
				return fmt.Errorf("查詢代理配置失敗: %w", err)
			}
			if !proxy.IsEnabled() {
				return errors.New("指定的代理配置已停用")
			}
			updates["proxy_config_id"] = *req.ProxyConfigID
		}
	}

	if len(updates) == 0 {
		return nil
	}

	updates["updated_at"] = time.Now()

	if err := s.db.WithContext(ctx).Model(&model.ConnectorConfig{}).
		Where("id = ?", id).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("更新配置失敗: %w", err)
	}

	logger.Infow("Connector 配置已更新", "connector_id", id)
	return nil
}

// DeleteConnectorConfig 刪除 Connector 配置
func (s *connectorConfigService) DeleteConnectorConfig(ctx context.Context, id string) error {
	// 取得現有配置
	config, err := s.GetConnectorConfig(ctx, id)
	if err != nil {
		return err
	}

	// 如果正在運行，先停止
	if config.IsRunning() {
		if err := s.StopConnector(ctx, id); err != nil {
			logger.Warnw("停止 Connector 失敗，繼續刪除", "connector_id", id, "error", err)
		}
	}

	// 軟刪除
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&model.ConnectorConfig{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"deleted_at": &now,
			"status":     model.ConnectorStatusStopped,
		}).Error; err != nil {
		return fmt.Errorf("刪除配置失敗: %w", err)
	}

	logger.Infow("Connector 配置已刪除", "connector_id", id)
	return nil
}

// GetConnectorConfig 取得單一配置（含代理資訊）
func (s *connectorConfigService) GetConnectorConfig(ctx context.Context, id string) (*model.ConnectorConfig, error) {
	var config model.ConnectorConfig
	if err := s.db.WithContext(ctx).
		Preload("ProxyConfig").
		Where("id = ? AND deleted_at IS NULL", id).
		First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("Connector 配置不存在")
		}
		return nil, fmt.Errorf("查詢配置失敗: %w", err)
	}
	return &config, nil
}

// GetConnectorConfigList 取得配置列表
func (s *connectorConfigService) GetConnectorConfigList(ctx context.Context, query *model.ConnectorConfigListQuery) ([]model.ConnectorConfigListItem, int64, error) {
	db := s.db.WithContext(ctx).Model(&model.ConnectorConfig{}).Where("connector_configs.deleted_at IS NULL")

	// 狀態篩選
	if query.Status != "" {
		db = db.Where("connector_configs.status = ?", query.Status)
	}

	// 代理篩選
	if query.ProxyConfigID != nil {
		db = db.Where("connector_configs.proxy_config_id = ?", *query.ProxyConfigID)
	}

	// 關鍵字搜尋
	if query.Keyword != "" {
		keyword := "%" + query.Keyword + "%"
		db = db.Where("connector_configs.id LIKE ? OR connector_configs.name LIKE ?", keyword, keyword)
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

	// 查詢（含代理資訊）
	var configs []model.ConnectorConfig
	if err := s.db.WithContext(ctx).
		Preload("ProxyConfig").
		Where("deleted_at IS NULL").
		Order("created_at DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&configs).Error; err != nil {
		return nil, 0, fmt.Errorf("查詢列表失敗: %w", err)
	}

	// 轉換為列表項目，交叉比對 Redis 心跳修正即時狀態
	items := make([]model.ConnectorConfigListItem, len(configs))
	for i, cfg := range configs {
		if cfg.Status == model.ConnectorStatusRunning {
			heartbeatKey := protocol.GetConnectorHeartbeatKey(cfg.ID)
			if exists, err := s.redis.Exists(ctx, heartbeatKey).Result(); err == nil && exists == 0 {
				cfg.Status = model.ConnectorStatusStopped
			}
		}
		var proxyName string
		if cfg.ProxyConfig != nil {
			proxyName = cfg.ProxyConfig.Name
		}
		items[i] = model.ConnectorConfigListItem{
			ConnectorConfig: cfg,
			ProxyName:       proxyName,
		}
	}

	return items, total, nil
}

// BindProxy 綁定代理
func (s *connectorConfigService) BindProxy(ctx context.Context, id string, proxyConfigID uint) error {
	// 取得現有配置
	config, err := s.GetConnectorConfig(ctx, id)
	if err != nil {
		return err
	}

	// 如果正在運行，不允許更改
	if config.IsRunning() {
		return errors.New("Connector 正在運行，請先停止後再更改代理綁定")
	}

	// 檢查代理是否存在且啟用
	var proxy model.ProxyConfig
	if err := s.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", proxyConfigID).
		First(&proxy).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("代理配置不存在")
		}
		return fmt.Errorf("查詢代理配置失敗: %w", err)
	}
	if !proxy.IsEnabled() {
		return errors.New("代理配置已停用")
	}

	// 更新綁定
	if err := s.db.WithContext(ctx).Model(&model.ConnectorConfig{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"proxy_config_id": proxyConfigID,
			"updated_at":      time.Now(),
		}).Error; err != nil {
		return fmt.Errorf("綁定代理失敗: %w", err)
	}

	logger.Infow("Connector 已綁定代理", "connector_id", id, "proxy_config_id", proxyConfigID)
	return nil
}

// UnbindProxy 解除代理綁定
func (s *connectorConfigService) UnbindProxy(ctx context.Context, id string) error {
	// 取得現有配置
	config, err := s.GetConnectorConfig(ctx, id)
	if err != nil {
		return err
	}

	// 如果正在運行，不允許更改
	if config.IsRunning() {
		return errors.New("Connector 正在運行，請先停止後再解除代理綁定")
	}

	// 更新
	if err := s.db.WithContext(ctx).Model(&model.ConnectorConfig{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"proxy_config_id": nil,
			"updated_at":      time.Now(),
		}).Error; err != nil {
		return fmt.Errorf("解除綁定失敗: %w", err)
	}

	logger.Infow("Connector 已解除代理綁定", "connector_id", id)
	return nil
}

// StartConnector 啟動 Connector（透過管理命令）
func (s *connectorConfigService) StartConnector(ctx context.Context, id string) error {
	// 取得配置（含代理資訊）
	config, err := s.GetConnectorConfig(ctx, id)
	if err != nil {
		return err
	}

	// 更新狀態為啟動中
	if err := s.updateStatus(ctx, id, model.ConnectorStatusStarting, ""); err != nil {
		return err
	}

	// 發送管理命令
	cmd := protocol.NewManageCommand(protocol.ManageStartConnector, id)
	if err := s.gateway.SendManageCommandAndWait(ctx, cmd, 30*time.Second); err != nil {
		_ = s.updateStatus(ctx, id, model.ConnectorStatusError, err.Error())
		return fmt.Errorf("啟動 Connector 失敗: %w", err)
	}

	logger.Infow("Connector 已啟動", "connector_id", id, "has_proxy", config.ProxyConfigID != nil)
	return nil
}

// StopConnector 停止 Connector（透過管理命令）
func (s *connectorConfigService) StopConnector(ctx context.Context, id string) error {
	if err := s.updateStatus(ctx, id, model.ConnectorStatusStopping, ""); err != nil {
		return err
	}

	cmd := protocol.NewManageCommand(protocol.ManageStopConnector, id)
	if err := s.gateway.SendManageCommandAndWait(ctx, cmd, 30*time.Second); err != nil {
		logger.Warnw("停止 Connector 失敗", "connector_id", id, "error", err)
	}

	if err := s.updateStatus(ctx, id, model.ConnectorStatusStopped, ""); err != nil {
		return err
	}

	logger.Infow("Connector 已停止", "connector_id", id)
	return nil
}

// RestartConnector 重啟 Connector（透過管理命令）
func (s *connectorConfigService) RestartConnector(ctx context.Context, id string) error {
	cmd := protocol.NewManageCommand(protocol.ManageRestartConnector, id)
	if err := s.gateway.SendManageCommandAndWait(ctx, cmd, 60*time.Second); err != nil {
		return fmt.Errorf("重啟 Connector 失敗: %w", err)
	}

	logger.Infow("Connector 已重啟", "connector_id", id)
	return nil
}

// GetConnectorStatus 取得 Connector 運行狀態（從 Redis 心跳讀取）
func (s *connectorConfigService) GetConnectorStatus(ctx context.Context, id string) (*ConnectorStatus, error) {
	// 檢查心跳是否存在（判斷是否存活）
	heartbeatKey := protocol.GetConnectorHeartbeatKey(id)
	exists, err := s.redis.Exists(ctx, heartbeatKey).Result()
	if err != nil {
		return nil, fmt.Errorf("檢查心跳失敗: %w", err)
	}
	if exists == 0 {
		return nil, nil // 未運行
	}

	// 從 Redis Hash 讀取狀態資訊
	infoKey := protocol.GetConnectorInfoKey(id)
	info, err := s.redis.HGetAll(ctx, infoKey).Result()
	if err != nil || len(info) == 0 {
		// 有心跳但沒有 info，回傳基本狀態
		return &ConnectorStatus{ID: id}, nil
	}

	status := &ConnectorStatus{ID: id}
	if v, ok := info["account_count"]; ok {
		status.AccountCount, _ = strconv.Atoi(v)
	}
	if v, ok := info["start_time"]; ok {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			status.StartTime = time.Unix(ts, 0)
			status.Uptime = time.Since(status.StartTime)
		}
	}
	if v, ok := info["account_ids"]; ok {
		_ = json.Unmarshal([]byte(v), &status.AccountIDs)
	}

	return status, nil
}

// updateStatus 更新狀態
func (s *connectorConfigService) updateStatus(ctx context.Context, id, status, errorMsg string) error {
	updates := map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	}
	if errorMsg != "" {
		updates["error_msg"] = errorMsg
	} else {
		updates["error_msg"] = ""
	}

	return s.db.WithContext(ctx).Model(&model.ConnectorConfig{}).
		Where("id = ?", id).
		Updates(updates).Error
}

