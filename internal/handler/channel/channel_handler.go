package channel

import (
	"fmt"
	"net/url"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	channelSvc "whatsapp_golang/internal/service/channel"
	systemSvc "whatsapp_golang/internal/service/system"

	"github.com/gin-gonic/gin"
)

// ChannelHandler 渠道處理器
type ChannelHandler struct {
	channelService channelSvc.ChannelService
	opLogService   systemSvc.OperationLogService
}

// NewChannelHandler 創建渠道處理器實例
func NewChannelHandler(channelService channelSvc.ChannelService, opLogService systemSvc.OperationLogService) *ChannelHandler {
	return &ChannelHandler{
		channelService: channelService,
		opLogService:   opLogService,
	}
}

// CreateChannel 創建渠道
func (h *ChannelHandler) CreateChannel(c *gin.Context) {
	var req model.ChannelCreateRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	channel, err := h.channelService.CreateChannel(&req)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log create operation
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpCreate,
		ResourceType:  model.ResChannel,
		ResourceID:    fmt.Sprint(channel.ID),
		ResourceName:  channel.ChannelName,
		AfterValue: map[string]interface{}{
			"channel_name": channel.ChannelName,
			"channel_code": channel.ChannelCode,
			"status":       channel.Status,
		},
	}, c)

	common.SuccessWithMessage(c, "創建渠道成功", channel)
}

// UpdateChannel 更新渠道
func (h *ChannelHandler) UpdateChannel(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// Get before value for logging
	beforeChannel, _ := h.channelService.GetChannel(id)

	var req model.ChannelUpdateRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.channelService.UpdateChannel(id, &req); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log update operation
	var resourceName string
	var beforeValue map[string]interface{}
	if beforeChannel != nil {
		resourceName = beforeChannel.ChannelName
		beforeValue = map[string]interface{}{
			"channel_name": beforeChannel.ChannelName,
			"status":       beforeChannel.Status,
		}
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUpdate,
		ResourceType:  model.ResChannel,
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
		BeforeValue:   beforeValue,
		AfterValue: map[string]interface{}{
			"channel_name": req.ChannelName,
			"remark":       req.Remark,
		},
	}, c)

	common.SuccessWithMessage(c, "更新渠道成功", nil)
}

// DeleteChannel 刪除渠道
func (h *ChannelHandler) DeleteChannel(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// Get before value for logging
	beforeChannel, _ := h.channelService.GetChannel(id)

	var handleReq model.ChannelDeleteRequest
	_ = c.ShouldBindJSON(&handleReq)

	if err := h.channelService.DeleteChannel(id, &handleReq); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log delete operation
	var resourceName string
	var beforeValue map[string]interface{}
	if beforeChannel != nil {
		resourceName = beforeChannel.ChannelName
		beforeValue = map[string]interface{}{
			"channel_name": beforeChannel.ChannelName,
			"channel_code": beforeChannel.ChannelCode,
			"status":       beforeChannel.Status,
		}
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpDelete,
		ResourceType:  model.ResChannel,
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
		BeforeValue:   beforeValue,
	}, c)

	common.SuccessWithMessage(c, "刪除渠道成功", nil)
}

// GetChannel 獲取渠道詳情
func (h *ChannelHandler) GetChannel(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	channel, err := h.channelService.GetChannel(id)
	if err != nil {
		common.HandleNotFoundError(c, "渠道")
		return
	}

	common.SuccessWithMessage(c, "獲取渠道成功", channel)
}

// GetChannelList 獲取渠道列表
func (h *ChannelHandler) GetChannelList(c *gin.Context) {
	params := common.ParsePaginationParams(c)

	query := &model.ChannelListQuery{
		Page:     params.Page,
		PageSize: params.PageSize,
		Status:   c.Query("status"),
		Keyword:  c.Query("keyword"),
	}

	items, total, err := h.channelService.GetChannelList(query)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.PaginatedList(c, items, total, params.Page, params.PageSize)
}

