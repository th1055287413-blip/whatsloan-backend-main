package content

import (
	"strings"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"

	"github.com/gin-gonic/gin"
)

// AutoReplyHandler 自动回复处理器
type AutoReplyHandler struct {
	autoReplyService contentSvc.AutoReplyService
}

// NewAutoReplyHandler 创建自动回复处理器实例
func NewAutoReplyHandler(autoReplyService contentSvc.AutoReplyService) *AutoReplyHandler {
	return &AutoReplyHandler{
		autoReplyService: autoReplyService,
	}
}

// CreateKeyword 创建关键词
func (h *AutoReplyHandler) CreateKeyword(c *gin.Context) {
	var keyword model.AutoReplyKeyword
	if !common.BindAndValidate(c, &keyword) {
		return
	}

	if err := h.autoReplyService.CreateKeyword(&keyword); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "创建关键词成功", keyword)
}

// UpdateKeyword 更新关键词
func (h *AutoReplyHandler) UpdateKeyword(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var keyword model.AutoReplyKeyword
	if !common.BindAndValidate(c, &keyword) {
		return
	}

	if err := h.autoReplyService.UpdateKeyword(id, &keyword); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "更新关键词成功", keyword)
}

// DeleteKeyword 删除关键词
func (h *AutoReplyHandler) DeleteKeyword(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.autoReplyService.DeleteKeyword(id); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "删除关键词成功", nil)
}

// GetKeyword 获取关键词详情
func (h *AutoReplyHandler) GetKeyword(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	keyword, err := h.autoReplyService.GetKeyword(id)
	if err != nil {
		common.HandleNotFoundError(c, "关键词")
		return
	}

	common.SuccessWithMessage(c, "获取关键词成功", keyword)
}

// GetKeywordList 获取关键词列表
func (h *AutoReplyHandler) GetKeywordList(c *gin.Context) {
	params := common.ParsePaginationParams(c)
	query := c.Query("query")
	matchType := c.Query("match_type")
	status := c.Query("status")
	language := c.Query("language")
	keywordType := c.Query("keyword_type")

	filters := make(map[string]interface{})
	if query != "" {
		filters["query"] = query
	}
	if matchType != "" {
		filters["match_type"] = model.AutoReplyMatchType(matchType)
	}
	if status != "" {
		filters["status"] = model.AutoReplyStatus(status)
	}
	if language != "" {
		filters["language"] = language
	}
	if keywordType != "" {
		filters["keyword_type"] = keywordType
	}

	keywords, total, err := h.autoReplyService.GetKeywordList(params.Page, params.PageSize, filters)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.PaginatedList(c, keywords, total, params.Page, params.PageSize)
}

// UpdateKeywordStatus 更新关键词状态
func (h *AutoReplyHandler) UpdateKeywordStatus(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req struct {
		Status model.AutoReplyStatus `json:"status" binding:"required"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.autoReplyService.UpdateKeywordStatus(id, req.Status); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "更新状态成功", nil)
}

// BatchDeleteKeywords 批量删除关键词
func (h *AutoReplyHandler) BatchDeleteKeywords(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.autoReplyService.BatchDeleteKeywords(req.IDs); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "批量删除成功", nil)
}

// GetActiveKeywords 获取激活的关键词（公开接口，供客户端使用）
func (h *AutoReplyHandler) GetActiveKeywords(c *gin.Context) {
	language := c.Query("language")
	pageSize := 100 // 默认返回100条

	keywords, err := h.autoReplyService.GetActiveKeywords(language, pageSize)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "获取关键词成功", keywords)
}

// GetWelcomeMessage 获取欢迎语（公开接口，根据语言返回）
func (h *AutoReplyHandler) GetWelcomeMessage(c *gin.Context) {
	language := c.Query("language")

	reply, err := h.autoReplyService.GetWelcomeMessage(language)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "获取欢迎语成功", map[string]string{
		"reply": reply,
	})
}

// MatchKeywordRequest 匹配关键词请求
type MatchKeywordRequest struct {
	UserMessage string `json:"user_message" binding:"required"`
	Language    string `json:"language"`
}

// MatchKeywordResponse 匹配关键词响应
type MatchKeywordResponse struct {
	Matched          bool   `json:"matched"`
	Reply            string `json:"reply"`
	MatchedKeywordID *uint  `json:"matched_keyword_id,omitempty"`
	NeedsHuman       bool   `json:"needs_human"` // 是否需要人工介入
}

// MatchKeyword 匹配关键词（公开接口，供客户端使用）
func (h *AutoReplyHandler) MatchKeyword(c *gin.Context) {
	var req MatchKeywordRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	keyword, reply, err := h.autoReplyService.MatchKeyword(req.UserMessage, req.Language)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 检查是否需要人工介入
	needsHuman := false
	if strings.HasPrefix(reply, "NEEDS_HUMAN:") {
		needsHuman = true
		reply = strings.TrimPrefix(reply, "NEEDS_HUMAN:")
	}

	response := MatchKeywordResponse{
		Matched:    keyword != nil,
		Reply:      reply,
		NeedsHuman: needsHuman,
	}

	if keyword != nil {
		response.MatchedKeywordID = &keyword.ID
	}

	common.SuccessWithMessage(c, "匹配成功", response)
}
