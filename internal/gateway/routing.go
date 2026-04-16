package gateway

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"whatsapp_golang/internal/config"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

// RoutingService 路由服務
type RoutingService struct {
	redis *redis.Client
	db    *gorm.DB
}

// NewRoutingService 建立路由服務
func NewRoutingService(redisClient *redis.Client, db *gorm.DB) *RoutingService {
	return &RoutingService{
		redis: redisClient,
		db:    db,
	}
}

// GetAccountDBStatus 從 DB 取得帳號狀態
func (r *RoutingService) GetAccountDBStatus(ctx context.Context, accountID uint) (string, error) {
	var status string
	err := r.db.WithContext(ctx).
		Table("whatsapp_accounts").
		Select("status").
		Where("id = ?", accountID).
		Scan(&status).Error
	if err != nil {
		return "", fmt.Errorf("查詢帳號狀態失敗: %w", err)
	}
	return status, nil
}

// GetConnectorForAccount 取得帳號對應的 Connector ID
func (r *RoutingService) GetConnectorForAccount(ctx context.Context, accountID uint) (string, error) {
	var connectorID *string
	err := r.db.WithContext(ctx).
		Table("whatsapp_accounts").
		Select("connector_id").
		Where("id = ?", accountID).
		Scan(&connectorID).Error
	if err != nil {
		return "", fmt.Errorf("查詢路由失敗: %w", err)
	}
	if connectorID == nil || *connectorID == "" {
		return "", fmt.Errorf("帳號 %d 未分配到任何 Connector", accountID)
	}
	return *connectorID, nil
}

