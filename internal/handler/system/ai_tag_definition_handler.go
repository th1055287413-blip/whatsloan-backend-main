package system

import (
	"strconv"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"

	"github.com/gin-gonic/gin"
)

type AiTagDefinitionHandler struct {
	svc contentSvc.AiTagDefinitionService
}

func NewAiTagDefinitionHandler(svc contentSvc.AiTagDefinitionService) *AiTagDefinitionHandler {
	return &AiTagDefinitionHandler{svc: svc}
}

func (h *AiTagDefinitionHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))

	defs, total, err := h.svc.List(page, pageSize)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}
	common.SuccessList(c, defs, total)
}

func (h *AiTagDefinitionHandler) Create(c *gin.Context) {
	var def model.AiTagDefinition
	if err := c.ShouldBindJSON(&def); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}
	if err := h.svc.Create(&def); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}
	common.Created(c, def)
}

func (h *AiTagDefinitionHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var def model.AiTagDefinition
	if err := c.ShouldBindJSON(&def); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}
	def.ID = uint(id)
	if err := h.svc.Update(&def); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}
	common.Success(c, def)
}

func (h *AiTagDefinitionHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	if err := h.svc.Delete(uint(id)); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}
	common.SuccessWithMessage(c, "刪除成功", nil)
}