// UpdateChannelStatus 更新渠道狀態
func (h *ChannelHandler) UpdateChannelStatus(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// Get before value for logging
	beforeChannel, _ := h.channelService.GetChannel(id)

	var req model.ChannelStatusRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.channelService.UpdateChannelStatus(id, req.Status); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log status change
	var resourceName string
	var beforeStatus string
	if beforeChannel != nil {
		resourceName = beforeChannel.ChannelName
		beforeStatus = beforeChannel.Status
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpStatusChange,
		ResourceType:  model.ResChannel,
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
		BeforeValue:   map[string]interface{}{"status": beforeStatus},
		AfterValue:    map[string]interface{}{"status": req.Status},
	}, c)

	common.SuccessWithMessage(c, "更新渠道狀態成功", nil)
}

// GenerateChannelCode 生成渠道號
func (h *ChannelHandler) GenerateChannelCode(c *gin.Context) {
	lang := c.Query("lang")
	if lang == "" {
		lang = "zh" // 默认中文
	}

	// 验证语言参数
	if lang != "zh" && lang != "ms" && lang != "en" {
		common.Error(c, common.CodeInvalidParams, "无效的语言参数，只支持 zh、ms、en")
		return
	}

	code, err := h.channelService.GenerateChannelCode(lang)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "生成渠道號成功", gin.H{
		"channel_code": code,
	})
}

// GetChannelByCode 根據渠道號獲取渠道資訊
func (h *ChannelHandler) GetChannelByCode(c *gin.Context) {
	code := c.Query("code")
	if code == "" {
		common.Error(c, common.CodeInvalidParams, "渠道號不能為空")
		return
	}

	channel, err := h.channelService.GetChannelByCode(code)
	if err != nil {
		common.HandleNotFoundError(c, "渠道")
		return
	}

	common.SuccessWithMessage(c, "獲取渠道成功", channel)
}

// GetAdPixels 公開 API：根據渠道號取得追蹤 pixels
func (h *ChannelHandler) GetAdPixels(c *gin.Context) {
	code := c.Param("channel_code")
	if code == "" {
		c.JSON(200, gin.H{"pixels": []interface{}{}, "loan_type": ""})
		return
	}

	pixels, loanType, err := h.channelService.GetChannelPixels(code)
	if err != nil {
		c.JSON(200, gin.H{"pixels": []interface{}{}, "loan_type": ""})
		return
	}

	c.JSON(200, gin.H{"pixels": pixels, "loan_type": loanType})
}

// GetAdPixelsByDomain 公開 API：根據 Origin/Referer header 或 request host 取得該 domain 的預設 pixels
func (h *ChannelHandler) GetAdPixelsByDomain(c *gin.Context) {
	host := c.Request.Host
	// 優先從 Origin 取（cross-origin 請求）
	if origin := c.GetHeader("Origin"); origin != "" {
		if u, err := url.Parse(origin); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
	} else if referer := c.GetHeader("Referer"); referer != "" {
		// same-origin 請求沒有 Origin，從 Referer 取
		if u, err := url.Parse(referer); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
	}
	pixels, loanType, err := h.channelService.GetPixelsByDomain(host)
	if err != nil {
		c.JSON(200, gin.H{"pixels": []interface{}{}, "loan_type": ""})
		return
	}

	c.JSON(200, gin.H{"pixels": pixels, "loan_type": loanType})
}

// GetChannelIsolationConfig 獲取渠道隔離配置
func (h *ChannelHandler) GetChannelIsolationConfig(c *gin.Context) {
	enabled, err := h.channelService.GetChannelIsolationEnabled()
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "獲取配置成功", gin.H{
		"enabled": enabled,
	})
}

// UpdateChannelIsolationConfig 更新渠道隔離配置
func (h *ChannelHandler) UpdateChannelIsolationConfig(c *gin.Context) {
	var req struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.channelService.SetChannelIsolationEnabled(*req.Enabled); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "更新配置成功", nil)
}

// SetViewerPassword 設定渠道查看密碼
func (h *ChannelHandler) SetViewerPassword(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req model.ViewerPasswordRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.channelService.SetViewerPassword(id, req.Password); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "設定查看密碼成功", nil)
}
