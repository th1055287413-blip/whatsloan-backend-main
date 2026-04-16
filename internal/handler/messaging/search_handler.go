package messaging

import (
	"errors"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	messagingSvc "whatsapp_golang/internal/service/messaging"
)

// SearchHandler 消息搜索處理器
type SearchHandler struct {
	searchService messagingSvc.MessageSearchService
}

// NewSearchHandler 創建消息搜索處理器
func NewSearchHandler(searchService messagingSvc.MessageSearchService) *SearchHandler {
	return &SearchHandler{
		searchService: searchService,
	}
}

// SearchMessages 搜索消息
// @Summary 搜索消息
// @Description 根據關鍵詞和篩選條件搜索消息
// @Tags Message
// @Accept json
// @Produce json
// @Param body body model.MessageSearchRequest true "搜索參數"
// @Success 200 {object} common.APIResponse{data=model.MessageSearchResult}
// @Router /api/whatsapp/messages/search [post]
func (h *SearchHandler) SearchMessages(c *gin.Context) {
	var req model.MessageSearchRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := req.Validate(); err != nil {
		logger.Ctx(c.Request.Context()).Warnw("搜尋請求參數驗證失敗", "error", err)
		common.HandleValidationError(c, err)
		return
	}

	result, err := h.searchService.SearchMessages(c.Request.Context(), &req)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("搜尋訊息失敗", "error", err)
		common.Error(c, common.CodeInternalError, "搜索失敗，請稍後重試")
		return
	}

	common.Success(c, result)
}

// GetMessageContext 獲取消息上下文
// @Summary 獲取消息上下文
// @Description 獲取指定消息的上下文(前後N條消息)
// @Tags Message
// @Accept json
// @Produce json
// @Param message_id path int true "消息ID"
// @Param before query int false "之前的消息數量" default(3)
// @Param after query int false "之後的消息數量" default(3)
// @Success 200 {object} common.APIResponse{data=model.MessageContextResult}
// @Router /api/whatsapp/messages/{message_id}/context [get]
func (h *SearchHandler) GetMessageContext(c *gin.Context) {
	messageID, ok := common.MustParseUintParam(c, "message_id")
	if !ok {
		return
	}

	var req model.MessageContextRequest
	req.MessageID = messageID

	// 解析 before 參數
	if beforeStr := c.Query("before"); beforeStr != "" {
		before, err := common.ParseIntParam(c, "before")
		if err != nil || before < 0 || before > 20 {
			common.Error(c, common.CodeInvalidParams, "before 參數必須在 0-20 之間")
			return
		}
		req.Before = before
	}

	// 解析 after 參數
	if afterStr := c.Query("after"); afterStr != "" {
		after, err := common.ParseIntParam(c, "after")
		if err != nil || after < 0 || after > 20 {
			common.Error(c, common.CodeInvalidParams, "after 參數必須在 0-20 之間")
			return
		}
		req.After = after
	}

	if err := req.Validate(); err != nil {
		logger.Ctx(c.Request.Context()).Warnw("上下文請求參數驗證失敗", "error", err)
		common.HandleValidationError(c, err)
		return
	}

	result, err := h.searchService.GetMessageContext(c.Request.Context(), req.MessageID, req.Before, req.After)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || err.Error() == "消息不存在" {
			logger.Ctx(c.Request.Context()).Warnw("訊息不存在", "message_id", req.MessageID)
			common.HandleNotFoundError(c, "消息")
			return
		}
		logger.Ctx(c.Request.Context()).Errorw("取得訊息上下文失敗", "message_id", req.MessageID, "error", err)
		common.Error(c, common.CodeInternalError, "獲取失敗，請稍後重試")
		return
	}

	common.Success(c, result)
}
