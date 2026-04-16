package system

import (
	"strconv"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	systemSvc "whatsapp_golang/internal/service/system"

	"github.com/gin-gonic/gin"
)

// OperationLogHandler handles operation log related requests
type OperationLogHandler struct {
	opLogService systemSvc.OperationLogService
}

// NewOperationLogHandler creates a new operation log handler instance
func NewOperationLogHandler(opLogService systemSvc.OperationLogService) *OperationLogHandler {
	return &OperationLogHandler{
		opLogService: opLogService,
	}
}

// GetOperationLogs returns a paginated list of operation logs
func (h *OperationLogHandler) GetOperationLogs(c *gin.Context) {
	var filter model.OperationLogFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		common.Error(c, common.CodeInvalidParams, "invalid query parameters")
		return
	}

	// Parse operator_id if provided
	if operatorIDStr := c.Query("operator_id"); operatorIDStr != "" {
		if id, err := strconv.ParseUint(operatorIDStr, 10, 64); err == nil {
			uid := uint(id)
			filter.OperatorID = &uid
		}
	}

	logs, total, err := h.opLogService.GetList(&filter)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	filter.SetDefaults()
	common.PaginatedList(c, logs, total, filter.Page, filter.PageSize)
}

// GetOperationLog returns a single operation log by ID
func (h *OperationLogHandler) GetOperationLog(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	log, err := h.opLogService.GetByID(id)
	if err != nil {
		common.HandleNotFoundError(c, "operation log")
		return
	}

	common.Success(c, log)
}
