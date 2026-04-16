package app

import (
	"context"

	"whatsapp_golang/internal/analyzer"
	"whatsapp_golang/internal/database"
	"whatsapp_golang/internal/gateway"
	"whatsapp_golang/internal/handler/realtime"
	"whatsapp_golang/internal/llm"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/middleware"
	"whatsapp_golang/internal/model"
	agentSvc "whatsapp_golang/internal/service/agent"
	authSvc "whatsapp_golang/internal/service/auth"
	channelSvc "whatsapp_golang/internal/service/channel"
	connectorSvc "whatsapp_golang/internal/service/connector"
	contactSvc "whatsapp_golang/internal/service/contact"
	contentSvc "whatsapp_golang/internal/service/content"
	messagingSvc "whatsapp_golang/internal/service/messaging"
	systemSvc "whatsapp_golang/internal/service/system"
	tagSvc "whatsapp_golang/internal/service/tag"
	"whatsapp_golang/internal/service/whatsapp"
	workgroupSvc "whatsapp_golang/internal/service/workgroup"

	appConfig "whatsapp_golang/internal/config"

	"github.com/go-redis/redis/v8"
)

// referralSessionServiceAdapter 适配器：将 whatsapp.ReferralSessionService 适配为 gateway.ReferralSessionService
type referralSessionServiceAdapter struct {
	service whatsapp.ReferralSessionService
}

// GetReferralSession 实现 gateway.ReferralSessionService 接口
func (a *referralSessionServiceAdapter) GetReferralSession(ctx context.Context, sessionID string) (*whatsapp.ReferralSessionInfo, error) {
	return a.service.GetReferralSession(ctx, sessionID)
}

// DeleteReferralSession 实现 gateway.ReferralSessionService 接口
func (a *referralSessionServiceAdapter) DeleteReferralSession(ctx context.Context, sessionID string) error {
	return a.service.DeleteReferralSession(ctx, sessionID)
}

// Services 包含所有服務實例
type Services struct {
	WhatsAppData         whatsapp.DataService // 純 DB 查詢服務
	Auth                 authSvc.AuthService
	Translation          contentSvc.TranslationService
	Tag                  tagSvc.TagService
	RBAC                 authSvc.RBACService
	MessageSearch        messagingSvc.MessageSearchService
	MessageAction        messagingSvc.MessageActionService
	SensitiveWord        contentSvc.SensitiveWordService
	Config               systemSvc.ConfigService
	Telegram             contentSvc.TelegramService
	BatchSend            messagingSvc.BatchSendService
	UserData             contactSvc.UserDataService
	Channel              channelSvc.ChannelService
	PromotionDomain      channelSvc.PromotionDomainService
	Account              whatsapp.AccountService
	Gateway              *gateway.Gateway // Connector Gateway
	OperationLog         systemSvc.OperationLogService
	MessageSending       messagingSvc.MessageSendingService
	Umami                systemSvc.UmamiService
	AutoReply            contentSvc.AutoReplyService
	CustomerConversation contentSvc.CustomerConversationService
	ChatTag              contentSvc.ChatTagService
	AnalysisWorkerPool   *analyzer.WorkerPool // 分析 Worker Pool
	ProxyConfig          connectorSvc.ProxyConfigService
	ConnectorConfig      connectorSvc.ConnectorConfigService
	AiTagDefinition      contentSvc.AiTagDefinitionService
	ChatAIAnalysis       contentSvc.ChatAIAnalysisService
	LLMClient            *llm.Client
	Workgroup            workgroupSvc.WorkgroupService
	AgentAuth            agentSvc.AgentAuthService
	AgentManagement      agentSvc.AgentManagementService
	AgentOperations      agentSvc.AgentOperationsService
	ReferralService      *whatsapp.ReferralService
	ReferralSession      whatsapp.ReferralSessionService
}