// AssignAccountToConnector 將帳號分配到指定 Connector
func (r *RoutingService) AssignAccountToConnector(ctx context.Context, accountID uint, connectorID string) error {
	result := r.db.WithContext(ctx).
		Table("whatsapp_accounts").
		Where("id = ?", accountID).
		Update("connector_id", connectorID)
	if result.Error != nil {
		return fmt.Errorf("設定路由失敗: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("帳號 %d 不存在", accountID)
	}
	logger.Infow("帳號已分配到 Connector", "account_id", accountID, "connector_id", connectorID)
	return nil
}

// RemoveAccountRouting 移除帳號的路由
func (r *RoutingService) RemoveAccountRouting(ctx context.Context, accountID uint) error {
	result := r.db.WithContext(ctx).
		Table("whatsapp_accounts").
		Where("id = ?", accountID).
		Update("connector_id", "")
	if result.Error != nil {
		return fmt.Errorf("移除路由失敗: %w", result.Error)
	}
	logger.Infow("帳號路由已移除", "account_id", accountID)
	return nil
}

// GetActiveConnectors 取得所有存活的 Connector
func (r *RoutingService) GetActiveConnectors(ctx context.Context) ([]string, error) {
	connectors, err := r.redis.SMembers(ctx, protocol.ConnectorsSetKey).Result()
	if err != nil {
		return nil, fmt.Errorf("查詢 Connector 集合失敗: %w", err)
	}

	// 過濾出有心跳的 Connector
	activeConnectors := make([]string, 0, len(connectors))
	for _, connectorID := range connectors {
		heartbeatKey := protocol.GetConnectorHeartbeatKey(connectorID)
		exists, err := r.redis.Exists(ctx, heartbeatKey).Result()
		if err == nil && exists > 0 {
			activeConnectors = append(activeConnectors, connectorID)
		}
	}

	return activeConnectors, nil
}

// ConnectorLoad Connector 負載資訊
type ConnectorLoad struct {
	ConnectorID  string
	AccountCount int
}

// GetConnectorLoads 取得各 Connector 的負載
func (r *RoutingService) GetConnectorLoads(ctx context.Context) ([]ConnectorLoad, error) {
	connectors, err := r.GetActiveConnectors(ctx)
	if err != nil {
		return nil, err
	}

	loads := make([]ConnectorLoad, 0, len(connectors))
	for _, connectorID := range connectors {
		count, err := r.GetAccountCountForConnector(ctx, connectorID)
		if err != nil {
			logger.Warnw("取得 Connector 帳號數量失敗", "connector_id", connectorID, "error", err)
			count = 0
		}
		loads = append(loads, ConnectorLoad{
			ConnectorID:  connectorID,
			AccountCount: count,
		})
	}

	return loads, nil
}

// GetAccountCountForConnector 取得 Connector 管理的帳號數量
func (r *RoutingService) GetAccountCountForConnector(ctx context.Context, connectorID string) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table("whatsapp_accounts").
		Where("connector_id = ?", connectorID).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

// AssignAccountAuto 自動分配帳號到負載最低的 Connector
// 會依帳號的 phone number 國碼匹配對應國家的 connector 群組
// phoneHint: 當 accountID=0（帳號尚未建立）時提供電話號碼以供國碼路由
func (r *RoutingService) AssignAccountAuto(ctx context.Context, accountID uint, phoneHint ...string) (string, error) {
	// accountID=0 表示新登入流程，只選擇 Connector，不寫入路由
	if accountID == 0 {
		// 嘗試用 phoneHint 做國碼路由
		if len(phoneHint) > 0 && phoneHint[0] != "" {
			if connector, err := r.selectConnectorByCountry(ctx, phoneHint[0]); err == nil {
				return connector, nil
			} else {
				logger.Warnw("新登入國碼路由失敗，使用全域分配", "phone", phoneHint[0], "error", err)
			}
		}
		return r.selectLeastLoadedConnector(ctx)
	}

	// 先檢查帳號是否已分配
	existingConnector, err := r.GetConnectorForAccount(ctx, accountID)
	if err == nil && existingConnector != "" {
		// 已分配，檢查該 Connector 是否存活
		if r.IsConnectorAlive(ctx, existingConnector) {
			return existingConnector, nil
		}
		// Connector 已死，但不自動轉移，返回錯誤
		return "", fmt.Errorf("帳號 %d 綁定的 Connector %s 已離線", accountID, existingConnector)
	}

	// 從 DB 查帳號的 phone number，嘗試國碼路由
	var phoneNumber string
	r.db.WithContext(ctx).
		Table("whatsapp_accounts").
		Select("phone_number").
		Where("id = ?", accountID).
		Scan(&phoneNumber)

	selectedConnector, err := r.selectConnectorByCountry(ctx, phoneNumber)
	if err != nil {
		// 國碼匹配失敗，fallback 到全域 least-loaded
		logger.Warnw("國碼路由失敗，使用全域分配", "account_id", accountID, "phone", phoneNumber, "error", err)
		selectedConnector, err = r.selectLeastLoadedConnector(ctx)
		if err != nil {
			return "", err
		}
	}

	if err := r.AssignAccountToConnector(ctx, accountID, selectedConnector); err != nil {
		return "", err
	}

	return selectedConnector, nil
}

// selectConnectorByCountry 依 phone number 國碼篩選 connector，再 least-loaded 選一個
func (r *RoutingService) selectConnectorByCountry(ctx context.Context, phoneNumber string) (string, error) {
	if phoneNumber == "" {
		return "", fmt.Errorf("phone number 為空")
	}

	// 去除 "+" 前綴，確保與 country_codes 格式一致
	phoneNumber = strings.TrimPrefix(phoneNumber, "+")

	// 查出所有有設定 country_codes 的 connector
	var configs []model.ConnectorConfig
	if err := r.db.WithContext(ctx).
		Where("deleted_at IS NULL AND accept_new_device = true AND country_codes IS NOT NULL AND array_length(country_codes, 1) > 0").
		Find(&configs).Error; err != nil {
		return "", fmt.Errorf("查詢 connector 國碼設定失敗: %w", err)
	}

	// 收集所有已註冊的國碼，並建立國碼→connector IDs 映射
	codeToConnectors := make(map[string][]string)
	for _, cfg := range configs {
		for _, code := range cfg.CountryCodes {
			codeToConnectors[code] = append(codeToConnectors[code], cfg.ID)
		}
	}

	// 從長到短比對國碼（最長 3 碼）
	var matchedConnectorIDs []string
	for length := 3; length >= 1; length-- {
		if length > len(phoneNumber) {
			continue
		}
		prefix := phoneNumber[:length]
		if ids, ok := codeToConnectors[prefix]; ok {
			matchedConnectorIDs = ids
			break
		}
	}

	if len(matchedConnectorIDs) == 0 {
		return "", fmt.Errorf("國碼 %s 無匹配的 connector", phoneNumber)
	}

	// 從匹配的 connector 中過濾出 active 的，再 least-loaded
	activeConnectors, err := r.GetActiveConnectors(ctx)
	if err != nil {
		return "", err
	}
	activeSet := make(map[string]bool, len(activeConnectors))
	for _, id := range activeConnectors {
		activeSet[id] = true
	}

	// 收集 active 且匹配國碼的 connector 負載
	var candidates []ConnectorLoad
	for _, id := range matchedConnectorIDs {
		if !activeSet[id] {
			continue
		}
		count, err := r.GetAccountCountForConnector(ctx, id)
		if err != nil {
			count = 0
		}
		candidates = append(candidates, ConnectorLoad{ConnectorID: id, AccountCount: count})
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("國碼匹配的 connector 均不在線")
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].AccountCount < candidates[j].AccountCount
	})

	return candidates[0].ConnectorID, nil
}

