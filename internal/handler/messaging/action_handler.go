package messaging

import (
	"fmt"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	messagingSvc "whatsapp_golang/internal/service/messaging"
	systemSvc "whatsapp_golang/internal/service/system"

	"github.com/gin-gonic/gin"
)

// ActionHandler 消息操作處理器
type ActionHandler struct {
	messageActionService messagingSvc.MessageActionService
	opLogService         systemSvc.OperationLogService
}

// NewActionHandler 創建消息操作處理器
func NewActionHandler(messageActionService messagingSvc.MessageActionService, opLogService systemSvc.OperationLogService) *ActionHandler {
	return &ActionHandler{
		messageActionService: messageActionService,
		opLogService:         opLogService,
	}
}

// DeleteMessage 刪除消息
// @Summary 刪除消息
// @Description 管理員刪除消息(軟刪除)
// @Tags Message
// @Accept json
// @Produce json
// @Param message_id path int true "消息ID"
// @Security Bearer
// @Success 200 {object} common.APIResponse
// @Router /messages/{message_id} [delete]
func (h *ActionHandler) DeleteMessage(c *gin.Context) {
	messageID, ok := common.MustParseUintParam(c, "message_id")
	if !ok {
		return
	}

	// 獲取當前用戶資訊
	userInterface, exists := c.Get("user")
	if !exists {
		common.HandleUnauthorized(c)
		return
	}

	// 使用類型斷言獲取用戶名
	adminUser, ok := userInterface.(*model.AdminUser)
	if !ok || adminUser.Username == "" {
		common.Error(c, common.CodeAuthFailed, "無法獲取用戶資訊")
		return
	}
	username := adminUser.Username

	if err := h.messageActionService.DeleteMessage(c.Request.Context(), messageID, username); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("刪除訊息失敗", "message_id", messageID, "error", err)

		errMsg := err.Error()
		if errMsg == "消息不存在" {
			common.HandleNotFoundError(c, "消息")
			return
		}
		if errMsg == "消息已被刪除" {
			common.Error(c, common.CodeInvalidParams, errMsg)
			return
		}

		common.Error(c, common.CodeInternalError, "刪除消息失敗")
		return
	}

	// Log delete operation
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpDelete,
		ResourceType:  model.ResMessage,
		ResourceID:    fmt.Sprint(messageID),
		AfterValue: map[string]interface{}{
			"deleted":    true,
			"deleted_by": username,
		},
	}, c)

	common.Success(c, nil)
}

// DeleteMessageForMe 僅刪除自己裝置上的訊息
// @Summary 刪除訊息（僅自己）
// @Description 管理員刪除訊息，僅在自己所有裝置上刪除，不撤銷對方的訊息
// @Tags Message
// @Accept json
// @Produce json
// @Param message_id path int true "消息ID"
// @Security Bearer
// @Success 200 {object} common.APIResponse
// @Router /messages/{message_id}/delete-for-me [post]
func (h *ActionHandler) DeleteMessageForMe(c *gin.Context) {
	messageID, ok := common.MustParseUintParam(c, "message_id")
	if !ok {
		return
	}

	userInterface, exists := c.Get("user")
	if !exists {
		common.HandleUnauthorized(c)
		return
	}

	adminUser, ok := userInterface.(*model.AdminUser)
	if !ok || adminUser.Username == "" {
		common.Error(c, common.CodeAuthFailed, "無法獲取用戶資訊")
		return
	}
	username := adminUser.Username

	if err := h.messageActionService.DeleteMessageForMe(c.Request.Context(), messageID, username); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("DeleteForMe 失敗", "message_id", messageID, "error", err)

		errMsg := err.Error()
		if errMsg == "消息不存在" {
			common.HandleNotFoundError(c, "消息")
			return
		}
		if errMsg == "消息已被刪除" {
			common.Error(c, common.CodeInvalidParams, errMsg)
			return
		}

		common.Error(c, common.CodeInternalError, "DeleteForMe 失敗")
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpDelete,
		ResourceType:  model.ResMessage,
		ResourceID:    fmt.Sprint(messageID),
		AfterValue: map[string]interface{}{
			"delete_for_me": true,
			"deleted_by":    username,
		},
	}, c)

	common.Success(c, nil)
}

// RevokeMessage 撤銷消息
// @Summary 撤銷消息
// @Description 用戶撤銷自己發送的消息(24小時內)
// @Tags Message
// @Accept json
// @Produce json
// @Param message_id path int true "消息ID"
// @Security Bearer
// @Success 200 {object} common.APIResponse
// @Router /messages/{message_id}/revoke [post]
func (h *ActionHandler) RevokeMessage(c *gin.Context) {
	messageID, ok := common.MustParseUintParam(c, "message_id")
	if !ok {
		return
	}

	// RevokeMessage 的第二個參數應該是消息所屬的 account_id
	// 這裡傳 0，讓 service 層自己從消息中獲取 account_id
	accountID := uint(0)

	// 取得操作者名稱
	var revokedBy string
	if userInterface, exists := c.Get("user"); exists {
		if adminUser, ok := userInterface.(*model.AdminUser); ok {
			revokedBy = adminUser.Username
		}
	}

	if err := h.messageActionService.RevokeMessage(c.Request.Context(), messageID, accountID, revokedBy); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("撤銷訊息失敗", "message_id", messageID, "error", err)
		errMsg := err.Error()

		switch errMsg {
		case "消息不存在":
			common.HandleNotFoundError(c, "消息")
		case "只能撤銷自己發送的消息":
			common.HandleForbidden(c, errMsg)
		case "消息已被刪除,無法撤銷", "消息已被撤銷", "超過撤銷時間限制(24小時)":
			common.Error(c, common.CodeInvalidParams, errMsg)
		default:
			common.Error(c, common.CodeInternalError, "撤銷消息失敗: "+errMsg)
		}
		return
	}

	// Log revoke operation
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpRevoke,
		ResourceType:  model.ResMessage,
		ResourceID:    fmt.Sprint(messageID),
		AfterValue: map[string]interface{}{
			"revoked": true,
		},
	}, c)

	common.Success(c, nil)
}

// RegisterRoutes 註冊消息操作路由
func RegisterRoutes(rg *gin.RouterGroup, handler *ActionHandler, authMiddleware gin.HandlerFunc) {
	messages := rg.Group("/messages")
	messages.Use(authMiddleware)
	{
		messages.DELETE("/:message_id", handler.DeleteMessage)
		messages.POST("/:message_id/delete-for-me", handler.DeleteMessageForMe)
		messages.POST("/:message_id/revoke", handler.RevokeMessage)
	}
}
