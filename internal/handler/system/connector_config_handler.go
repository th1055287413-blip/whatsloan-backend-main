package system

import (
	"strconv"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	connectorSvc "whatsapp_golang/internal/service/connector"
	systemSvc "whatsapp_golang/internal/service/system"

	"github.com/gin-gonic/gin"
)

// ConnectorConfigHandler Connector 配置處理器
type ConnectorConfigHandler struct {
	service      connectorSvc.ConnectorConfigService
	opLogService systemSvc.OperationLogService
}

// NewConnectorConfigHandler 建立處理器實例
func NewConnectorConfigHandler(service connectorSvc.ConnectorConfigService, opLogService systemSvc.OperationLogService) *ConnectorConfigHandler {
	return &ConnectorConfigHandler{
		service:      service,
		opLogService: opLogService,
	}
}

// CreateConnectorConfig 創建 Connector 配置
// POST /api/admin/connector-configs
func (h *ConnectorConfigHandler) CreateConnectorConfig(c *gin.Context) {
	var req model.ConnectorConfigCreateRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	config, err := h.service.CreateConnectorConfig(c.Request.Context(), &req)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 記錄操作日誌
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpCreate,
		ResourceType:  model.ResConnector,
		ResourceID:    config.ID,
		ResourceName:  config.Name,
		AfterValue: map[string]interface{}{
			"id":              config.ID,
			"name":            config.Name,
			"proxy_config_id": config.ProxyConfigID,
		},
	}, c)

	common.SuccessWithMessage(c, "創建 Connector 配置成功", config)
}

// UpdateConnectorConfig 更新 Connector 配置
// PUT /api/admin/connector-configs/:id
func (h *ConnectorConfigHandler) UpdateConnectorConfig(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		common.Error(c, common.CodeInvalidParams, "ID 不能為空")
		return
	}

	// 取得更新前的值
	beforeConfig, _ := h.service.GetConnectorConfig(c.Request.Context(), id)

	var req model.ConnectorConfigUpdateRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.service.UpdateConnectorConfig(c.Request.Context(), id, &req); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 記錄操作日誌
	var resourceName string
	var beforeValue map[string]interface{}
	if beforeConfig != nil {
		resourceName = beforeConfig.Name
		beforeValue = map[string]interface{}{
			"name":              beforeConfig.Name,
			"proxy_config_id":   beforeConfig.ProxyConfigID,
			"accept_new_device": beforeConfig.AcceptNewDevice,
		}
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUpdate,
		ResourceType:  model.ResConnector,
		ResourceID:    id,
		ResourceName:  resourceName,
		BeforeValue:   beforeValue,
		AfterValue: map[string]interface{}{
			"name":              req.Name,
			"proxy_config_id":   req.ProxyConfigID,
			"accept_new_device": req.AcceptNewDevice,
		},
	}, c)

	common.SuccessWithMessage(c, "更新 Connector 配置成功", nil)
}

// DeleteConnectorConfig 刪除 Connector 配置
// DELETE /api/admin/connector-configs/:id
func (h *ConnectorConfigHandler) DeleteConnectorConfig(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		common.Error(c, common.CodeInvalidParams, "ID 不能為空")
		return
	}

	// 取得刪除前的值
	beforeConfig, _ := h.service.GetConnectorConfig(c.Request.Context(), id)

	if err := h.service.DeleteConnectorConfig(c.Request.Context(), id); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 記錄操作日誌
	var resourceName string
	var beforeValue map[string]interface{}
	if beforeConfig != nil {
		resourceName = beforeConfig.Name
		beforeValue = map[string]interface{}{
			"id":   beforeConfig.ID,
			"name": beforeConfig.Name,
		}
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpDelete,
		ResourceType:  model.ResConnector,
		ResourceID:    id,
		ResourceName:  resourceName,
		BeforeValue:   beforeValue,
	}, c)

	common.SuccessWithMessage(c, "刪除 Connector 配置成功", nil)
}

// GetConnectorConfig 取得單一 Connector 配置
// GET /api/admin/connector-configs/:id
func (h *ConnectorConfigHandler) GetConnectorConfig(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		common.Error(c, common.CodeInvalidParams, "ID 不能為空")
		return
	}

	config, err := h.service.GetConnectorConfig(c.Request.Context(), id)
	if err != nil {
		common.HandleNotFoundError(c, "Connector 配置")
		return
	}

	common.SuccessWithMessage(c, "取得 Connector 配置成功", config)
}

// GetConnectorConfigList 取得 Connector 配置列表
// GET /api/admin/connector-configs
func (h *ConnectorConfigHandler) GetConnectorConfigList(c *gin.Context) {
	params := common.ParsePaginationParams(c)

	query := &model.ConnectorConfigListQuery{
		Page:     params.Page,
		PageSize: params.PageSize,
		Status:   c.Query("status"),
		Keyword:  c.Query("keyword"),
	}

	// 解析 proxy_config_id
	if proxyIDStr := c.Query("proxy_config_id"); proxyIDStr != "" {
		if proxyID, err := strconv.ParseUint(proxyIDStr, 10, 32); err == nil {
			id := uint(proxyID)
			query.ProxyConfigID = &id
		}
	}

	items, total, err := h.service.GetConnectorConfigList(c.Request.Context(), query)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.PaginatedList(c, items, total, params.Page, params.PageSize)
}

