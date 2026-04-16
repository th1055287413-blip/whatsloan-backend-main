package agent

import (
	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	systemSvc "whatsapp_golang/internal/service/system"
)

// ActivityLogHandler 組長審計日誌處理器
type ActivityLogHandler struct {
	opLogService systemSvc.OperationLogService
}

// NewActivityLogHandler 建立審計日誌處理器
func NewActivityLogHandler(opLogService systemSvc.OperationLogService) *ActivityLogHandler {
	return &ActivityLogHandler{opLogService: opLogService}
}

// allowedOpTypes 組長可查詢的操作類型
var allowedOpTypes = map[string]bool{
	model.OpSend:      true,
	model.OpRevoke:    true,
	model.OpDelete:    true,
	model.OpArchive:   true,
	model.OpUnarchive: true,
}

// GetActivityLogs 查詢組員操作紀錄
func (h *ActivityLogHandler) GetActivityLogs(c *gin.Context) {
	wgID, ok := getWorkgroupID(c)
	if !ok {
		return
	}

	var filter model.OperationLogFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		common.Error(c, common.CodeInvalidParams, "參數錯誤")
		return
	}
	filter.SetDefaults()
	filter.WorkgroupID = &wgID

	// Validate operation_type if provided
	if filter.OperationType != "" && !allowedOpTypes[filter.OperationType] {
		common.Error(c, common.CodeInvalidParams, "不支援的操作類型")
		return
	}

	logs, total, err := h.opLogService.GetList(&filter)
	if err != nil {
		common.Error(c, common.CodeInternalError, "查詢失敗")
		return
	}

	common.PaginatedList(c, logs, total, filter.Page, filter.PageSize)
}