// selectLeastLoadedConnector 選擇負載最低的 Connector
func (r *RoutingService) selectLeastLoadedConnector(ctx context.Context) (string, error) {
	loads, err := r.GetConnectorLoads(ctx)
	if err != nil {
		return "", err
	}

	if len(loads) == 0 {
		return "", fmt.Errorf("沒有可用的 Connector")
	}

	// 排除不接受新裝置的 Connector
	var rejected []string
	r.db.WithContext(ctx).Model(&model.ConnectorConfig{}).
		Where("deleted_at IS NULL AND accept_new_device = false").
		Pluck("id", &rejected)
	if len(rejected) > 0 {
		rejectedSet := make(map[string]bool, len(rejected))
		for _, id := range rejected {
			rejectedSet[id] = true
		}
		filtered := loads[:0]
		for _, l := range loads {
			if !rejectedSet[l.ConnectorID] {
				filtered = append(filtered, l)
			}
		}
		loads = filtered
	}

	if len(loads) == 0 {
		return "", fmt.Errorf("沒有可用的 Connector")
	}

	// 按負載排序，選擇最低的
	sort.Slice(loads, func(i, j int) bool {
		return loads[i].AccountCount < loads[j].AccountCount
	})

	return loads[0].ConnectorID, nil
}

// IsConnectorAlive 檢查 Connector 是否存活
func (r *RoutingService) IsConnectorAlive(ctx context.Context, connectorID string) bool {
	heartbeatKey := protocol.GetConnectorHeartbeatKey(connectorID)
	exists, err := r.redis.Exists(ctx, heartbeatKey).Result()
	return err == nil && exists > 0
}

// GetDeadConnectors 取得已死亡的 Connector（在集合中但沒有心跳）
func (r *RoutingService) GetDeadConnectors(ctx context.Context) ([]string, error) {
	connectors, err := r.redis.SMembers(ctx, protocol.ConnectorsSetKey).Result()
	if err != nil {
		return nil, fmt.Errorf("查詢 Connector 集合失敗: %w", err)
	}

	deadConnectors := make([]string, 0)
	for _, connectorID := range connectors {
		if !r.IsConnectorAlive(ctx, connectorID) {
			deadConnectors = append(deadConnectors, connectorID)
		}
	}

	return deadConnectors, nil
}

// CleanupDeadConnector 清理死亡的 Connector
// 只清理 Redis 記錄，不動 DB 帳號狀態（Connector 重啟後會自動恢復連線）
// API 層透過 GetAccountStatus() 驗證 Connector 存活來反映即時狀態
func (r *RoutingService) CleanupDeadConnector(ctx context.Context, connectorID string) error {
	// 從 Connector 集合移除
	if err := r.redis.SRem(ctx, protocol.ConnectorsSetKey, connectorID).Err(); err != nil {
		return fmt.Errorf("從 Connector 集合移除失敗: %w", err)
	}

	logger.Infow("已清理死亡的 Connector", "connector_id", connectorID)
	return nil
}

// GetAccountsForConnector 取得 Connector 管理的所有帳號 ID
func (r *RoutingService) GetAccountsForConnector(ctx context.Context, connectorID string) ([]uint, error) {
	var accountIDs []uint
	err := r.db.WithContext(ctx).
		Table("whatsapp_accounts").
		Where("connector_id = ?", connectorID).
		Pluck("id", &accountIDs).Error
	if err != nil {
		return nil, err
	}
	return accountIDs, nil
}

// ConnectorInfo Connector 資訊
type ConnectorInfo struct {
	ConnectorID   string
	AccountCount  int
	IsAlive       bool
	LastHeartbeat time.Time
}

// ConnectorStatus Connector 狀態（用於 API 回應）
type ConnectorStatus struct {
	ID            string                `json:"id"`
	Alive         bool                  `json:"alive"`
	LastHeartbeat *time.Time            `json:"last_heartbeat,omitempty"`
	Version       string                `json:"version,omitempty"`
	Accounts      ConnectorAccountStats `json:"accounts"`
}

