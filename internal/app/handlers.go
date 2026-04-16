package app

import (
	appConfig "whatsapp_golang/internal/config"
	"whatsapp_golang/internal/database"
	handlerAgent "whatsapp_golang/internal/handler/agent"
	"whatsapp_golang/internal/handler/auth"

	"github.com/go-redis/redis/v8"
	handlerChannel "whatsapp_golang/internal/handler/channel"
	"whatsapp_golang/internal/handler/contact"
	"whatsapp_golang/internal/handler/content"
	handlerContract "whatsapp_golang/internal/handler/contract"
	"whatsapp_golang/internal/handler/messaging"
	"whatsapp_golang/internal/handler/system"
	handlerTag "whatsapp_golang/internal/handler/tag"
	"whatsapp_golang/internal/handler/whatsapp"
	handlerWorkgroup "whatsapp_golang/internal/handler/workgroup"
	"whatsapp_golang/internal/reporter"
)

// Handlers 包含所有 Handler 實例
type Handlers struct {
	WhatsApp             *whatsapp.WhatsAppHandler
	MessageSearch        *messaging.SearchHandler
	MessageAction        *messaging.ActionHandler
	Auth                 *auth.AuthHandler
	AdminUser            *auth.AdminUserHandler
	UserAssignment       *auth.UserAssignmentHandler
	UserData             *contact.UserDataHandler
	Translation          *content.TranslationHandler
	Tag                  *handlerTag.TagHandler
	RBAC                 *auth.RBACHandler
	Media                *system.MediaHandler
	SensitiveWord        *content.SensitiveWordHandler
	SystemConfig         *system.ConfigHandler
	SensitiveWordAlert   *content.SensitiveWordAlertHandler
	BatchSend            *messaging.BatchSendHandler
	Channel              *handlerChannel.ChannelHandler
	PromotionDomain      *handlerChannel.PromotionDomainHandler
	OperationLog         *system.OperationLogHandler
	Reporter             *reporter.Handler
	AutoReply            *content.AutoReplyHandler
	CustomerConversation *content.CustomerConversationHandler
	ChatTag              *content.ChatTagHandler
	Connector            *system.ConnectorHandler
	Monitor              *system.MonitorHandler
	ProxyConfig          *system.ProxyConfigHandler
	ConnectorConfig      *system.ConnectorConfigHandler
	AiTagDefinition      *system.AiTagDefinitionHandler
	ModerationConfig     *content.ModerationConfigHandler
	Workgroup            *handlerWorkgroup.WorkgroupHandler
	AgentAuth            *handlerAgent.AuthHandler
	AgentManagement      *handlerAgent.ManagementHandler
	AgentLeader          *handlerAgent.LeaderHandler
	AgentOperations      *handlerAgent.OperationsHandler
	AgentActivityLog     *handlerAgent.ActivityLogHandler
	Referral             *whatsapp.ReferralHandler
	Contract             *handlerContract.Handler
}

