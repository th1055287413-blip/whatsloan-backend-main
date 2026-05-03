package contract

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	handler "whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/llm"
	"whatsapp_golang/internal/service/contract"
	systemSvc "whatsapp_golang/internal/service/system"
)

type Handler struct {
	service *contract.Service
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{
		service: contract.NewService(db),
	}
}

func NewHandlerWithLLM(db *gorm.DB, llmClient *llm.Client, configSvc systemSvc.ConfigService) *Handler {
	return &Handler{
		service: contract.NewServiceWithLLM(db, llmClient, configSvc),
	}
}

func (h *Handler) CreateContract(c *gin.Context) {
	var req contract.CreateContractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	resp, err := h.service.CreateContract(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": resp})
}

func (h *Handler) GetPublicContract(c *gin.Context) {
	contractID := c.Param("id")

	contract, err := h.service.GetContract(contractID)
	if err != nil {
		if err.Error() == "contract expired" {
			c.JSON(http.StatusGone, gin.H{"code": 1, "message": "合同已过期"})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "合同不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": contract})
}

func (h *Handler) SubmitContract(c *gin.Context) {
	contractID := c.Param("id")

	var req contract.SubmitContractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	if err := h.service.SubmitContract(contractID, &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"success": true, "message": "提交成功"}})
}

func (h *Handler) ListContracts(c *gin.Context) {
	contracts, err := h.service.ListContracts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": contracts})
}

func (h *Handler) DeleteContract(c *gin.Context) {
	contractID := c.Param("id")
	if err := h.service.DeleteContract(contractID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

func (h *Handler) UpdateContract(c *gin.Context) {
	contractID := c.Param("id")
	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	if err := h.service.UpdateContract(contractID, req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "更新成功"})
}

func (h *Handler) GenerateSample(c *gin.Context) {
	var req struct {
		Keyword  string `json:"keyword"`
		Language string `json:"language"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	if req.Language == "" {
		req.Language = "zh"
	}

	sample, err := h.service.GenerateSample(c.Request.Context(), req.Keyword, req.Language)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sample})
}

// -------- Agent (workgroup) endpoints --------

func (h *Handler) AgentCreateContract(c *gin.Context) {
	var req contract.CreateContractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		handler.Error(c, handler.CodeInvalidParams, err.Error())
		return
	}

	wgIDAny, ok := c.Get("workgroup_id")
	if !ok {
		handler.Error(c, handler.CodeAuthFailed, "未登入")
		return
	}
	agentIDAny, ok := c.Get("agent_id")
	if !ok {
		handler.Error(c, handler.CodeAuthFailed, "未登入")
		return
	}

	wgID, _ := wgIDAny.(uint)
	agentID, _ := agentIDAny.(uint)

	resp, err := h.service.CreateContractForWorkgroup(&req, wgID, agentID)
	if err != nil {
		handler.Error(c, handler.CodeInternalError, err.Error())
		return
	}

	handler.Success(c, resp)
}

func (h *Handler) AgentListContracts(c *gin.Context) {
	wgIDAny, ok := c.Get("workgroup_id")
	if !ok {
		handler.Error(c, handler.CodeAuthFailed, "未登入")
		return
	}
	wgID, _ := wgIDAny.(uint)

	contracts, err := h.service.ListContractsByWorkgroup(wgID)
	if err != nil {
		handler.Error(c, handler.CodeInternalError, err.Error())
		return
	}

	handler.Success(c, contracts)
}

func (h *Handler) AgentDeleteContract(c *gin.Context) {
	contractID := c.Param("id")

	wgIDAny, ok := c.Get("workgroup_id")
	if !ok {
		handler.Error(c, handler.CodeAuthFailed, "未登入")
		return
	}
	wgID, _ := wgIDAny.(uint)

	// Ensure this contract belongs to current workgroup
	ctr, err := h.service.GetContract(contractID)
	if err != nil {
		handler.Error(c, handler.CodeResourceNotFound, "合同不存在")
		return
	}
	if ctr.WorkgroupID == nil || *ctr.WorkgroupID != wgID {
		handler.Error(c, handler.CodePermissionDenied, "無權操作此合同")
		return
	}

	if err := h.service.DeleteContract(contractID); err != nil {
		handler.Error(c, handler.CodeInternalError, err.Error())
		return
	}

	handler.SuccessWithMessage(c, "删除成功", gin.H{"success": true})
}

func (h *Handler) AgentGenerateSample(c *gin.Context) {
	var req struct {
		Keyword  string `json:"keyword"`
		Language string `json:"language"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handler.Error(c, handler.CodeInvalidParams, err.Error())
		return
	}
	if req.Language == "" {
		req.Language = "zh"
	}

	sample, err := h.service.GenerateSample(c.Request.Context(), req.Keyword, req.Language)
	if err != nil {
		handler.Error(c, handler.CodeInternalError, err.Error())
		return
	}

	handler.Success(c, sample)
}
