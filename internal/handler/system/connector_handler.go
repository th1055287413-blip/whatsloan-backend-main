package system

import (
	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/gateway"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
)

// ConnectorHandler Connector 狀態處理器
type ConnectorHandler struct {
	routing *gateway.RoutingService
}

// NewConnectorHandler 創建 Connector 狀態處理器
func NewConnectorHandler(routing *gateway.RoutingService) *ConnectorHandler {
	return &ConnectorHandler{
		routing: routing,
	}
}

// GetConnectorsStatus 取得所有 Connector 的狀態
func (h *ConnectorHandler) GetConnectorsStatus(c *gin.Context) {
	ctx := c.Request.Context()

	status, err := h.routing.GetConnectorsStatus(ctx)
	if err != nil {
		logger.Ctx(ctx).Errorw("取得 Connector 狀態失敗", "error", err)
		common.Error(c, common.CodeInternalError, "取得 Connector 狀態失敗")
		return
	}

	common.Success(c, status)
}
