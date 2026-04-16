package whatsapp

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/gateway"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/service/whatsapp"
)

// SyncHandler WhatsApp 同步處理器
type SyncHandler struct {
	dataService whatsapp.DataService
	gateway     *gateway.Gateway
}

// NewSyncHandler 創建同步處理器
func NewSyncHandler(dataService whatsapp.DataService, gw *gateway.Gateway) *SyncHandler {
	return &SyncHandler{
		dataService: dataService,
		gateway:     gw,
	}
}

// SyncChats 手動同步聊天列表
func (h *SyncHandler) SyncChats(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	ctx := context.Background()
	if err := h.gateway.SyncChats(ctx, id); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("同步聊天列表失敗", "account_id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "聊天列表同步已開始，請稍後查看結果"})
}

// UpdateContactNames 手動更新聯絡人名稱
// TODO: 需要實現對應的 Gateway Command
func (h *SyncHandler) UpdateContactNames(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// TODO: 透過 Gateway 發送更新聯絡人名稱命令
	logger.Ctx(c.Request.Context()).Warnw("UpdateContactNames 尚未遷移到 Connector 架構", "account_id", id)
	common.Error(c, common.CodeInternalError, "此功能暫時不可用，正在遷移到新架構")
}

// SyncChatHistory 同步聊天歷史訊息
func (h *SyncHandler) SyncChatHistory(c *gin.Context) {
	var accountIDStr, chatJID, countStr string

	if c.Request.Method == "POST" {
		accountIDStr = c.PostForm("account_id")
		chatJID = c.PostForm("chat_jid")
		countStr = c.PostForm("count")
	}

	if accountIDStr == "" {
		accountIDStr = c.Query("account_id")
	}
	if chatJID == "" {
		chatJID = c.Query("chat_jid")
	}
	if countStr == "" {
		countStr = c.Query("count")
	}

	if accountIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 account_id 參數"})
		return
	}

	accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無效的帳號 ID"})
		return
	}

	if chatJID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 chat_jid 參數"})
		return
	}

	count := 50
	if countStr != "" {
		if cnt, err := strconv.Atoi(countStr); err == nil && cnt > 0 && cnt <= 1000 {
			count = cnt
		}
	}

	ctx := context.Background()
	if err := h.gateway.SyncHistory(ctx, uint(accountID), chatJID, count); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("同步聊天歷史訊息失敗", "account_id", accountID, "chat_jid", chatJID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("聊天 %s 的歷史訊息同步已開始，請稍後查看結果", chatJID)})
}

// SyncAllChatHistory 同步帳號下所有聊天的歷史訊息
func (h *SyncHandler) SyncAllChatHistory(c *gin.Context) {
	accountIDStr := c.Query("account_id")
	if accountIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 account_id 參數"})
		return
	}

	accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無效的帳號 ID"})
		return
	}

	count := 50
	if countStr := c.Query("count"); countStr != "" {
		if cnt, err := strconv.Atoi(countStr); err == nil && cnt > 0 && cnt <= 1000 {
			count = cnt
		}
	}

	maxChats := 15
	if maxChatsStr := c.Query("max_chats"); maxChatsStr != "" {
		if mc, err := strconv.Atoi(maxChatsStr); err == nil && mc > 0 && mc <= 50 {
			maxChats = mc
		}
	}

	gw := h.gateway
	ds := h.dataService
	go func() {
		log := logger.WithAccount(uint(accountID))
		log.Infow("開始同步所有聊天的歷史訊息")

		chats, err := ds.GetChats(uint(accountID))
		if err != nil {
			log.Errorw("取得聊天列表失敗", "error", err)
			return
		}

		log.Infow("找到聊天，開始同步歷史訊息", "chat_count", len(chats))

		syncCount := len(chats)
		if syncCount > maxChats {
			syncCount = maxChats
		}

		for i := 0; i < syncCount; i++ {
			chat := chats[i]
			ctx := context.Background()
			if syncErr := gw.SyncHistory(ctx, uint(accountID), chat.JID, count); syncErr != nil {
				log.Errorw("同步聊天歷史訊息失敗", "chat_name", chat.Name, "error", syncErr)
			}

			time.Sleep(2 * time.Second)
		}

		log.Infow("所有聊天歷史訊息同步完成")
	}()

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("帳號 %d 的所有聊天歷史訊息同步已開始，將同步最多 %d 個聊天，每個聊天 %d 條訊息", accountID, maxChats, count)})
}

// SyncAllAccountData 完整同步帳號所有數據
// TODO: 需要實現對應的 Gateway Command（組合 SyncChats + SyncHistory + SyncContacts）
func (h *SyncHandler) SyncAllAccountData(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// TODO: 透過 Gateway 發送完整同步命令
	logger.Ctx(c.Request.Context()).Warnw("SyncAllAccountData 尚未完全遷移到 Connector 架構", "account_id", id)

	// 目前先用 SyncChats 替代
	ctx := context.Background()
	if err := h.gateway.SyncChats(ctx, id); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("觸發同步失敗", "account_id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("帳號 %d 的聊天列表同步已開始", id)})
}