// ConnectorAccountStats Connector 帳號統計
type ConnectorAccountStats struct {
	Total        int `json:"total"`
	Connected    int `json:"connected"`
	Disconnected int `json:"disconnected"`
}

// ConnectorsStatusResponse Connector 狀態列表回應
type ConnectorsStatusResponse struct {
	APIVersion string                  `json:"api_version"`
	Connectors []ConnectorStatus       `json:"connectors"`
	Summary    ConnectorsSummary       `json:"summary"`
}

// ConnectorsSummary 總覽統計
type ConnectorsSummary struct {
	TotalConnectors      int `json:"total_connectors"`
	AliveConnectors      int `json:"alive_connectors"`
	TotalAccounts        int `json:"total_accounts"`
	ConnectedAccounts    int `json:"connected_accounts"`
	DisconnectedAccounts int `json:"disconnected_accounts"`
}

// GetConnectorInfo 取得 Connector 詳細資訊
func (r *RoutingService) GetConnectorInfo(ctx context.Context, connectorID string) (*ConnectorInfo, error) {
	info := &ConnectorInfo{
		ConnectorID: connectorID,
		IsAlive:     r.IsConnectorAlive(ctx, connectorID),
	}

	count, err := r.GetAccountCountForConnector(ctx, connectorID)
	if err == nil {
		info.AccountCount = count
	}

	// 取得最後心跳時間
	heartbeatKey := protocol.GetConnectorHeartbeatKey(connectorID)
	timestamp, err := r.redis.Get(ctx, heartbeatKey).Int64()
	if err == nil {
		info.LastHeartbeat = time.Unix(timestamp, 0)
	}

	return info, nil
}

// GetConnectorsStatus 取得所有 Connector 的狀態
func (r *RoutingService) GetConnectorsStatus(ctx context.Context) (*ConnectorsStatusResponse, error) {
	// 取得所有 Connector（包含死亡的）
	allConnectors, err := r.redis.SMembers(ctx, protocol.ConnectorsSetKey).Result()
	if err != nil {
		return nil, fmt.Errorf("查詢 Connector 集合失敗: %w", err)
	}

	response := &ConnectorsStatusResponse{
		APIVersion: config.GetVersion(),
		Connectors: make([]ConnectorStatus, 0, len(allConnectors)),
	}

	for _, connectorID := range allConnectors {
		status := ConnectorStatus{
			ID:    connectorID,
			Alive: r.IsConnectorAlive(ctx, connectorID),
		}

		// 取得最後心跳時間
		heartbeatKey := protocol.GetConnectorHeartbeatKey(connectorID)
		if timestamp, err := r.redis.Get(ctx, heartbeatKey).Int64(); err == nil {
			t := time.Unix(timestamp, 0)
			status.LastHeartbeat = &t
		}

		// 取得 Connector 資訊（版本等）
		infoKey := protocol.GetConnectorInfoKey(connectorID)
		if infoData, err := r.redis.HGetAll(ctx, infoKey).Result(); err == nil {
			if v, ok := infoData["version"]; ok {
				status.Version = v
			}
		}

		// 取得帳號統計
		stats, err := r.getAccountStatsForConnector(ctx, connectorID)
		if err == nil {
			status.Accounts = stats
		}

		response.Connectors = append(response.Connectors, status)

		// 更新總覽統計
		response.Summary.TotalConnectors++
		if status.Alive {
			response.Summary.AliveConnectors++
		}
		response.Summary.TotalAccounts += status.Accounts.Total
		response.Summary.ConnectedAccounts += status.Accounts.Connected
		response.Summary.DisconnectedAccounts += status.Accounts.Disconnected
	}

	return response, nil
}

// getAccountStatsForConnector 取得 Connector 的帳號統計
func (r *RoutingService) getAccountStatsForConnector(ctx context.Context, connectorID string) (ConnectorAccountStats, error) {
	var stats ConnectorAccountStats

	// 查詢該 Connector 管理的所有帳號及其狀態
	type accountStatus struct {
		Status string
	}
	var accounts []accountStatus
	err := r.db.WithContext(ctx).
		Table("whatsapp_accounts").
		Select("status").
		Where("connector_id = ?", connectorID).
		Scan(&accounts).Error
	if err != nil {
		return stats, err
	}

	stats.Total = len(accounts)
	for _, acc := range accounts {
		if acc.Status == "connected" {
			stats.Connected++
		} else {
			stats.Disconnected++
		}
	}

	return stats, nil
}
