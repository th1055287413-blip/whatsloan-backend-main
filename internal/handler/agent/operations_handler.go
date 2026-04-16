package agent

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	agentSvc "whatsapp_golang/internal/service/agent"
	contentSvc "whatsapp_golang/internal/service/content"
	messagingSvc "whatsapp_golang/internal/service/messaging"
	systemSvc "whatsapp_golang/internal/service/system"
)

// OperationsHandler Agent 帳號操作處理器
type OperationsHandler struct {
	svc            agentSvc.AgentOperationsService
	chatTagService contentSvc.ChatTagService
	messageAction  messagingSvc.MessageActionService
	opLogService   systemSvc.OperationLogService
}

// NewOperationsHandler 建立 Agent 操作處理器
func NewOperationsHandler(svc agentSvc.AgentOperationsService, chatTagService contentSvc.ChatTagService, messageAction messagingSvc.MessageActionService, opLogService systemSvc.OperationLogService) *OperationsHandler {
	return &OperationsHandler{svc: svc, chatTagService: chatTagService, messageAction: messageAction, opLogService: opLogService}
}

func getAgentID(c *gin.Context) (uint, bool) {
	aid, exists := c.Get("agent_id")
	if !exists {
		common.Error(c, common.CodeAuthFailed, "未登入")
		return 0, false
	}
	id, ok := aid.(uint)
	if !ok {
		common.Error(c, common.CodeInternalError, "Agent 資訊錯誤")
		return 0, false
	}
	return id, true
}

// GetMyAccounts 我的帳號列表
func (h *OperationsHandler) GetMyAccounts(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	p := common.ParsePaginationParams(c)

	accounts, total, err := h.svc.GetMyAccounts(agentID, p.Page, p.PageSize)
	if err != nil {
		common.HandleServiceError(c, err, "帳號")
		return
	}
	common.PaginatedList(c, accounts, total, p.Page, p.PageSize)
}

// GetAccountDetail 帳號詳情
func (h *OperationsHandler) GetAccountDetail(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	accountID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	account, err := h.svc.GetAccountDetail(agentID, accountID)
	if err != nil {
		common.HandleServiceError(c, err, "帳號")
		return
	}
	common.Success(c, account)
}

// GetAccountChats 帳號對話列表
func (h *OperationsHandler) GetAccountChats(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	accountID, ok := common.MustParseID(c)
	if !ok {
		return
	}
	p := common.ParsePaginationParams(c)
	search := c.Query("keyword")
	var archived *bool
	if v := c.Query("archived"); v != "" {
		b := v == "true"
		archived = &b
	}

	chats, total, err := h.svc.GetAccountChats(agentID, accountID, p.Page, p.PageSize, search, archived)
	if err != nil {
		common.HandleServiceError(c, err, "對話")
		return
	}

	srcType, srcAgentName := h.svc.GetAccountSourceInfo(accountID)
	si := &common.ChatSourceInfo{SourceType: srcType, SourceAgentName: srcAgentName}
	common.Success(c, common.BuildChatsResponse(chats, total, p.Page, p.PageSize, accountID, h.chatTagService, si))
}

// GetAccountChatCounts 帳號對話數量統計
func (h *OperationsHandler) GetAccountChatCounts(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	accountID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	counts, err := h.svc.GetAccountChatCounts(agentID, accountID)
	if err != nil {
		common.HandleServiceError(c, err, "對話統計")
		return
	}
	common.Success(c, counts)
}

// GetChatMessages 對話訊息
func (h *OperationsHandler) GetChatMessages(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	accountID, ok := common.MustParseID(c)
	if !ok {
		return
	}
	chatJID := c.Param("jid")
	if chatJID == "" {
		common.Error(c, common.CodeInvalidParams, "缺少 jid 參數")
		return
	}
	p := common.ParsePaginationParams(c)
	targetLanguage := c.Query("target_language")

	messages, total, err := h.svc.GetChatMessages(agentID, accountID, chatJID, p.Page, p.PageSize, targetLanguage)
	if err != nil {
		common.HandleServiceError(c, err, "訊息")
		return
	}

	common.Success(c, common.BuildMessagesResponse(messages, total, p.Page, p.PageSize, chatJID))
}

// SendMessage 發送訊息
func (h *OperationsHandler) SendMessage(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	accountID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req agentSvc.SendMessageRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.svc.SendMessage(c.Request.Context(), agentID, accountID, req); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpSend,
		ResourceType:  model.ResMessage,
		ResourceID:    fmt.Sprintf("account:%d", accountID),
		AfterValue: map[string]interface{}{
			"account_id":  accountID,
			"to_jid":      req.ToJID,
			"media_type":  req.MediaType,
		},
	}, c)

	common.Success(c, nil)
}

// GetWorkgroupAccountStats 工作組帳號統計
func (h *OperationsHandler) GetWorkgroupAccountStats(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}

	stats, err := h.svc.GetWorkgroupAccountStats(agentID)
	if err != nil {
		common.HandleServiceError(c, err, "帳號統計")
		return
	}
	common.Success(c, stats)
}

// GetUserDataList 客戶資料列表
func (h *OperationsHandler) GetUserDataList(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	p := common.ParsePaginationParams(c)

	items, total, err := h.svc.GetUserDataList(agentID, p.Page, p.PageSize)
	if err != nil {
		common.HandleServiceError(c, err, "客戶資料")
		return
	}
	common.PaginatedList(c, items, total, p.Page, p.PageSize)
}

// GetUserDataByPhone 特定客戶資料
func (h *OperationsHandler) GetUserDataByPhone(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	phone := c.Param("phone")
	if phone == "" {
		common.Error(c, common.CodeInvalidParams, "缺少 phone 參數")
		return
	}

	ud, err := h.svc.GetUserDataByPhone(agentID, phone)
	if err != nil {
		common.HandleServiceError(c, err, "客戶資料")
		return
	}
	common.Success(c, ud)
}