// initServices 初始化所有服務
func initServices(cfg *appConfig.Config, db database.Database, redisClient *redis.Client) (*Services, error) {
	// 初始化 WhatsApp DataService（純 DB 查詢）
	whatsappDataService := whatsapp.NewDataService(db)
	logger.Info("WhatsApp DataService 初始化完成")

	// 初始化認證服務
	authService, err := authSvc.NewAuthService(db.GetDB(), redisClient, cfg)
	if err != nil {
		return nil, err
	}
	logger.Info("認證服務初始化完成")

	// 初始化配置服務（提前初始化，其他服務依賴它）
	configService := systemSvc.NewConfigService(db.GetDB())
	logger.Info("配置服務初始化完成")

	// 初始化帳號服務（Gateway / RBAC 稍後注入）
	accountService := contactSvc.NewAccountService(db.GetDB())

	// 初始化翻譯服務
	translationService := contentSvc.NewTranslationService(db.GetDB(), 30, configService)
	logger.Info("翻譯服務初始化完成")

	// 初始化標籤服務
	tagService := tagSvc.NewTagService(db.GetDB())
	logger.Info("標籤服務初始化完成")

	// 初始化 RBAC 服務
	rbacService := authSvc.NewRBACService(db.GetDB())
	logger.Info("RBAC服務初始化完成")

	// 初始化消息搜索服務
	messageSearchService := messagingSvc.NewMessageSearchService(db)
	logger.Info("消息搜索服務初始化完成")

	// 初始化 Connector Gateway（必要元件，用於分散式 WhatsApp 連接）
	// 需要在 MessageActionService 之前初始化，因為它依賴 Gateway
	connectorGateway := gateway.New(&gateway.Config{
		RedisClient: redisClient,
		DB:          db.GetDB(),
	})
	logger.Info("Connector Gateway 初始化完成")

	// 注入 Gateway 到 AccountService
	if accountServiceImpl, ok := accountService.(*contactSvc.AccountServiceImpl); ok {
		accountServiceImpl.SetGateway(connectorGateway)
	}

	// 初始化消息操作服務（使用 Gateway）
	// Gateway 可能為 nil（當 Connector 未啟用時）
	messageActionService := messagingSvc.NewMessageActionService(db, connectorGateway)
	logger.Info("消息操作服務初始化完成")

	// 初始化敏感詞服務
	sensitiveWordService := contentSvc.NewSensitiveWordService(db.GetDB())
	logger.Info("敏感詞服務初始化完成")

	// 初始化 KeywordAnalyzer
	keywordAnalyzer := analyzer.NewKeywordAnalyzer()
	logger.Info("KeywordAnalyzer 初始化完成")

	// 初始化 AnalysisQueue
	analysisQueue := analyzer.NewAnalysisQueue(redisClient)
	logger.Info("AnalysisQueue 初始化完成")

	// 初始化 Telegram 服務
	telegramService := contentSvc.NewTelegramService(configService, 10)
	logger.Info("Telegram服務初始化完成")

	// 初始化 WebSocket 處理器 (必須在 BatchSendService 之前)
	realtime.InitWebSocketHandler()
	logger.Info("WebSocket處理器初始化完成")

	// 初始化 SSE 處理器
	realtime.InitSSEHandler()
	logger.Info("SSE處理器初始化完成")

	// 初始化批量發送服務（使用 DataService + Gateway）
	batchSendService := messagingSvc.NewBatchSendService(db.GetDB(), whatsappDataService, connectorGateway, realtime.WSHandler)
	logger.Info("批量發送服務初始化完成")

	// 初始化用戶數據服務
	userDataService := contactSvc.NewUserDataService(db.GetDB())
	logger.Info("用戶數據服務初始化完成")

	// 初始化渠道服務
	channelService := channelSvc.NewChannelService(db.GetDB(), configService)
	logger.Info("渠道服務初始化完成")

	// 初始化推廣域名服務
	promotionDomainService := channelSvc.NewPromotionDomainService(db.GetDB())
	logger.Info("推廣域名服務初始化完成")

	// 初始化操作日誌服務
	operationLogService := systemSvc.NewOperationLogService(db.GetDB())
	logger.Info("操作日誌服務初始化完成")

	// 初始化 Umami 服務（從 system_configs 讀取 umami.base_url / umami.api_token）
	umamiService := systemSvc.NewUmamiService(configService)
	logger.Info("Umami 服務初始化完成")

	// 初始化統一訊息發送服務
	messageSendingService := messagingSvc.NewMessageSendingService(db.GetDB())
	logger.Info("統一訊息發送服務初始化完成")

	// 初始化自動回復服務
	autoReplyService := contentSvc.NewAutoReplyService(db.GetDB())
	logger.Info("自動回復服務初始化完成")

	// 初始化客户咨询对话服务
	customerConversationService := contentSvc.NewCustomerConversationService(db.GetDB())
	logger.Info("客户咨询对话服务初始化完成")

	// 初始化聊天室標籤服務
	chatTagService := contentSvc.NewChatTagService(db.GetDB())
	logger.Info("聊天室標籤服務初始化完成")

	// 初始化 AI 標籤定義服務
	aiTagDefService := contentSvc.NewAiTagDefinitionService(db.GetDB())
	logger.Info("AI 標籤定義服務初始化完成")

	// 初始化 LLM Client（API Key 從 system_configs 動態讀取，lazy 不會立即呼叫）
	llmClient := llm.NewClient(func() string {
		if v, err := configService.GetConfig("llm.api_key"); err == nil {
			return v
		}
		return ""
	}, 30)
	logger.Info("LLM Client 初始化完成")

	// 初始化 AI 聊天分析服務
	chatAIAnalysisService := contentSvc.NewChatAIAnalysisService(&contentSvc.ChatAIAnalysisConfig{
		DB:        db.GetDB(),
		LLMClient: llmClient,
		ConfigSvc: configService,
		TagDefSvc: aiTagDefService,
	})
	logger.Info("AI 聊天分析服務初始化完成")

	// 初始化代理配置服務
	proxyConfigService := connectorSvc.NewProxyConfigService(db.GetDB())
	logger.Info("代理配置服務初始化完成")

	// 初始化 Connector 配置服務（透過 Gateway 發送管理命令）
	connectorConfigService := connectorSvc.NewConnectorConfigService(db.GetDB(), connectorGateway.Connector, redisClient)
	logger.Info("Connector 配置服務初始化完成")

	// 初始化 WorkerPool（AI 分析器稍後添加）
	workerPool := analyzer.NewWorkerPool(&analyzer.WorkerPoolConfig{
		WorkerCount: 5,
		Queue:       analysisQueue,
		Analyzers:   []analyzer.Analyzer{}, // 預留 AI 分析器
		OnComplete: func(task *analyzer.AnalysisTask, results []analyzer.Result) {
			// 處理異步分析完成（未來可更新告警記錄 / ChatTag）
			if len(results) > 0 {
				logger.Debugw("異步分析完成", "task_id", task.ID, "result_count", len(results))
			}
		},
	})
	logger.Info("AnalysisWorkerPool 初始化完成")

	// 將 SensitiveWord 轉換為 analyzer.Word 的輔助函數
	convertToAnalyzerWords := func(words []model.SensitiveWord) []analyzer.Word {
		analyzerWords := make([]analyzer.Word, len(words))
		for i, w := range words {
			analyzerWords[i] = analyzer.Word{
				ID:          w.ID,
				Word:        w.Word,
				MatchType:   w.MatchType,
				Category:    w.Category,
				Enabled:     w.Enabled,
				Priority:    w.Priority,
				ReplaceText: w.ReplaceText,
			}
		}
		return analyzerWords
	}

	// 註冊敏感詞快取刷新回調，同步更新 KeywordAnalyzer
	sensitiveWordService.OnCacheRefresh(func(words []model.SensitiveWord) {
		keywordAnalyzer.RefreshCache(convertToAnalyzerWords(words))
	})

	// 手動同步初始快取（因為 NewSensitiveWordService 內部的 RefreshCache 在 callback 註冊前執行）
	if cachedWords := sensitiveWordService.GetCache(); len(cachedWords) > 0 {
		keywordAnalyzer.RefreshCache(convertToAnalyzerWords(cachedWords))
		logger.Infow("敏感詞快取已同步到 KeywordAnalyzer", "count", len(cachedWords))
	}
	logger.Info("敏感詞快取回調已註冊")

	// 初始化敏感詞攔截器（設置到 Gateway）
	sensitiveWordInterceptor := middleware.NewSensitiveWordInterceptor(
		keywordAnalyzer,
		analysisQueue,
		telegramService,
		configService,
		db.GetDB(),
	)
	sensitiveWordInterceptor.SetGateway(connectorGateway) // 注入 Gateway（用於自動替換功能）
	connectorGateway.SetMessageInterceptor(sensitiveWordInterceptor)
	logger.Info("敏感詞攔截器已設置到 Gateway（含自動替換功能）")

	// 初始化 JID 映射服務並注入到 Gateway
	jidMappingService := whatsapp.NewJIDMappingService(db.GetDB())
	connectorGateway.SetJIDMappingService(jidMappingService)
	logger.Info("JID 映射服務初始化完成")

	// 注入 Gateway 到 MessageSendingService
	messageSendingService.SetGateway(connectorGateway)

	// 注入 RBAC 服務 + JID 映射服務到 AccountService
	if accountServiceImpl, ok := accountService.(*contactSvc.AccountServiceImpl); ok {
		accountServiceImpl.SetRBACService(rbacService)
		accountServiceImpl.SetJIDMappingService(jidMappingService)
	}
	logger.Info("RBACService + JIDMappingService 已注入 AccountService")

	// 注入 MessageSendingService 到 BatchSendService
	batchSendService.SetMessageSendingService(messageSendingService)
	logger.Info("MessageSendingService 已注入 BatchSendService")

	// 初始化工作組服務
	workgroupService := workgroupSvc.NewWorkgroupService(db.GetDB())
	logger.Info("工作組服務初始化完成")

	// 初始化 Agent 服務
	agentAuthService := agentSvc.NewAgentAuthService(db.GetDB(), redisClient, cfg)
	agentManagementService := agentSvc.NewAgentManagementService(db.GetDB())
	agentOperationsService := agentSvc.NewAgentOperationsService(db.GetDB(), messageSendingService, accountService, jidMappingService, cfg.WhatsApp.MediaDir)
	logger.Info("Agent 服務初始化完成")

	// 初始化推荐码服务
	referralService := whatsapp.NewReferralService(db.GetDB())
	referralSessionService := whatsapp.NewReferralSessionService(redisClient)
	logger.Info("推荐码服务初始化完成")

	// 创建推荐码会话服务适配器（将 whatsapp.ReferralSessionService 适配为 gateway.ReferralSessionService）
	referralSessionAdapter := &referralSessionServiceAdapter{service: referralSessionService}

	// 注入推荐码会话服务到 Gateway
	connectorGateway.SetReferralSessionService(referralSessionAdapter)
	logger.Info("推荐码会话服务已注入 Gateway")

	// 注入工作組自動分配服務到 Gateway
	connectorGateway.SetWorkgroupAutoAssigner(workgroupService)
	logger.Info("工作組自動分配服務已注入 Gateway")

	return &Services{
		WhatsAppData:         whatsappDataService,
		Auth:                 authService,
		Translation:          translationService,
		Tag:                  tagService,
		RBAC:                 rbacService,
		MessageSearch:        messageSearchService,
		MessageAction:        messageActionService,
		SensitiveWord:        sensitiveWordService,
		Config:               configService,
		Telegram:             telegramService,
		BatchSend:            batchSendService,
		UserData:             userDataService,
		Channel:              channelService,
		PromotionDomain:      promotionDomainService,
		Account:              accountService,
		Gateway:              connectorGateway,
		OperationLog:         operationLogService,
		MessageSending:       messageSendingService,
		Umami:                umamiService,
		AutoReply:            autoReplyService,
		CustomerConversation: customerConversationService,
		ChatTag:              chatTagService,
		AnalysisWorkerPool:   workerPool,
		ProxyConfig:          proxyConfigService,
		ConnectorConfig:      connectorConfigService,

		AiTagDefinition:      aiTagDefService,
		ChatAIAnalysis:       chatAIAnalysisService,
		LLMClient:            llmClient,
		Workgroup:            workgroupService,
		AgentAuth:            agentAuthService,
		AgentManagement:      agentManagementService,
		AgentOperations:      agentOperationsService,
		ReferralService:      referralService,
		ReferralSession:      referralSessionService,
	}, nil
}
