package channel

import (
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	channelSvc "whatsapp_golang/internal/service/channel"

	"github.com/gin-gonic/gin"
)

// PromotionDomainHandler 推廣域名處理器
type PromotionDomainHandler struct {
	domainService channelSvc.PromotionDomainService
}

// NewPromotionDomainHandler 創建推廣域名處理器實例
func NewPromotionDomainHandler(domainService channelSvc.PromotionDomainService) *PromotionDomainHandler {
	return &PromotionDomainHandler{
		domainService: domainService,
	}
}

// CreateDomain 創建域名
func (h *PromotionDomainHandler) CreateDomain(c *gin.Context) {
	var req model.PromotionDomainCreateRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	domain, err := h.domainService.CreateDomain(&req)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "創建域名成功", domain)
}

// UpdateDomain 更新域名
func (h *PromotionDomainHandler) UpdateDomain(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req model.PromotionDomainUpdateRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.domainService.UpdateDomain(id, &req); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "更新域名成功", nil)
}

// DeleteDomain 刪除域名
func (h *PromotionDomainHandler) DeleteDomain(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.domainService.DeleteDomain(id); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "刪除域名成功", nil)
}

// GetDomain 獲取域名詳情
func (h *PromotionDomainHandler) GetDomain(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	domain, err := h.domainService.GetDomain(id)
	if err != nil {
		common.HandleNotFoundError(c, "域名")
		return
	}

	common.SuccessWithMessage(c, "獲取域名成功", domain)
}

// GetDomainList 獲取域名列表
func (h *PromotionDomainHandler) GetDomainList(c *gin.Context) {
	params := common.ParsePaginationParams(c)

	query := &model.PromotionDomainListQuery{
		Page:     params.Page,
		PageSize: params.PageSize,
		Status:   c.Query("status"),
		Keyword:  c.Query("keyword"),
	}

	items, total, err := h.domainService.GetDomainList(query)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.PaginatedList(c, items, total, params.Page, params.PageSize)
}

// UpdateDomainStatus 更新域名狀態
func (h *PromotionDomainHandler) UpdateDomainStatus(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req model.PromotionDomainStatusRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.domainService.UpdateDomainStatus(id, req.Status); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "更新域名狀態成功", nil)
}

// GetEnabledDomains 獲取啟用的域名列表（用於下拉選擇）
func (h *PromotionDomainHandler) GetEnabledDomains(c *gin.Context) {
	domains, err := h.domainService.GetEnabledDomains()
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "獲取域名列表成功", domains)
}