// BindProxy 綁定代理
// POST /api/admin/connector-configs/:id/bind-proxy
func (h *ConnectorConfigHandler) BindProxy(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		common.Error(c, common.CodeInvalidParams, "ID 不能為空")
		return
	}

	var req struct {
		ProxyConfigID uint `json:"proxy_config_id" binding:"required,min=1"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.service.BindProxy(c.Request.Context(), id, req.ProxyConfigID); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 記錄操作日誌
	config, _ := h.service.GetConnectorConfig(c.Request.Context(), id)
	var resourceName string
	if config != nil {
		resourceName = config.Name
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUpdate,
		ResourceType:  model.ResConnector,
		ResourceID:    id,
		ResourceName:  resourceName,
		AfterValue:    map[string]interface{}{"action": "bind_proxy", "proxy_config_id": req.ProxyConfigID},
	}, c)

	common.SuccessWithMessage(c, "綁定代理成功", nil)
}

// UnbindProxy 解除代理綁定
// POST /api/admin/connector-configs/:id/unbind-proxy
func (h *ConnectorConfigHandler) UnbindProxy(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		common.Error(c, common.CodeInvalidParams, "ID 不能為空")
		return
	}

	if err := h.service.UnbindProxy(c.Request.Context(), id); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 記錄操作日誌
	config, _ := h.service.GetConnectorConfig(c.Request.Context(), id)
	var resourceName string
	if config != nil {
		resourceName = config.Name
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUpdate,
		ResourceType:  model.ResConnector,
		ResourceID:    id,
		ResourceName:  resourceName,
		AfterValue:    map[string]interface{}{"action": "unbind_proxy"},
	}, c)

	common.SuccessWithMessage(c, "解除代理綁定成功", nil)
}

// StartConnector 啟動 Connector
// POST /api/admin/connector-configs/:id/start
func (h *ConnectorConfigHandler) StartConnector(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		common.Error(c, common.CodeInvalidParams, "ID 不能為空")
		return
	}

	if err := h.service.StartConnector(c.Request.Context(), id); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 記錄操作日誌
	config, _ := h.service.GetConnectorConfig(c.Request.Context(), id)
	var resourceName string
	if config != nil {
		resourceName = config.Name
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpStatusChange,
		ResourceType:  model.ResConnector,
		ResourceID:    id,
		ResourceName:  resourceName,
		BeforeValue:   map[string]interface{}{"status": model.ConnectorStatusStopped},
		AfterValue:    map[string]interface{}{"status": model.ConnectorStatusRunning},
	}, c)

	common.SuccessWithMessage(c, "Connector 啟動成功", nil)
}

// StopConnector 停止 Connector
// POST /api/admin/connector-configs/:id/stop
func (h *ConnectorConfigHandler) StopConnector(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		common.Error(c, common.CodeInvalidParams, "ID 不能為空")
		return
	}

	if err := h.service.StopConnector(c.Request.Context(), id); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 記錄操作日誌
	config, _ := h.service.GetConnectorConfig(c.Request.Context(), id)
	var resourceName string
	if config != nil {
		resourceName = config.Name
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpStatusChange,
		ResourceType:  model.ResConnector,
		ResourceID:    id,
		ResourceName:  resourceName,
		BeforeValue:   map[string]interface{}{"status": model.ConnectorStatusRunning},
		AfterValue:    map[string]interface{}{"status": model.ConnectorStatusStopped},
	}, c)

	common.SuccessWithMessage(c, "Connector 停止成功", nil)
}

// RestartConnector 重啟 Connector
// POST /api/admin/connector-configs/:id/restart
func (h *ConnectorConfigHandler) RestartConnector(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		common.Error(c, common.CodeInvalidParams, "ID 不能為空")
		return
	}

	if err := h.service.RestartConnector(c.Request.Context(), id); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 記錄操作日誌
	config, _ := h.service.GetConnectorConfig(c.Request.Context(), id)
	var resourceName string
	if config != nil {
		resourceName = config.Name
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpStatusChange,
		ResourceType:  model.ResConnector,
		ResourceID:    id,
		ResourceName:  resourceName,
		AfterValue:    map[string]interface{}{"action": "restart"},
	}, c)

	common.SuccessWithMessage(c, "Connector 重啟成功", nil)
}

// GetConnectorStatus 取得 Connector 運行狀態
// GET /api/admin/connector-configs/:id/status
func (h *ConnectorConfigHandler) GetConnectorStatus(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		common.Error(c, common.CodeInvalidParams, "ID 不能為空")
		return
	}

	status, err := h.service.GetConnectorStatus(c.Request.Context(), id)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	if status == nil {
		common.SuccessWithMessage(c, "Connector 未運行", gin.H{
			"running": false,
		})
		return
	}

	common.SuccessWithMessage(c, "取得 Connector 狀態成功", gin.H{
		"running":       true,
		"account_count": status.AccountCount,
		"account_ids":   status.AccountIDs,
		"uptime":        status.Uptime.String(),
		"start_time":    status.StartTime,
	})
}
