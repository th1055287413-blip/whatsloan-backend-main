package whatsapp

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/gateway"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"
	systemSvc "whatsapp_golang/internal/service/system"
	"whatsapp_golang/internal/service/whatsapp"
)

// ChatHandler WhatsApp 聊天處理器
type ChatHandler struct {
	dataService    whatsapp.DataService
	gateway        *gateway.Gateway
	opLogService   systemSvc.OperationLogService
	chatTagService contentSvc.ChatTagService
}

// NewChatHandler 創建聊天處理器
func NewChatHandler(dataService whatsapp.DataService, gw *gateway.Gateway, opLogService systemSvc.OperationLogService, chatTagService contentSvc.ChatTagService) *ChatHandler {
	return &ChatHandler{
		dataService:    dataService,
		gateway:        gw,
		opLogService:   opLogService,
		chatTagService: chatTagService,
	}
}

// ArchiveChatResponse 歸檔會話響應
type ArchiveChatResponse struct {
	ID         uint       `json:"id"`
	Archived   bool       `json:"archived"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
}

// GetChats 獲取聊天列表
func (h *ChatHandler) GetChats(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	chats, err := h.dataService.GetChats(id)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得聊天列表失敗", "account_id", id, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 批次查詢標籤和 AI 摘要
	chatJIDs := make([]string, 0, len(chats))
	chatIDs := make([]uint, 0, len(chats))
	for _, chat := range chats {
		chatJIDs = append(chatJIDs, chat.JID)
		chatIDs = append(chatIDs, chat.ID)
	}

	var chatTags map[string][]string
	var chatSummaries map[uint]string
	if h.chatTagService != nil && len(chats) > 0 {
		if tags, err := h.chatTagService.GetTagsByChatIDs(id, chatJIDs); err != nil {
			logger.Ctx(c.Request.Context()).Warnw("查詢聊天室標籤失敗", "account_id", id, "error", err)
		} else {
			chatTags = tags
		}
		if sums, err := h.chatTagService.GetSummariesByChatIDs(id, chatIDs); err != nil {
			logger.Ctx(c.Request.Context()).Warnw("查詢 AI 摘要失敗", "account_id", id, "error", err)
		} else {
			chatSummaries = sums
		}
	}

	type chatWithExtra struct {
		*model.WhatsAppChat
		Tags      []string `json:"tags"`
		AISummary *string  `json:"ai_summary"`
	}

	result := make([]chatWithExtra, 0, len(chats))
	for _, chat := range chats {
		item := chatWithExtra{WhatsAppChat: chat}
		if chatTags != nil {
			item.Tags = chatTags[chat.JID]
		}
		if item.Tags == nil {
			item.Tags = []string{}
		}
		if chatSummaries != nil {
			if s, ok := chatSummaries[chat.ID]; ok {
				item.AISummary = &s
			}
		}
		result = append(result, item)
	}

	common.Success(c, result)
}

// GetContacts 獲取帳號的聯絡人列表
func (h *ChatHandler) GetContacts(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// 分頁參數
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	contacts, total, err := h.dataService.GetContacts(id, page, pageSize)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得聯絡人列表失敗", "account_id", id, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, gin.H{
		"items":     contacts,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ArchiveChat 歸檔會話
func (h *ChatHandler) ArchiveChat(c *gin.Context) {
	chatIDStr := c.Param("chatId")
	chatID, err := strconv.ParseUint(chatIDStr, 10, 32)
	if err != nil {
		common.Error(c, common.CodeInvalidParams, "無效的會話 ID")
		return
	}

	// 獲取聊天資訊（需要 JID 和 accountID）
	chat, err := h.dataService.GetChatByID(uint(chatID))
	if err != nil {
		common.Error(c, common.CodeResourceNotFound, "會話不存在")
		return
	}

	// 調用 Gateway 執行 WhatsApp API 歸檔操作
	ctx := context.Background()
	if err := h.gateway.ArchiveChat(ctx, chat.AccountID, chat.JID, uint(chatID), true); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("WhatsApp 歸檔失敗", "chat_id", uint(chatID), "error", err)
		common.Error(c, common.CodeWhatsAppError, "歸檔失敗: "+err.Error())
		return
	}

	// 更新本地 DB
	chat, err = h.dataService.ArchiveChatByID(uint(chatID))
	if err != nil {
		// WhatsApp 已歸檔，但本地 DB 更新失敗，記錄錯誤但不回傳錯誤
		logger.Ctx(c.Request.Context()).Warnw("WhatsApp 已歸檔，但本地 DB 更新失敗", "chat_id", uint(chatID), "error", err)
	}

	logger.Ctx(c.Request.Context()).Infow("會話歸檔成功", "chat_id", uint(chatID), "account_id", chat.AccountID, "jid", chat.JID)

	// Log archive operation
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpArchive,
		ResourceType:  model.ResChat,
		ResourceID:    fmt.Sprint(chatID),
		ResourceName:  chat.Name,
		AfterValue: map[string]interface{}{
			"archived":    true,
			"archived_at": chat.ArchivedAt,
			"jid":         chat.JID,
		},
	}, c)

	common.SuccessWithMessage(c, "會話歸檔成功", ArchiveChatResponse{
		ID:         chat.ID,
		Archived:   chat.Archived,
		ArchivedAt: chat.ArchivedAt,
	})
}

// UnarchiveChat 取消歸檔會話
func (h *ChatHandler) UnarchiveChat(c *gin.Context) {
	chatIDStr := c.Param("chatId")
	chatID, err := strconv.ParseUint(chatIDStr, 10, 32)
	if err != nil {
		common.Error(c, common.CodeInvalidParams, "無效的會話 ID")
		return
	}

	// 獲取聊天資訊（需要 JID 和 accountID）
	chat, err := h.dataService.GetChatByID(uint(chatID))
	if err != nil {
		common.Error(c, common.CodeResourceNotFound, "會話不存在")
		return
	}

	// 調用 Gateway 執行 WhatsApp API 取消歸檔操作
	ctx := context.Background()
	if err := h.gateway.ArchiveChat(ctx, chat.AccountID, chat.JID, uint(chatID), false); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("WhatsApp 取消歸檔失敗", "chat_id", uint(chatID), "error", err)
		common.Error(c, common.CodeWhatsAppError, "取消歸檔失敗: "+err.Error())
		return
	}

	// 更新本地 DB
	chat, err = h.dataService.UnarchiveChatByID(uint(chatID))
	if err != nil {
		// WhatsApp 已取消歸檔，但本地 DB 更新失敗，記錄錯誤但不回傳錯誤
		logger.Ctx(c.Request.Context()).Warnw("WhatsApp 已取消歸檔，但本地 DB 更新失敗", "chat_id", uint(chatID), "error", err)
	}

	logger.Ctx(c.Request.Context()).Infow("會話取消歸檔成功", "chat_id", uint(chatID), "account_id", chat.AccountID, "jid", chat.JID)

	// Log unarchive operation
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUnarchive,
		ResourceType:  model.ResChat,
		ResourceID:    fmt.Sprint(chatID),
		ResourceName:  chat.Name,
		AfterValue: map[string]interface{}{
			"archived": false,
			"jid":      chat.JID,
		},
	}, c)

	common.SuccessWithMessage(c, "會話取消歸檔成功", ArchiveChatResponse{
		ID:         chat.ID,
		Archived:   chat.Archived,
		ArchivedAt: chat.ArchivedAt,
	})
}
