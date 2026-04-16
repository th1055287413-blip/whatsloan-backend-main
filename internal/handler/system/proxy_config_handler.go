package system

import (
	"fmt"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	connectorSvc "whatsapp_golang/internal/service/connector"
	systemSvc "whatsapp_golang/internal/service/system"

	"github.com/gin-gonic/gin"
)

// ProxyConfigHandler 代理配置處理器
type ProxyConfigHandler struct {
	service      connectorSvc.ProxyConfigService
	opLogService systemSvc.OperationLogService
}

// NewProxyConfigHandler 建立處理器實例
func NewProxyConfigHandler(service connectorSvc.ProxyConfigService, opLogService systemSvc.OperationLogService) *ProxyConfigHandler {
	return &ProxyConfigHandler{
		service:      service,
		opLogService: opLogService,
	}
}

// CreateProxyConfig 創建代理配置
// POST /api/admin/proxy-configs
func (h *ProxyConfigHandler) CreateProxyConfig(c *gin.Context) {
	var req model.ProxyConfigCreateRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	config, err := h.service.CreateProxyConfig(c.Request.Context(), &req)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 記錄操作日誌
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpCreate,
		ResourceType:  "proxy",
		ResourceID:    fmt.Sprint(config.ID),
		ResourceName:  config.Name,
		AfterValue: map[string]interface{}{
			"name": config.Name,
			"host": config.Host,
			"port": config.Port,
			"type": config.Type,
		},
	}, c)

	common.SuccessWithMessage(c, "創建代理配置成功", config)
}

// UpdateProxyConfig 更新代理配置
// PUT /api/admin/proxy-configs/:id
func (h *ProxyConfigHandler) UpdateProxyConfig(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// 取得更新前的值
	beforeConfig, _ := h.service.GetProxyConfig(c.Request.Context(), id)

	var req model.ProxyConfigUpdateRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.service.UpdateProxyConfig(c.Request.Context(), id, &req); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 記錄操作日誌
	var resourceName string
	var beforeValue map[string]interface{}
	if beforeConfig != nil {
		resourceName = beforeConfig.Name
		beforeValue = map[string]interface{}{
			"name": beforeConfig.Name,
			"host": beforeConfig.Host,
			"port": beforeConfig.Port,
		}
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUpdate,
		ResourceType:  "proxy",
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
		BeforeValue:   beforeValue,
		AfterValue: map[string]interface{}{
			"name": req.Name,
			"host": req.Host,
			"port": req.Port,
		},
	}, c)

	common.SuccessWithMessage(c, "更新代理配置成功", nil)
}

// DeleteProxyConfig 刪除代理配置
// DELETE /api/admin/proxy-configs/:id
func (h *ProxyConfigHandler) DeleteProxyConfig(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// 取得刪除前的值
	beforeConfig, _ := h.service.GetProxyConfig(c.Request.Context(), id)

	if err := h.service.DeleteProxyConfig(c.Request.Context(), id); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 記錄操作日誌
	var resourceName string
	var beforeValue map[string]interface{}
	if beforeConfig != nil {
		resourceName = beforeConfig.Name
		beforeValue = map[string]interface{}{
			"name": beforeConfig.Name,
			"host": beforeConfig.Host,
			"port": beforeConfig.Port,
		}
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpDelete,
		ResourceType:  "proxy",
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
		BeforeValue:   beforeValue,
	}, c)

	common.SuccessWithMessage(c, "刪除代理配置成功", nil)
}

// GetProxyConfig 取得單一代理配置
// GET /api/admin/proxy-configs/:id
func (h *ProxyConfigHandler) GetProxyConfig(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	config, err := h.service.GetProxyConfig(c.Request.Context(), id)
	if err != nil {
		common.HandleNotFoundError(c, "代理配置")
		return
	}

	common.SuccessWithMessage(c, "取得代理配置成功", config)
}

// GetProxyConfigList 取得代理配置列表
// GET /api/admin/proxy-configs
func (h *ProxyConfigHandler) GetProxyConfigList(c *gin.Context) {
	params := common.ParsePaginationParams(c)

	query := &model.ProxyConfigListQuery{
		Page:     params.Page,
		PageSize: params.PageSize,
		Status:   c.Query("status"),
		Keyword:  c.Query("keyword"),
	}

	items, total, err := h.service.GetProxyConfigList(c.Request.Context(), query)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.PaginatedList(c, items, total, params.Page, params.PageSize)
}

// GetEnabledProxyConfigs 取得所有啟用的代理配置（下拉選單用）
// GET /api/admin/proxy-configs/options
func (h *ProxyConfigHandler) GetEnabledProxyConfigs(c *gin.Context) {
	configs, err := h.service.GetEnabledProxyConfigs(c.Request.Context())
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "取得代理配置列表成功", configs)
}

// UpdateProxyConfigStatus 更新代理狀態
// PUT /api/admin/proxy-configs/:id/status
func (h *ProxyConfigHandler) UpdateProxyConfigStatus(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req struct {
		Status string `json:"status" binding:"required,oneof=enabled disabled"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	// 取得更新前的值
	beforeConfig, _ := h.service.GetProxyConfig(c.Request.Context(), id)

	if err := h.service.UpdateProxyConfigStatus(c.Request.Context(), id, req.Status); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 記錄操作日誌
	var resourceName string
	var beforeStatus string
	if beforeConfig != nil {
		resourceName = beforeConfig.Name
		beforeStatus = beforeConfig.Status
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpStatusChange,
		ResourceType:  "proxy",
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
		BeforeValue:   map[string]interface{}{"status": beforeStatus},
		AfterValue:    map[string]interface{}{"status": req.Status},
	}, c)

	common.SuccessWithMessage(c, "更新代理狀態成功", nil)
}
