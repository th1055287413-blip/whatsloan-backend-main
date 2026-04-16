package content

import (
	"time"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"

	"github.com/gin-gonic/gin"
)

// CustomerConversationHandler 客户咨询对话处理器
type CustomerConversationHandler struct {
	conversationService contentSvc.CustomerConversationService
}

// NewCustomerConversationHandler 创建客户咨询对话处理器实例
func NewCustomerConversationHandler(conversationService contentSvc.CustomerConversationService) *CustomerConversationHandler {
	return &CustomerConversationHandler{
		conversationService: conversationService,
	}
}

// RecordConversation 记录对话（公开接口，供客户端调用）
func (h *CustomerConversationHandler) RecordConversation(c *gin.Context) {
	var conversation model.CustomerConversation
	if !common.BindAndValidate(c, &conversation) {
		return
	}

	// 获取客户端IP
	conversation.IPAddress = c.ClientIP()
	// 获取User-Agent
	conversation.UserAgent = c.GetHeader("User-Agent")

	if err := h.conversationService.RecordConversation(&conversation); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "记录成功", conversation)
}

// GetConversationList 获取对话列表
func (h *CustomerConversationHandler) GetConversationList(c *gin.Context) {
	params := common.ParsePaginationParams(c)

	filters := make(map[string]interface{})

	// 用户标识
	if userIdentifier := c.Query("user_identifier"); userIdentifier != "" {
		filters["user_identifier"] = userIdentifier
	}

	// 会话ID
	if sessionID := c.Query("session_id"); sessionID != "" {
		filters["session_id"] = sessionID
	}

	// 是否匹配
	if isMatched := c.Query("is_matched"); isMatched != "" {
		filters["is_matched"] = isMatched == "true"
	}

	// 关键词ID
	if keywordID := c.Query("keyword_id"); keywordID != "" {
		if id, err := common.ParseUintFromString(keywordID); err == nil {
			filters["keyword_id"] = id
		}
	}

	// 时间范围
	if startDate := c.Query("start_date"); startDate != "" {
		if t, err := time.Parse("2006-01-02", startDate); err == nil {
			filters["start_date"] = t
		}
	}
	if endDate := c.Query("end_date"); endDate != "" {
		if t, err := time.Parse("2006-01-02", endDate); err == nil {
			// 将结束时间设为当天23:59:59
			filters["end_date"] = t.Add(24*time.Hour - time.Second)
		}
	}

	// 搜索
	if search := c.Query("search"); search != "" {
		filters["search"] = search
	}

	conversations, total, err := h.conversationService.GetConversationList(params.Page, params.PageSize, filters)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.PaginatedList(c, conversations, total, params.Page, params.PageSize)
}

// GetSessionConversations 获取会话详情
func (h *CustomerConversationHandler) GetSessionConversations(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		common.Error(c, common.CodeInvalidParams, "会话ID不能为空")
		return
	}

	conversations, err := h.conversationService.GetSessionConversations(sessionID)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "获取成功", conversations)
}

// GetStats 获取统计数据
func (h *CustomerConversationHandler) GetStats(c *gin.Context) {
	var startDate, endDate *time.Time

	// 时间范围
	if startDateStr := c.Query("start_date"); startDateStr != "" {
		if t, err := time.Parse("2006-01-02", startDateStr); err == nil {
			startDate = &t
		}
	}
	if endDateStr := c.Query("end_date"); endDateStr != "" {
		if t, err := time.Parse("2006-01-02", endDateStr); err == nil {
			t = t.Add(24*time.Hour - time.Second)
			endDate = &t
		}
	}

	stats, err := h.conversationService.GetStats(startDate, endDate)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "获取统计成功", stats)
}

// GetKeywordMatchStats 获取关键词匹配统计
func (h *CustomerConversationHandler) GetKeywordMatchStats(c *gin.Context) {
	var startDate, endDate *time.Time
	limit := 10 // 默认返回前10个

	// 时间范围
	if startDateStr := c.Query("start_date"); startDateStr != "" {
		if t, err := time.Parse("2006-01-02", startDateStr); err == nil {
			startDate = &t
		}
	}
	if endDateStr := c.Query("end_date"); endDateStr != "" {
		if t, err := time.Parse("2006-01-02", endDateStr); err == nil {
			t = t.Add(24*time.Hour - time.Second)
			endDate = &t
		}
	}

	// 限制数量
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := common.ParseUintFromString(limitStr); err == nil {
			limit = int(l)
		}
	}

	stats, err := h.conversationService.GetKeywordMatchStats(startDate, endDate, limit)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "获取统计成功", stats)
}

// GetActiveSessions 获取活跃会话列表
func (h *CustomerConversationHandler) GetActiveSessions(c *gin.Context) {
	limit := 20
	offset := 0

	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := common.ParseUintFromString(limitStr); err == nil {
			limit = int(l)
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := common.ParseUintFromString(offsetStr); err == nil {
			offset = int(o)
		}
	}

	sessions, total, err := h.conversationService.GetActiveSessions(limit, offset)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "获取成功", gin.H{
		"list":   sessions,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// AdminReplyRequest 管理员回复请求
type AdminReplyRequest struct {
	Content string `json:"content" binding:"required"`
}

// AdminReply 管理员回复
func (h *CustomerConversationHandler) AdminReply(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		common.Error(c, common.CodeInvalidParams, "会话ID不能为空")
		return
	}

	var req AdminReplyRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	// 从上下文获取管理员信息
	userID, _ := c.Get("user_id")
	username, _ := c.Get("username")

	adminIDUint, ok := userID.(uint)
	if !ok {
		common.Error(c, common.CodeAuthFailed, "无法获取管理员信息")
		return
	}

	adminNameStr, _ := username.(string)

	conversation, err := h.conversationService.AdminReply(sessionID, req.Content, adminIDUint, adminNameStr)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "回复成功", conversation)
}

// GetSessionMessagesPublic 获取会话消息（公开接口，供客户端轮询）
func (h *CustomerConversationHandler) GetSessionMessagesPublic(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		common.Error(c, common.CodeInvalidParams, "会话ID不能为空")
		return
	}

	conversations, err := h.conversationService.GetSessionConversations(sessionID)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "获取成功", conversations)
}