// CreateChat 建立新對話
func (h *OperationsHandler) CreateChat(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	accountID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req struct {
		Phone string `json:"phone" binding:"required"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	chat, err := h.svc.CreateChat(agentID, accountID, req.Phone)
	if err != nil {
		common.HandleServiceError(c, err, "對話")
		return
	}
	common.Success(c, chat)
}

// RevokeMessage 撤回訊息
func (h *OperationsHandler) RevokeMessage(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	messageID, ok := common.MustParseUintParam(c, "message_id")
	if !ok {
		return
	}

	accountID, err := h.svc.VerifyMessageAccess(agentID, messageID)
	if err != nil {
		common.HandleServiceError(c, err, "訊息")
		return
	}

	var revokedBy string
	if a, ok := c.Get("agent"); ok {
		if agent, ok := a.(*model.Agent); ok {
			revokedBy = "agent:" + agent.Username
		}
	}

	if err := h.messageAction.RevokeMessage(c.Request.Context(), messageID, accountID, revokedBy); err != nil {
		errMsg := err.Error()
		switch errMsg {
		case "只能撤銷自己發送的消息":
			common.HandleForbidden(c, errMsg)
		case "消息已被刪除,無法撤銷", "消息已被撤銷", "超過撤銷時間限制(24小時)":
			common.Error(c, common.CodeInvalidParams, errMsg)
		default:
			common.Error(c, common.CodeInternalError, "撤銷消息失敗")
		}
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpRevoke,
		ResourceType:  model.ResMessage,
		ResourceID:    fmt.Sprintf("%d", messageID),
		AfterValue: map[string]interface{}{
			"account_id": accountID,
			"message_id": messageID,
		},
	}, c)

	common.Success(c, nil)
}

// DeleteMessageForMe 刪除訊息（僅自己裝置）
func (h *OperationsHandler) DeleteMessageForMe(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	messageID, ok := common.MustParseUintParam(c, "message_id")
	if !ok {
		return
	}

	if _, err := h.svc.VerifyMessageAccess(agentID, messageID); err != nil {
		common.HandleServiceError(c, err, "訊息")
		return
	}

	// 用 agent username 作為 deletedBy
	var deletedBy string
	if a, ok := c.Get("agent"); ok {
		if agent, ok := a.(*model.Agent); ok {
			deletedBy = agent.Username
		}
	}

	if err := h.messageAction.DeleteMessageForMe(c.Request.Context(), messageID, deletedBy); err != nil {
		errMsg := err.Error()
		switch errMsg {
		case "消息不存在":
			common.HandleNotFoundError(c, "消息")
		case "消息已被刪除":
			common.Error(c, common.CodeInvalidParams, errMsg)
		default:
			common.Error(c, common.CodeInternalError, "刪除消息失敗")
		}
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpDelete,
		ResourceType:  model.ResMessage,
		ResourceID:    fmt.Sprintf("%d", messageID),
		AfterValue: map[string]interface{}{
			"message_id": messageID,
			"deleted_by": deletedBy,
		},
	}, c)

	common.Success(c, nil)
}

// GetUnifiedChats 跨帳號統一聊天列表
func (h *OperationsHandler) GetUnifiedChats(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}

	p := common.ParsePaginationParams(c)
	search := c.Query("keyword")
	var archived *bool
	if v := c.Query("archived"); v != "" {
		b := v == "true"
		archived = &b
	}
	var accountID *uint
	if v := c.Query("account_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			uid := uint(id)
			accountID = &uid
		}
	}
	var pinned *bool
	if v := c.Query("pinned"); v != "" {
		b := v == "true"
		pinned = &b
	}

	rows, total, err := h.svc.GetUnifiedChats(agentID, p.Page, p.PageSize, search, archived, accountID, pinned)
	if err != nil {
		common.HandleServiceError(c, err, "對話")
		return
	}

	common.PaginatedList(c, rows, total, p.Page, p.PageSize)
}

// PinChat 釘選聊天
func (h *OperationsHandler) PinChat(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}

	chatID, ok := common.MustParseUintParam(c, "chatId")
	if !ok {
		return
	}

	if err := h.svc.PinChat(agentID, chatID); err != nil {
		common.HandleServiceError(c, err, "釘選")
		return
	}

	common.Success(c, nil)
}

// UnpinChat 取消釘選
func (h *OperationsHandler) UnpinChat(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}

	chatID, ok := common.MustParseUintParam(c, "chatId")
	if !ok {
		return
	}

	if err := h.svc.UnpinChat(agentID, chatID); err != nil {
		common.HandleServiceError(c, err, "取消釘選")
		return
	}

	common.Success(c, nil)
}

// GetAccountContacts 帳號聯絡人列表
func (h *OperationsHandler) GetAccountContacts(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	accountID, ok := common.MustParseID(c)
	if !ok {
		return
	}
	p := common.ParsePaginationParams(c)
	search := c.Query("keyword")

	contacts, total, err := h.svc.GetAccountContacts(agentID, accountID, p.Page, p.PageSize, search)
	if err != nil {
		common.HandleServiceError(c, err, "聯絡人")
		return
	}

	common.PaginatedList(c, contacts, total, p.Page, p.PageSize)
}

// ExportAccountContacts 匯出帳號聯絡人
func (h *OperationsHandler) ExportAccountContacts(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}
	accountID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	csvData, err := h.svc.ExportAccountContacts(agentID, accountID)
	if err != nil {
		common.HandleServiceError(c, err, "聯絡人")
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=contacts.csv")
	c.Data(200, "text/csv; charset=utf-8", csvData)
}