// initHandlers 初始化所有 Handler
func initHandlers(cfg *appConfig.Config, db database.Database, redisClient *redis.Client, svc *Services) *Handlers {
	// 使用 DataService + Gateway 創建 Handler
	// Gateway 可能為 nil（當 Connector 未啟用時）
	whatsappHandler := whatsapp.NewWhatsAppHandler(svc.WhatsAppData, svc.Gateway, svc.OperationLog, svc.ChatTag)
	// 注入 AccountService 及相關依賴到內嵌的 AccountHandler
	whatsappHandler.SetAccountService(svc.Account)
	whatsappHandler.SetRBACService(svc.RBAC)
	whatsappHandler.SetOpLogService(svc.OperationLog)
	whatsappHandler.SetChatTagService(svc.ChatTag)
	whatsappHandler.SetMessageSendingService(svc.MessageSending)
	whatsappHandler.SetMediaDir(cfg.WhatsApp.MediaDir)
	// 注入推荐码服务到 SessionHandler
	whatsappHandler.SessionHandler.SetReferralServices(svc.ReferralService, svc.ReferralSession)

	// 初始化 Connector Handler（如果 Gateway 可用）
	var connectorHandler *system.ConnectorHandler
	if svc.Gateway != nil {
		connectorHandler = system.NewConnectorHandler(svc.Gateway.Routing)
	}

	// 初始化 ProxyConfig Handler
	proxyConfigHandler := system.NewProxyConfigHandler(svc.ProxyConfig, svc.OperationLog)

	// 初始化 ConnectorConfig Handler
	connectorConfigHandler := system.NewConnectorConfigHandler(svc.ConnectorConfig, svc.OperationLog)

	// 初始化 Monitor Handler
	monitorHandler := system.NewMonitorHandler(redisClient)

	// 初始化推荐码 Handler
	referralHandler := whatsapp.NewReferralHandler(svc.ReferralService, svc.PromotionDomain)

	// 初始化 Contract Handler
	contractHandler := handlerContract.NewHandler(db.GetDB())
	if svc.LLMClient != nil {
		contractHandler = handlerContract.NewHandlerWithLLM(db.GetDB(), svc.LLMClient, svc.Config)
	}

	return &Handlers{
		WhatsApp:             whatsappHandler,
		MessageSearch:        messaging.NewSearchHandler(svc.MessageSearch),
		MessageAction:        messaging.NewActionHandler(svc.MessageAction, svc.OperationLog),
		Auth:                 auth.NewAuthHandler(svc.Auth, svc.RBAC, svc.OperationLog),
		AdminUser:            auth.NewAdminUserHandler(svc.Auth, svc.RBAC, svc.OperationLog),
		UserAssignment:       auth.NewUserAssignmentHandler(svc.RBAC, svc.Auth, svc.OperationLog),
		UserData:             contact.NewUserDataHandler(svc.UserData),
		Translation:          content.NewTranslationHandler(svc.Translation),
		Tag:                  handlerTag.NewTagHandler(svc.Tag),
		RBAC:                 auth.NewRBACHandler(svc.RBAC, svc.OperationLog),
		Media:                system.NewMediaHandler(cfg.WhatsApp.MediaDir),
		SensitiveWord:        content.NewSensitiveWordHandler(svc.SensitiveWord),
		SystemConfig:         system.NewConfigHandler(svc.Config, svc.Telegram, svc.OperationLog),
		SensitiveWordAlert:   content.NewSensitiveWordAlertHandler(db.GetDB(), svc.Telegram),
		BatchSend:            messaging.NewBatchSendHandler(svc.BatchSend, svc.OperationLog),
		Channel:              handlerChannel.NewChannelHandler(svc.Channel, svc.OperationLog),
		PromotionDomain:      handlerChannel.NewPromotionDomainHandler(svc.PromotionDomain),
		OperationLog:         system.NewOperationLogHandler(svc.OperationLog),
		Reporter:             reporter.NewHandler(db.GetDB(), svc.Umami, svc.Config),
		AutoReply:            content.NewAutoReplyHandler(svc.AutoReply),
		CustomerConversation: content.NewCustomerConversationHandler(svc.CustomerConversation),
		ChatTag:              content.NewChatTagHandler(svc.ChatTag),
		Connector:            connectorHandler,
		Monitor:              monitorHandler,
		ProxyConfig:          proxyConfigHandler,
		ConnectorConfig:      connectorConfigHandler,
		AiTagDefinition:      system.NewAiTagDefinitionHandler(svc.AiTagDefinition),
		ModerationConfig:     content.NewModerationConfigHandler(svc.Config, svc.Telegram, svc.OperationLog),
		Workgroup:            handlerWorkgroup.NewWorkgroupHandler(svc.Workgroup, svc.OperationLog),
		AgentAuth:            handlerAgent.NewAuthHandler(svc.AgentAuth, svc.AgentManagement),
		AgentManagement:      handlerAgent.NewManagementHandler(svc.AgentManagement, svc.OperationLog),
		AgentLeader:          handlerAgent.NewLeaderHandler(svc.AgentManagement),
		AgentOperations:      handlerAgent.NewOperationsHandler(svc.AgentOperations, svc.ChatTag, svc.MessageAction, svc.OperationLog),
		AgentActivityLog:     handlerAgent.NewActivityLogHandler(svc.OperationLog),
		Referral:             referralHandler,
		Contract:             contractHandler,
	}
}
