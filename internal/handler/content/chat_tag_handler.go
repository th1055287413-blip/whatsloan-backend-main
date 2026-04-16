package content

import (
	"errors"
	"strconv"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ChatTagHandler 聊天室標籤處理器
type ChatTagHandler struct {
	chatTagService contentSvc.ChatTagService
}

// NewChatTagHandler 創建處理器
func NewChatTagHandler(chatTagService contentSvc.ChatTagService) *ChatTagHandler {
	return &ChatTagHandler{
		chatTagService: chatTagService,
	}
}

// ListTags 獲取標籤列表
func (h *ChatTagHandler) ListTags(c *gin.Context) {
	params := common.ParsePaginationParams(c)

	filters := make(map[string]interface{})
	if accountID := c.Query("account_id"); accountID != "" {
		if id, err := strconv.ParseUint(accountID, 10, 32); err == nil {
			filters["account_id"] = uint(id)
		}
	}
	if tag := c.Query("tag"); tag != "" {
		filters["tag"] = tag
	}
	if source := c.Query("source"); source != "" {
		filters["source"] = source
	}
	if chatID := c.Query("chat_id"); chatID != "" {
		filters["chat_id"] = chatID
	}

	tags, total, err := h.chatTagService.ListTags(params.Page, params.PageSize, filters)
	if err != nil {
		common.HandleDatabaseError(c, err, "查詢標籤")
		return
	}

	common.PaginatedList(c, tags, total, params.Page, params.PageSize)
}

// CreateTagRequest 建立標籤請求
type CreateTagRequest struct {
	ChatID    string `json:"chat_id" binding:"required"`
	AccountID uint   `json:"account_id" binding:"required"`
	Tag       string `json:"tag" binding:"required"`
	Source    string `json:"source"`
}

// CreateTag 手動新增標籤
func (h *ChatTagHandler) CreateTag(c *gin.Context) {
	var req CreateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, "參數錯誤: "+err.Error())
		return
	}

	source := req.Source
	if source == "" {
		source = "manual"
	}

	tag := &model.ChatTag{
		ChatID:    req.ChatID,
		AccountID: req.AccountID,
		Tag:       req.Tag,
		Category:  "manual",
		Source:    source,
	}

	if err := h.chatTagService.CreateTag(tag); err != nil {
		common.HandleDatabaseError(c, err, "新增標籤")
		return
	}

	common.Success(c, tag)
}

// DeleteTag 刪除標籤
func (h *ChatTagHandler) DeleteTag(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.chatTagService.DeleteTag(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.HandleNotFoundError(c, "標籤")
			return
		}
		common.HandleDatabaseError(c, err, "刪除標籤")
		return
	}

	common.Success(c, nil)
}

// GetStats 取得標籤統計
func (h *ChatTagHandler) GetStats(c *gin.Context) {
	stats, err := h.chatTagService.GetTagStats()
	if err != nil {
		common.HandleDatabaseError(c, err, "取得統計")
		return
	}

	common.Success(c, stats)
}

// TriggerSync 手動觸發同步
func (h *ChatTagHandler) TriggerSync(c *gin.Context) {
	if err := h.chatTagService.SyncFromSensitiveWordAlerts(); err != nil {
		common.HandleDatabaseError(c, err, "同步標籤")
		return
	}

	common.Success(c, map[string]string{"message": "同步完成"})
}
