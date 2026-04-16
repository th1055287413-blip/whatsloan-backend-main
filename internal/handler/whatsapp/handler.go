package whatsapp

import (
	"whatsapp_golang/internal/gateway"
	contentSvc "whatsapp_golang/internal/service/content"
	systemSvc "whatsapp_golang/internal/service/system"
	"whatsapp_golang/internal/service/whatsapp"
)

// WhatsAppHandler 組合的 WhatsApp 處理器
// 整合帳號、會話、同步、聊天、同步狀態五個子處理器
// 使用 Gateway 處理 WhatsApp 操作，使用 DataService 處理 DB 查詢
type WhatsAppHandler struct {
	*AccountHandler
	*SessionHandler
	*SyncHandler
	*ChatHandler
	*SyncStatusHandler
	dataService whatsapp.DataService
	gateway     *gateway.Gateway
}

// NewWhatsAppHandler 創建組合的 WhatsApp 處理器
// 使用 DataService 進行 DB 查詢，Gateway 進行 WhatsApp 操作
func NewWhatsAppHandler(dataService whatsapp.DataService, gw *gateway.Gateway, opLogService systemSvc.OperationLogService, chatTagService contentSvc.ChatTagService) *WhatsAppHandler {
	return &WhatsAppHandler{
		AccountHandler:    NewAccountHandler(dataService, gw),
		SessionHandler:    NewSessionHandler(dataService, gw),
		SyncHandler:       NewSyncHandler(dataService, gw),
		ChatHandler:       NewChatHandler(dataService, gw, opLogService, chatTagService),
		SyncStatusHandler: NewSyncStatusHandler(dataService),
		dataService:       dataService,
		gateway:           gw,
	}
}
