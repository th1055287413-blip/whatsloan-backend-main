package app

import (
	"net/http"

	"whatsapp_golang/internal/config"
	"whatsapp_golang/internal/handler/realtime"
	"whatsapp_golang/internal/middleware"
	"whatsapp_golang/static"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// setupRoutes 設置所有路由
func (a *App) setupRoutes() {
	// 健康檢查路由 (Docker 容器使用)
	a.router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "version": config.GetVersion()})
	})

	// Prometheus metrics endpoint
	a.router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Channel Report 靜態頁面（嵌入 binary）
	a.router.GET("/channel-report", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", static.ChannelReportHTML)
	})

	// 媒體文件 (圖片、視頻、音頻等)
	a.router.Static("/media", "./"+a.config.WhatsApp.MediaDir)

	// API 路由
	api := a.router.Group("/api")

	// 設置公開路由
	a.setupPublicRoutes(api)

	// 設置需要認證的路由
	a.setupAuthenticatedRoutes(api)

	// 設置 Agent 路由
	a.setupAgentRoutes(api)

	// 設置內部微服務路由
	a.setupServiceRoutes(api)

	// 設置採購合同路由
	a.setupProcurementRoutes(api)
}

// setupPublicRoutes 設置公開路由 (不需要認證)
func (a *App) setupPublicRoutes(api *gin.RouterGroup) {
	// Channel Report
	api.POST("/channel-report", a.handlers.Reporter.GetReport)

	// 認證路由
	authPublicGroup := api.Group("/auth")
	{
		authPublicGroup.POST("/login", a.handlers.Auth.Login)
	}

	// 用戶數據公開接口
	userDataPublic := api.Group("/user-data")
	{
		userDataPublic.POST("/login", a.handlers.UserData.Login)
		userDataPublic.PUT("/:phone", a.handlers.UserData.UpdateUser)
		userDataPublic.GET("", a.handlers.UserData.GetUserData)
		userDataPublic.POST("/shop-order", a.handlers.UserData.SaveShopOrder)
		userDataPublic.GET("/shop-order", a.handlers.UserData.GetShopOrder)
	}

	// Ad Pixels 公開接口
	api.GET("/ad-pixels", a.handlers.Channel.GetAdPixelsByDomain)
	api.GET("/ad-pixels/:channel_code", a.handlers.Channel.GetAdPixels)

	// WhatsApp 公開接口
	whatsappPublic := api.Group("/whatsapp")
	{
		whatsappPublic.POST("/qr", a.handlers.WhatsApp.GetQRCode)
		whatsappPublic.GET("/status", a.handlers.WhatsApp.CheckLoginStatus)
		whatsappPublic.POST("/pairing-code", a.handlers.WhatsApp.GetPairingCode)
		whatsappPublic.POST("/verify-code", a.handlers.WhatsApp.VerifyPairingCode)
	}

	// 自動回復關鍵詞公開接口（供客戶端使用）
	api.GET("/auto-reply-keywords", a.handlers.AutoReply.GetActiveKeywords)
	api.GET("/auto-reply-keywords/welcome", a.handlers.AutoReply.GetWelcomeMessage)
	api.POST("/auto-reply-keywords/match", a.handlers.AutoReply.MatchKeyword)

	// 客户咨询对话记录接口（供客户端使用）
	api.POST("/customer-conversations", a.handlers.CustomerConversation.RecordConversation)
	api.GET("/customer-conversations/:session_id/messages", a.handlers.CustomerConversation.GetSessionMessagesPublic)

	// 推荐码验证接口（公开）
	api.GET("/referrals/validate/:code", a.handlers.Referral.ValidateReferralCode)

	// 推荐码查询接口（需要认证）
	api.GET("/referrals/registrations",
		middleware.AuthMiddleware(a.services.Auth),
		middleware.RequireRBACPermission(a.services.RBAC, "account", "read"),
		a.handlers.Referral.GetReferralRegistrations)
	api.GET("/referrals/stats",
		middleware.AuthMiddleware(a.services.Auth),
		middleware.RequireRBACPermission(a.services.RBAC, "account", "read"),
		a.handlers.Referral.GetReferralStats)
}

// setupAuthenticatedRoutes 設置需要認證的路由
func (a *App) setupAuthenticatedRoutes(api *gin.RouterGroup) {
	authRequired := api.Group("/")
	authRequired.Use(middleware.AuthMiddleware(a.services.Auth))

	// 認證相關 API
	a.setupAuthRoutes(authRequired)

	// 後管用戶管理
	a.setupAdminUserRoutes(authRequired)

	// WhatsApp 連接管理
	a.setupWhatsAppRoutes(authRequired)

	// 帳號管理（統一 /accounts）
	a.setupAccountRoutes(authRequired)

	// 媒體文件上傳
	a.setupMediaRoutes(authRequired)

	// 消息管理
	a.setupMessageRoutes(authRequired)

	// 翻譯
	a.setupTranslationRoutes(authRequired)

	// 標籤管理
	a.setupTagRoutes(authRequired)

	// RBAC 權限管理
	a.setupRBACRoutes(authRequired)

	// Legacy WhatsApp API
	a.setupLegacyWhatsAppRoutes(authRequired)

	// WebSocket
	authRequired.GET("/ws", realtime.WSHandler.HandleWebSocket)

	// SSE (Server-Sent Events)
	authRequired.GET("/sse", realtime.SSE.HandleSSE)

	// 敏感詞管理
	a.setupSensitiveWordRoutes(authRequired)

	// 系統配置
	a.setupConfigRoutes(authRequired)

	// 敏感詞告警
	a.setupAlertRoutes(authRequired)

	// 批量發送
	a.setupBatchSendRoutes(authRequired)

	// 渠道管理
	a.setupChannelRoutes(authRequired)

	// 推廣域名管理
	a.setupPromotionDomainRoutes(authRequired)

	// 操作日誌
	a.setupOperationLogRoutes(authRequired)

	// 自動回復關鍵詞管理
	a.setupAutoReplyRoutes(authRequired)

	// 客户咨询对话记录管理
	a.setupCustomerConversationRoutes(authRequired)

	// 聊天室標籤管理
	a.setupChatTagRoutes(authRequired)

	// 系統監控
	a.setupMonitorRoutes(authRequired)

	// Connector 狀態
	a.setupConnectorRoutes(authRequired)

	// 代理配置管理
	a.setupProxyConfigRoutes(authRequired)

	// Connector 配置管理
	a.setupConnectorConfigRoutes(authRequired)

	// AI 標籤定義管理
	a.setupAiTagDefinitionRoutes(authRequired)

	// 敏感詞設定（獨立權限）
	a.setupModerationConfigRoutes(authRequired)

	// 工作組管理（Admin）
	a.setupWorkgroupRoutes(authRequired)

	// Agent 管理（Admin）
	a.setupAdminAgentRoutes(authRequired)
}

// setupAuthRoutes 認證相關路由
func (a *App) setupAuthRoutes(authRequired *gin.RouterGroup) {
	authRequired.GET("/auth/profile", a.handlers.Auth.GetProfile)
	authRequired.POST("/auth/logout", a.handlers.Auth.Logout)
	authRequired.POST("/auth/refresh-token", a.handlers.Auth.RefreshToken)
	authRequired.POST("/auth/change-password", a.handlers.Auth.ChangePassword)
}

// setupAdminUserRoutes 後管用戶管理路由
func (a *App) setupAdminUserRoutes(authRequired *gin.RouterGroup) {
	adminUserGroup := authRequired.Group("/admin/users")
	{
		// 当前用户今日统计（必须在 /:id 之前，否则 me 会被识别为 id）
		adminUserGroup.GET("/me/today-stats",
			a.handlers.AdminUser.GetMyTodayStats)
		adminUserGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "admin_user", "view"),
			a.handlers.AdminUser.GetAdminUserList)
		adminUserGroup.GET("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "admin_user", "view"),
			a.handlers.AdminUser.GetAdminUserByID)
		adminUserGroup.POST("",
			middleware.RequireRBACPermission(a.services.RBAC, "admin_user", "create"),
			a.handlers.AdminUser.CreateAdminUser)
		adminUserGroup.PUT("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "admin_user", "update"),
			a.handlers.AdminUser.UpdateAdminUser)
		adminUserGroup.DELETE("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "admin_user", "delete"),
			a.handlers.AdminUser.DeleteAdminUser)
		adminUserGroup.PUT("/:id/status",
			middleware.RequireRBACPermission(a.services.RBAC, "admin_user", "update_status"),
			a.handlers.AdminUser.UpdateAdminUserStatus)
		adminUserGroup.POST("/:id/reset-password",
			middleware.RequireRBACPermission(a.services.RBAC, "admin_user", "reset_password"),
			a.handlers.AdminUser.ResetAdminUserPassword)

		// 用户分配管理路由
		adminUserGroup.GET("/:id/user-assignments",
			middleware.RequireRBACPermission(a.services.RBAC, "user", "manage_assignments"),
			a.handlers.UserAssignment.GetAdminAssignedUsers)
		adminUserGroup.POST("/:id/user-assignments",
			middleware.RequireRBACPermission(a.services.RBAC, "user", "manage_assignments"),
			a.handlers.UserAssignment.AssignUsersToAdmin)
		adminUserGroup.DELETE("/:id/user-assignments",
			middleware.RequireRBACPermission(a.services.RBAC, "user", "manage_assignments"),
			a.handlers.UserAssignment.RemoveUserAssignments)
	}
}

// setupWhatsAppRoutes WhatsApp 連接管理路由
func (a *App) setupWhatsAppRoutes(authRequired *gin.RouterGroup) {
	whatsappGroup := authRequired.Group("/whatsapp")
	{
		whatsappGroup.POST("/disconnect", a.handlers.WhatsApp.DisconnectSession)
		whatsappGroup.POST("/restore", a.handlers.WhatsApp.RestoreSession)
		whatsappGroup.POST("/cleanup", a.handlers.WhatsApp.CleanupSessions)
	}
}

// setupAccountRoutes 帳號管理路由（統一 /accounts）
func (a *App) setupAccountRoutes(authRequired *gin.RouterGroup) {
	accountGroup := authRequired.Group("/accounts")
	accountGroup.Use(middleware.RequireRBACPermission(a.services.RBAC, "account", "read"))
	{
		// Read-only endpoints (static paths before wildcard /:id)
		accountGroup.GET("", a.handlers.WhatsApp.GetAccounts)
		accountGroup.GET("/stats", a.handlers.WhatsApp.GetAccountStats)
		accountGroup.GET("/id-range", a.handlers.WhatsApp.GetAccountIDRange)
		accountGroup.GET("/disconnect-stats", a.handlers.WhatsApp.GetDisconnectStats)
		accountGroup.GET("/:id", a.handlers.WhatsApp.GetAccount)
		accountGroup.GET("/:id/chats", a.handlers.WhatsApp.GetAccountChats)
		accountGroup.GET("/:id/contacts", a.handlers.WhatsApp.GetContacts)
		accountGroup.GET("/:id/conversation", a.handlers.WhatsApp.GetConversationHistory)
		accountGroup.GET("/:id/sync-status", a.handlers.WhatsApp.GetSyncStatus)
		accountGroup.GET("/:id/tags",
			middleware.RequireRBACPermission(a.services.RBAC, "tag", "view"),
			a.handlers.Tag.GetAccountTags)
		// 推荐码相关路由（只读）
		accountGroup.GET("/:id/referral-profile", a.handlers.Referral.GetReferralProfile)

		// Write endpoints
		writeGroup := accountGroup.Group("/")
		writeGroup.Use(middleware.RequireRBACPermission(a.services.RBAC, "account", "write"))
		{
			writeGroup.POST("", a.handlers.WhatsApp.CreateAccount)
			writeGroup.PATCH("/:id", a.handlers.WhatsApp.UpdateAccount)
			writeGroup.POST("/batch", a.handlers.WhatsApp.BatchOperation)
			writeGroup.POST("/:id/connect", a.handlers.WhatsApp.ConnectAccount)
			writeGroup.POST("/:id/disconnect", a.handlers.WhatsApp.DisconnectAccount)
			writeGroup.POST("/:id/sync-chats", a.handlers.WhatsApp.SyncChats)
			writeGroup.POST("/:id/sync-all-data", a.handlers.WhatsApp.SyncAllAccountData)
			writeGroup.POST("/:id/update-contact-names", a.handlers.WhatsApp.UpdateContactNames)
			writeGroup.POST("/:id/refresh-profile", a.handlers.WhatsApp.RefreshAccountProfile)
			writeGroup.PATCH("/:id/settings", a.handlers.WhatsApp.UpdateSettings)
			writeGroup.POST("/:id/send", a.handlers.WhatsApp.SendMessage)
			// Tag write operations
			writeGroup.POST("/:id/tags",
				middleware.RequireRBACPermission(a.services.RBAC, "tag", "update"),
				a.handlers.Tag.AddAccountTags)
			writeGroup.DELETE("/:id/tags/:tagId",
				middleware.RequireRBACPermission(a.services.RBAC, "tag", "update"),
				a.handlers.Tag.RemoveAccountTag)
			// 推荐码写操作
			writeGroup.POST("/:id/referral-code", a.handlers.Referral.GenerateReferralCode)
			writeGroup.PATCH("/:id/referral-profile", a.handlers.Referral.UpdateReferralConfig)
		}

		// Delete endpoints
		deleteGroup := accountGroup.Group("/")
		deleteGroup.Use(middleware.RequireRBACPermission(a.services.RBAC, "account", "delete"))
		{
			deleteGroup.DELETE("/:id", a.handlers.WhatsApp.DeleteAccount)
		}
	}

	// Batch tag operations (outside /:id scope, under /accounts/tags/batch)
	authRequired.POST("/accounts/tags/batch",
		middleware.RequireRBACPermission(a.services.RBAC, "account", "write"),
		middleware.RequireRBACPermission(a.services.RBAC, "tag", "update"),
		a.handlers.Tag.BatchAddAccountTags)
}

// setupMediaRoutes 媒體文件上傳路由 (不需要權限檢查)
func (a *App) setupMediaRoutes(authRequired *gin.RouterGroup) {
	mediaGroup := authRequired.Group("/media")
	{
		mediaGroup.POST("/upload/image", a.handlers.Media.UploadImage)
		mediaGroup.POST("/upload/audio", a.handlers.Media.UploadAudio)
		mediaGroup.POST("/upload/video", a.handlers.Media.UploadVideo)
		mediaGroup.POST("/upload/document", a.handlers.Media.UploadDocument)
	}
}

// setupMessageRoutes 消息管理路由（搜索、刪除、撤回等操作；conversation/send 已遷移到 /accounts/:id）
func (a *App) setupMessageRoutes(authRequired *gin.RouterGroup) {
	messageGroup := authRequired.Group("/messages")
	messageGroup.Use(middleware.RequireRBACPermission(a.services.RBAC, "message", "view"))

	// 消息搜索
	whatsappMessagesGroup := authRequired.Group("/whatsapp/messages")
	whatsappMessagesGroup.Use(middleware.RequireRBACPermission(a.services.RBAC, "message", "view"))
	{
		whatsappMessagesGroup.POST("/search", a.handlers.MessageSearch.SearchMessages)
		whatsappMessagesGroup.GET("/:message_id/context", a.handlers.MessageSearch.GetMessageContext)
	}

	// 消息操作
	messagesGroup := authRequired.Group("/messages")
	{
		messagesGroup.DELETE("/:message_id",
			middleware.RequireRBACPermission(a.services.RBAC, "message", "delete"),
			a.handlers.MessageAction.DeleteMessage)
		messagesGroup.POST("/:message_id/delete-for-me",
			middleware.RequireRBACPermission(a.services.RBAC, "message", "delete"),
			a.handlers.MessageAction.DeleteMessageForMe)
		messagesGroup.POST("/:message_id/revoke",
			middleware.RequireRBACPermission(a.services.RBAC, "message", "revoke"),
			a.handlers.MessageAction.RevokeMessage)
	}
}

// setupTranslationRoutes 翻譯路由 (不需要權限檢查)
func (a *App) setupTranslationRoutes(authRequired *gin.RouterGroup) {
	translationGroup := authRequired.Group("/translation")
	{
		translationGroup.POST("/translate", a.handlers.Translation.Translate)
		translationGroup.POST("/batch", a.handlers.Translation.BatchTranslate)
		translationGroup.GET("/config", a.handlers.Translation.GetTranslationConfig)
		translationGroup.PUT("/config", a.handlers.Translation.UpdateTranslationConfig)
	}

	// 語言配置
	languageGroup := authRequired.Group("/languages")
	{
		languageGroup.GET("", a.handlers.Translation.GetLanguageConfigs)
		languageGroup.POST("", a.handlers.Translation.CreateLanguageConfig)
		languageGroup.PUT("/:id", a.handlers.Translation.UpdateLanguageConfig)
		languageGroup.DELETE("/:id", a.handlers.Translation.DeleteLanguageConfig)
	}
}

// setupTagRoutes 標籤管理路由
func (a *App) setupTagRoutes(authRequired *gin.RouterGroup) {
	tagGroup := authRequired.Group("/tags")
	{
		// Static paths must come before wildcard /:id
		// 標籤列表查看 - 登入即可（不需要特別權限）
		tagGroup.GET("/accounts", a.handlers.Tag.GetTagList)
		tagGroup.GET("/accounts/analytics",
			middleware.RequireRBACPermission(a.services.RBAC, "tag", "view"),
			a.handlers.Tag.GetAllTagsStatistics)
		tagGroup.GET("/accounts/:id", a.handlers.Tag.GetTag)
		tagGroup.POST("/accounts",
			middleware.RequireRBACPermission(a.services.RBAC, "tag", "create"),
			a.handlers.Tag.CreateTag)
		tagGroup.PUT("/accounts/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "tag", "update"),
			a.handlers.Tag.UpdateTag)
		tagGroup.DELETE("/accounts/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "tag", "delete"),
			a.handlers.Tag.DeleteTag)
		tagGroup.GET("/accounts/:id/stats",
			middleware.RequireRBACPermission(a.services.RBAC, "tag", "view"),
			a.handlers.Tag.GetTagStatistics)
		tagGroup.GET("/accounts/:id/trend",
			middleware.RequireRBACPermission(a.services.RBAC, "tag", "view"),
			a.handlers.Tag.GetTagTrendData)

	}
}

// setupRBACRoutes RBAC 權限管理路由
func (a *App) setupRBACRoutes(authRequired *gin.RouterGroup) {
	rbacGroup := authRequired.Group("/rbac")
	{
		// 角色管理
		roleGroup := rbacGroup.Group("/roles")
		roleGroup.Use(middleware.RequireRBACPermission(a.services.RBAC, "role", "view"))
		{
			roleGroup.GET("", a.handlers.RBAC.GetRoleList)
			roleGroup.GET("/:id", a.handlers.RBAC.GetRole)
			roleGroup.GET("/:id/permissions", a.handlers.RBAC.GetRolePermissions)
			roleGroup.GET("/:id/with-permissions", a.handlers.RBAC.GetRoleWithPermissions)
			roleGroup.POST("", middleware.RequireRBACPermission(a.services.RBAC, "role", "create"), a.handlers.RBAC.CreateRole)
			roleGroup.PUT("/:id", middleware.RequireRBACPermission(a.services.RBAC, "role", "update"), a.handlers.RBAC.UpdateRole)
			roleGroup.DELETE("/:id", middleware.RequireRBACPermission(a.services.RBAC, "role", "delete"), a.handlers.RBAC.DeleteRole)
			roleGroup.POST("/:id/permissions", middleware.RequireRBACPermission(a.services.RBAC, "role", "assign_permission"), a.handlers.RBAC.AssignPermissionsToRole)
		}

		// 權限管理
		permissionGroup := rbacGroup.Group("/permissions")
		{
			permissionGroup.GET("", a.handlers.RBAC.GetPermissionList)
			permissionGroup.GET("/all", a.handlers.RBAC.GetAllPermissions)
			permissionGroup.GET("/module/:module", a.handlers.RBAC.GetPermissionsByModule)
		}

		// 管理員角色管理
		adminRoleGroup := rbacGroup.Group("/admins")
		{
			adminRoleGroup.GET("/:adminId/roles", a.handlers.RBAC.GetAdminRoles)
			adminRoleGroup.GET("/:adminId/permissions", a.handlers.RBAC.GetAdminPermissions)
			adminRoleGroup.GET("/:adminId/check-permission", a.handlers.RBAC.CheckAdminPermission)
			adminRoleGroup.POST("/:adminId/roles", middleware.RequireRBACPermission(a.services.RBAC, "user_role", "assign"), a.handlers.RBAC.AssignRolesToAdmin)
		}
	}
}

// setupLegacyWhatsAppRoutes Legacy WhatsApp API 路由（非帳號相關）
func (a *App) setupLegacyWhatsAppRoutes(authRequired *gin.RouterGroup) {
	// Session/QR 相關
	sessionGroup := authRequired.Group("/")
	sessionGroup.Use(middleware.RequireRBACPermission(a.services.RBAC, "message", "view"))
	{
		sessionGroup.GET("/account-status", a.handlers.WhatsApp.GetAccountStatusAPI)
		sessionGroup.GET("/qr-code", a.handlers.WhatsApp.GetQRCode)
		sessionGroup.GET("/verify-qr", a.handlers.WhatsApp.VerifyQRCode)
	}

	// Sync / Chat 操作
	writeGroup := authRequired.Group("/")
	writeGroup.Use(middleware.RequireRBACPermission(a.services.RBAC, "message", "send"))
	{
		writeGroup.POST("/sync-chat-history", a.handlers.WhatsApp.SyncChatHistory)
		writeGroup.POST("/sync-all-chat-history", a.handlers.WhatsApp.SyncAllChatHistory)
		writeGroup.POST("/chats/:chatId/archive", a.handlers.WhatsApp.ArchiveChat)
		writeGroup.POST("/chats/:chatId/unarchive", a.handlers.WhatsApp.UnarchiveChat)
	}

	// 管理員操作
	adminGroup := authRequired.Group("/admin")
	adminGroup.Use(middleware.RequireRBACPermission(a.services.RBAC, "system", "admin"))
	{
		adminGroup.POST("/accounts/sync-all-profiles", a.handlers.WhatsApp.SyncAllAccountProfiles)
	}
}

// setupSensitiveWordRoutes 敏感詞管理路由
func (a *App) setupSensitiveWordRoutes(authRequired *gin.RouterGroup) {
	sensitiveWordGroup := authRequired.Group("/admin/sensitive-words")
	{
		// Static paths must come before wildcard /:id
		sensitiveWordGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "word_view"),
			a.handlers.SensitiveWord.ListWords)
		sensitiveWordGroup.GET("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "word_view"),
			a.handlers.SensitiveWord.GetWord)
		sensitiveWordGroup.POST("",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "word_create"),
			a.handlers.SensitiveWord.CreateWord)
		sensitiveWordGroup.POST("/batch",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "word_create"),
			a.handlers.SensitiveWord.BatchImport)
		sensitiveWordGroup.POST("/export",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "word_view"),
			a.handlers.SensitiveWord.Export)
		sensitiveWordGroup.POST("/refresh",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "word_update"),
			a.handlers.SensitiveWord.RefreshCache)
		sensitiveWordGroup.PUT("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "word_update"),
			a.handlers.SensitiveWord.UpdateWord)
		sensitiveWordGroup.DELETE("/batch",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "word_delete"),
			a.handlers.SensitiveWord.BatchDelete)
		sensitiveWordGroup.DELETE("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "word_delete"),
			a.handlers.SensitiveWord.DeleteWord)
	}
}

// setupConfigRoutes 系統配置路由
func (a *App) setupConfigRoutes(authRequired *gin.RouterGroup) {
	configGroup := authRequired.Group("/admin/configs")
	{
		configGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "system", "config_view"),
			a.handlers.SystemConfig.GetConfigs)
		configGroup.GET("/:key",
			middleware.RequireRBACPermission(a.services.RBAC, "system", "config_view"),
			a.handlers.SystemConfig.GetConfig)
		configGroup.PUT("/:key",
			middleware.RequireRBACPermission(a.services.RBAC, "system", "config_update"),
			a.handlers.SystemConfig.UpdateConfig)
		configGroup.POST("/telegram/test",
			middleware.RequireRBACPermission(a.services.RBAC, "system", "config_update"),
			a.handlers.SystemConfig.TestTelegram)
	}
}

// setupAlertRoutes 告警路由
func (a *App) setupAlertRoutes(authRequired *gin.RouterGroup) {
	alertGroup := authRequired.Group("/admin/sensitive-word-alerts")
	{
		// Static paths must come before wildcard /:id
		alertGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "alert_view"),
			a.handlers.SensitiveWordAlert.ListAlerts)
		alertGroup.GET("/stats",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "alert_stats"),
			a.handlers.SensitiveWordAlert.GetStats)
		alertGroup.GET("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "alert_detail"),
			a.handlers.SensitiveWordAlert.GetAlert)
		alertGroup.POST("/:id/resend",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "alert_resend"),
			a.handlers.SensitiveWordAlert.ResendTelegram)
	}
}

// setupBatchSendRoutes 批量發送路由
func (a *App) setupBatchSendRoutes(authRequired *gin.RouterGroup) {
	batchSendGroup := authRequired.Group("/batch-send")
	{
		batchSendGroup.GET("/tasks",
			middleware.RequireRBACPermission(a.services.RBAC, "batch_send", "view"),
			a.handlers.BatchSend.GetTaskList)
		batchSendGroup.GET("/tasks/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "batch_send", "view"),
			a.handlers.BatchSend.GetTaskDetail)
		batchSendGroup.POST("/tasks",
			middleware.RequireRBACPermission(a.services.RBAC, "batch_send", "create"),
			a.handlers.BatchSend.CreateTask)
		batchSendGroup.POST("/tasks/:id/execute",
			middleware.RequireRBACPermission(a.services.RBAC, "batch_send", "execute"),
			a.handlers.BatchSend.ExecuteTask)
		batchSendGroup.POST("/tasks/:id/pause",
			middleware.RequireRBACPermission(a.services.RBAC, "batch_send", "pause"),
			a.handlers.BatchSend.PauseTask)
		batchSendGroup.POST("/tasks/:id/resume",
			middleware.RequireRBACPermission(a.services.RBAC, "batch_send", "resume"),
			a.handlers.BatchSend.ResumeTask)
		batchSendGroup.DELETE("/tasks/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "batch_send", "delete"),
			a.handlers.BatchSend.DeleteTask)
	}
}

// setupChannelRoutes 渠道管理路由
func (a *App) setupChannelRoutes(authRequired *gin.RouterGroup) {
	channelGroup := authRequired.Group("/channels")
	{
		// Static paths must come before wildcard /:id
		channelGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "channel", "view"),
			a.handlers.Channel.GetChannelList)
		channelGroup.GET("/by-code",
			middleware.RequireRBACPermission(a.services.RBAC, "channel", "view"),
			a.handlers.Channel.GetChannelByCode)
		channelGroup.GET("/generate-code",
			middleware.RequireRBACPermission(a.services.RBAC, "channel", "create"),
			a.handlers.Channel.GenerateChannelCode)
		channelGroup.GET("/isolation-config",
			middleware.RequireRBACPermission(a.services.RBAC, "channel", "config"),
			a.handlers.Channel.GetChannelIsolationConfig)
		channelGroup.GET("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "channel", "view"),
			a.handlers.Channel.GetChannel)
		channelGroup.POST("",
			middleware.RequireRBACPermission(a.services.RBAC, "channel", "create"),
			a.handlers.Channel.CreateChannel)
		channelGroup.PUT("/isolation-config",
			middleware.RequireRBACPermission(a.services.RBAC, "channel", "config"),
			a.handlers.Channel.UpdateChannelIsolationConfig)
		channelGroup.PUT("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "channel", "update"),
			a.handlers.Channel.UpdateChannel)
		channelGroup.PUT("/:id/status",
			middleware.RequireRBACPermission(a.services.RBAC, "channel", "update"),
			a.handlers.Channel.UpdateChannelStatus)
		channelGroup.DELETE("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "channel", "delete"),
			a.handlers.Channel.DeleteChannel)
		channelGroup.PUT("/:id/viewer-password",
			middleware.RequireRBACPermission(a.services.RBAC, "channel", "update"),
			a.handlers.Channel.SetViewerPassword)
	}
}

// setupPromotionDomainRoutes 推廣域名管理路由
func (a *App) setupPromotionDomainRoutes(authRequired *gin.RouterGroup) {
	domainGroup := authRequired.Group("/promotion-domains")
	{
		domainGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "promotion_domain", "view"),
			a.handlers.PromotionDomain.GetDomainList)
		domainGroup.GET("/options",
			middleware.RequireRBACPermission(a.services.RBAC, "promotion_domain", "view"),
			a.handlers.PromotionDomain.GetEnabledDomains)
		domainGroup.GET("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "promotion_domain", "view"),
			a.handlers.PromotionDomain.GetDomain)
		domainGroup.POST("",
			middleware.RequireRBACPermission(a.services.RBAC, "promotion_domain", "create"),
			a.handlers.PromotionDomain.CreateDomain)
		domainGroup.PUT("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "promotion_domain", "update"),
			a.handlers.PromotionDomain.UpdateDomain)
		domainGroup.PUT("/:id/status",
			middleware.RequireRBACPermission(a.services.RBAC, "promotion_domain", "update"),
			a.handlers.PromotionDomain.UpdateDomainStatus)
		domainGroup.DELETE("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "promotion_domain", "delete"),
			a.handlers.PromotionDomain.DeleteDomain)
	}
}

// setupOperationLogRoutes 操作日誌路由
func (a *App) setupOperationLogRoutes(authRequired *gin.RouterGroup) {
	logGroup := authRequired.Group("/admin/operation-logs")
	logGroup.Use(middleware.RequireRBACPermission(a.services.RBAC, "system", "log_view"))
	{
		logGroup.GET("", a.handlers.OperationLog.GetOperationLogs)
		logGroup.GET("/:id", a.handlers.OperationLog.GetOperationLog)
	}
}

// setupAutoReplyRoutes 自動回復關鍵詞管理路由
func (a *App) setupAutoReplyRoutes(authRequired *gin.RouterGroup) {
	autoReplyGroup := authRequired.Group("/admin/auto-reply-keywords")
	{
		autoReplyGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "keyword", "view"),
			a.handlers.AutoReply.GetKeywordList)
		autoReplyGroup.GET("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "keyword", "view"),
			a.handlers.AutoReply.GetKeyword)
		autoReplyGroup.POST("",
			middleware.RequireRBACPermission(a.services.RBAC, "keyword", "create"),
			a.handlers.AutoReply.CreateKeyword)
		autoReplyGroup.PUT("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "keyword", "update"),
			a.handlers.AutoReply.UpdateKeyword)
		autoReplyGroup.PATCH("/:id/status",
			middleware.RequireRBACPermission(a.services.RBAC, "keyword", "update"),
			a.handlers.AutoReply.UpdateKeywordStatus)
		autoReplyGroup.DELETE("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "keyword", "delete"),
			a.handlers.AutoReply.DeleteKeyword)
		autoReplyGroup.POST("/batch-delete",
			middleware.RequireRBACPermission(a.services.RBAC, "keyword", "delete"),
			a.handlers.AutoReply.BatchDeleteKeywords)
	}
}

// setupCustomerConversationRoutes 客户咨询对话记录管理路由
func (a *App) setupCustomerConversationRoutes(authRequired *gin.RouterGroup) {
	conversationGroup := authRequired.Group("/admin/customer-conversations")
	{
		conversationGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "customer_conversation", "view"),
			a.handlers.CustomerConversation.GetConversationList)
		conversationGroup.GET("/stats",
			middleware.RequireRBACPermission(a.services.RBAC, "customer_conversation", "view"),
			a.handlers.CustomerConversation.GetStats)
		conversationGroup.GET("/keyword-stats",
			middleware.RequireRBACPermission(a.services.RBAC, "customer_conversation", "view"),
			a.handlers.CustomerConversation.GetKeywordMatchStats)
		conversationGroup.GET("/session/:session_id",
			middleware.RequireRBACPermission(a.services.RBAC, "customer_conversation", "view"),
			a.handlers.CustomerConversation.GetSessionConversations)
		// 活跃会话列表
		conversationGroup.GET("/sessions",
			middleware.RequireRBACPermission(a.services.RBAC, "customer_conversation", "view"),
			a.handlers.CustomerConversation.GetActiveSessions)
		// 管理员回复
		conversationGroup.POST("/session/:session_id/reply",
			middleware.RequireRBACPermission(a.services.RBAC, "customer_conversation", "view"),
			a.handlers.CustomerConversation.AdminReply)
	}
}

// setupChatTagRoutes 聊天室標籤管理路由
func (a *App) setupChatTagRoutes(authRequired *gin.RouterGroup) {
	chatTagGroup := authRequired.Group("/admin/chat-tags")
	{
		chatTagGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "alert_view"),
			a.handlers.ChatTag.ListTags)
		chatTagGroup.GET("/stats",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "alert_view"),
			a.handlers.ChatTag.GetStats)
		chatTagGroup.POST("",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "alert_view"),
			a.handlers.ChatTag.CreateTag)
		chatTagGroup.POST("/sync",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "alert_view"),
			a.handlers.ChatTag.TriggerSync)
		chatTagGroup.DELETE("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "alert_view"),
			a.handlers.ChatTag.DeleteTag)
	}
}

// setupConnectorRoutes Connector 狀態路由
func (a *App) setupConnectorRoutes(authRequired *gin.RouterGroup) {
	if a.handlers.Connector == nil {
		return
	}
	connectorGroup := authRequired.Group("/admin/connectors")
	{
		connectorGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "connector", "view"),
			a.handlers.Connector.GetConnectorsStatus)
	}
}

// setupMonitorRoutes 系統監控路由
func (a *App) setupMonitorRoutes(authRequired *gin.RouterGroup) {
	authRequired.GET("/admin/monitor",
		middleware.RequireRBACPermission(a.services.RBAC, "connector", "view"),
		a.handlers.Monitor.GetMonitor)
}

// setupProxyConfigRoutes 代理配置管理路由
func (a *App) setupProxyConfigRoutes(authRequired *gin.RouterGroup) {
	proxyGroup := authRequired.Group("/admin/proxy-configs")
	{
		// 列表和查詢
		proxyGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "proxy", "view"),
			a.handlers.ProxyConfig.GetProxyConfigList)
		proxyGroup.GET("/options",
			middleware.RequireRBACPermission(a.services.RBAC, "proxy", "view"),
			a.handlers.ProxyConfig.GetEnabledProxyConfigs)
		proxyGroup.GET("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "proxy", "view"),
			a.handlers.ProxyConfig.GetProxyConfig)

		// 創建和更新
		proxyGroup.POST("",
			middleware.RequireRBACPermission(a.services.RBAC, "proxy", "create"),
			a.handlers.ProxyConfig.CreateProxyConfig)
		proxyGroup.PUT("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "proxy", "update"),
			a.handlers.ProxyConfig.UpdateProxyConfig)
		proxyGroup.PUT("/:id/status",
			middleware.RequireRBACPermission(a.services.RBAC, "proxy", "update"),
			a.handlers.ProxyConfig.UpdateProxyConfigStatus)

		// 刪除
		proxyGroup.DELETE("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "proxy", "delete"),
			a.handlers.ProxyConfig.DeleteProxyConfig)
	}
}

// setupConnectorConfigRoutes Connector 配置管理路由
func (a *App) setupConnectorConfigRoutes(authRequired *gin.RouterGroup) {
	configGroup := authRequired.Group("/admin/connector-configs")
	{
		// 列表和查詢
		configGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "connector", "view"),
			a.handlers.ConnectorConfig.GetConnectorConfigList)
		configGroup.GET("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "connector", "view"),
			a.handlers.ConnectorConfig.GetConnectorConfig)
		configGroup.GET("/:id/status",
			middleware.RequireRBACPermission(a.services.RBAC, "connector", "view"),
			a.handlers.ConnectorConfig.GetConnectorStatus)

		// 創建和更新
		configGroup.POST("",
			middleware.RequireRBACPermission(a.services.RBAC, "connector", "create"),
			a.handlers.ConnectorConfig.CreateConnectorConfig)
		configGroup.PUT("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "connector", "update"),
			a.handlers.ConnectorConfig.UpdateConnectorConfig)

		// 刪除
		configGroup.DELETE("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "connector", "delete"),
			a.handlers.ConnectorConfig.DeleteConnectorConfig)

		// 代理綁定
		configGroup.POST("/:id/bind-proxy",
			middleware.RequireRBACPermission(a.services.RBAC, "connector", "update"),
			a.handlers.ConnectorConfig.BindProxy)
		configGroup.POST("/:id/unbind-proxy",
			middleware.RequireRBACPermission(a.services.RBAC, "connector", "update"),
			a.handlers.ConnectorConfig.UnbindProxy)

		// Connector 操作（in-process）
		configGroup.POST("/:id/start",
			middleware.RequireRBACPermission(a.services.RBAC, "connector", "manage"),
			a.handlers.ConnectorConfig.StartConnector)
		configGroup.POST("/:id/stop",
			middleware.RequireRBACPermission(a.services.RBAC, "connector", "manage"),
			a.handlers.ConnectorConfig.StopConnector)
		configGroup.POST("/:id/restart",
			middleware.RequireRBACPermission(a.services.RBAC, "connector", "manage"),
			a.handlers.ConnectorConfig.RestartConnector)
	}
}

// setupModerationConfigRoutes 敏感詞設定路由
func (a *App) setupModerationConfigRoutes(authRequired *gin.RouterGroup) {
	moderationConfigGroup := authRequired.Group("/admin/content-moderation/config")
	{
		moderationConfigGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "config_view"),
			a.handlers.ModerationConfig.GetModerationConfigs)
		moderationConfigGroup.PUT("/:key",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "config_update"),
			a.handlers.ModerationConfig.UpdateModerationConfig)
		moderationConfigGroup.POST("/telegram/test",
			middleware.RequireRBACPermission(a.services.RBAC, "content_moderation", "config_update"),
			a.handlers.ModerationConfig.TestTelegram)
	}
}

// setupAiTagDefinitionRoutes AI 標籤定義管理路由
func (a *App) setupAiTagDefinitionRoutes(authRequired *gin.RouterGroup) {
	group := authRequired.Group("/admin/ai-tags")
	{
		group.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "ai_tag", "view"),
			a.handlers.AiTagDefinition.List)
		group.POST("",
			middleware.RequireRBACPermission(a.services.RBAC, "ai_tag", "create"),
			a.handlers.AiTagDefinition.Create)
		group.PUT("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "ai_tag", "update"),
			a.handlers.AiTagDefinition.Update)
		group.DELETE("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "ai_tag", "delete"),
			a.handlers.AiTagDefinition.Delete)
	}
}

// setupWorkgroupRoutes 工作組管理路由（Admin）
func (a *App) setupWorkgroupRoutes(authRequired *gin.RouterGroup) {
	wgGroup := authRequired.Group("/admin/workgroups")
	{
		wgGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "workgroup", "read"),
			a.handlers.Workgroup.List)
		wgGroup.POST("",
			middleware.RequireRBACPermission(a.services.RBAC, "workgroup", "write"),
			a.handlers.Workgroup.Create)
		wgGroup.GET("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "workgroup", "read"),
			a.handlers.Workgroup.GetByID)
		wgGroup.PUT("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "workgroup", "write"),
			a.handlers.Workgroup.Update)
		wgGroup.POST("/:id/archive",
			middleware.RequireRBACPermission(a.services.RBAC, "workgroup", "write"),
			a.handlers.Workgroup.Archive)

		// 帳號分配（獨立權限）
		wgGroup.GET("/:id/accounts",
			middleware.RequireRBACPermission(a.services.RBAC, "workgroup_account", "read"),
			a.handlers.Workgroup.GetAccounts)
		wgGroup.POST("/:id/accounts",
			middleware.RequireRBACPermission(a.services.RBAC, "workgroup_account", "write"),
			a.handlers.Workgroup.AssignAccounts)
		wgGroup.DELETE("/:id/accounts",
			middleware.RequireRBACPermission(a.services.RBAC, "workgroup_account", "write"),
			a.handlers.Workgroup.RemoveAccounts)
	}

	// 可分配帳號列表
	authRequired.GET("/admin/accounts/assignable",
		middleware.RequireRBACPermission(a.services.RBAC, "workgroup_account", "read"),
		a.handlers.Workgroup.GetAssignableAccounts)

	// 按條件查詢可分配帳號數量
	authRequired.GET("/admin/accounts/assignable/count",
		middleware.RequireRBACPermission(a.services.RBAC, "workgroup_account", "read"),
		a.handlers.Workgroup.GetAssignableAccountsCount)

	// 按條件批量分配帳號到工作組
	wgGroup.POST("/:id/accounts/assign-by-condition",
		middleware.RequireRBACPermission(a.services.RBAC, "workgroup_account", "write"),
		a.handlers.Workgroup.AssignAccountsByCondition)
}

// setupAdminAgentRoutes Admin Agent 管理路由
func (a *App) setupAdminAgentRoutes(authRequired *gin.RouterGroup) {
	agentGroup := authRequired.Group("/admin/agents")
	{
		agentGroup.GET("",
			middleware.RequireRBACPermission(a.services.RBAC, "agent", "read"),
			a.handlers.AgentManagement.List)
		agentGroup.POST("",
			middleware.RequireRBACPermission(a.services.RBAC, "agent", "write"),
			a.handlers.AgentManagement.Create)
		agentGroup.GET("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "agent", "read"),
			a.handlers.AgentManagement.GetByID)
		agentGroup.PUT("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "agent", "write"),
			a.handlers.AgentManagement.Update)
		agentGroup.DELETE("/:id",
			middleware.RequireRBACPermission(a.services.RBAC, "agent", "delete"),
			a.handlers.AgentManagement.Delete)
		agentGroup.POST("/:id/reset-password",
			middleware.RequireRBACPermission(a.services.RBAC, "agent", "write"),
			a.handlers.AgentManagement.ResetPassword)
	}
}

// setupServiceRoutes 內部微服務 API 路由
func (a *App) setupServiceRoutes(api *gin.RouterGroup) {
	serviceGroup := api.Group("/service")
	serviceGroup.Use(middleware.ServiceAPIKeyMiddleware(a.config.ServiceAPI.Key))
	{
		serviceGroup.POST("/upload", a.handlers.Media.UploadFile)
	}
}

// setupAgentRoutes Agent API 路由
func (a *App) setupAgentRoutes(api *gin.RouterGroup) {
	agentAPI := api.Group("/agent")

	// 公開：登入
	agentAPI.POST("/auth/login", a.handlers.AgentAuth.Login)

	// 需要 Agent 認證
	agentAuth := agentAPI.Group("/")
	agentAuth.Use(middleware.AgentAuthMiddleware(a.services.AgentAuth))
	agentAuth.Use(middleware.WorkgroupActiveMiddleware(a.services.AgentManagement))
	{
		// 認證
		agentAuth.POST("/auth/logout", a.handlers.AgentAuth.Logout)
		agentAuth.GET("/auth/profile", a.handlers.AgentAuth.GetProfile)
		agentAuth.POST("/auth/change-password", a.handlers.AgentAuth.ChangePassword)

		// Leader 功能
		leaderGroup := agentAuth.Group("/members")
		leaderGroup.Use(middleware.RequireAgentRole("leader"))
		{
			leaderGroup.GET("", a.handlers.AgentLeader.GetMembers)
			leaderGroup.POST("", a.handlers.AgentLeader.CreateMember)
			leaderGroup.GET("/:id", a.handlers.AgentLeader.GetMember)
			leaderGroup.PUT("/:id", a.handlers.AgentLeader.UpdateMember)
			leaderGroup.DELETE("/:id", a.handlers.AgentLeader.DeleteMember)
			leaderGroup.POST("/:id/reset-password", a.handlers.AgentLeader.ResetMemberPassword)
			leaderGroup.POST("/:id/accounts", a.handlers.AgentLeader.AssignMemberAccounts)
			leaderGroup.DELETE("/:id/accounts", a.handlers.AgentLeader.RemoveMemberAccounts)
			leaderGroup.GET("/:id/accounts", a.handlers.AgentLeader.GetMemberAccounts)
		}

		// Leader 工作組設定
		wgSettingsGroup := agentAuth.Group("/workgroup")
		wgSettingsGroup.Use(middleware.RequireAgentRole("leader"))
		{
			wgSettingsGroup.GET("/settings", a.handlers.AgentLeader.GetWorkgroupSettings)
			wgSettingsGroup.PUT("/settings", a.handlers.AgentLeader.UpdateWorkgroupSettings)
		}

		// 組長審計日誌
		agentAuth.GET("/activity-logs", middleware.RequireAgentRole("leader"), a.handlers.AgentActivityLog.GetActivityLogs)

		// 工作組帳號統計
		agentAuth.GET("/workgroup/account-stats", a.handlers.AgentOperations.GetWorkgroupAccountStats)

		// 帳號操作（leader + member 都可）
		agentAccountPathAccess := middleware.AgentAccountPathAccessMiddleware(a.services.AgentOperations)
		agentAuth.GET("/accounts", a.handlers.AgentOperations.GetMyAccounts)
		agentAuth.GET("/accounts/:id", a.handlers.AgentOperations.GetAccountDetail)
		agentAuth.GET("/accounts/:id/chats", a.handlers.AgentOperations.GetAccountChats)
		agentAuth.GET("/accounts/:id/chats/counts", a.handlers.AgentOperations.GetAccountChatCounts)
		agentAuth.GET("/accounts/:id/contacts", a.handlers.AgentOperations.GetAccountContacts)
		agentAuth.GET("/accounts/:id/contacts/export", a.handlers.AgentOperations.ExportAccountContacts)
		agentAuth.POST("/accounts/:id/chats", middleware.AgentWritePermission(), a.handlers.AgentOperations.CreateChat)
		agentAuth.GET("/accounts/:id/chats/:jid/messages", a.handlers.AgentOperations.GetChatMessages)
		agentAuth.POST("/accounts/:id/send", middleware.AgentWritePermission(), a.handlers.AgentOperations.SendMessage)

		// 統一聊天列表 & 釘選
		agentAuth.GET("/chats", a.handlers.AgentOperations.GetUnifiedChats)
		agentAuth.POST("/chats/:chatId/pin", middleware.AgentWritePermission(), a.handlers.AgentOperations.PinChat)
		agentAuth.DELETE("/chats/:chatId/pin", middleware.AgentWritePermission(), a.handlers.AgentOperations.UnpinChat)

		// 媒體上傳（復用 admin 的 handler）
		agentAuth.POST("/media/upload/image", middleware.AgentWritePermission(), a.handlers.Media.UploadImage)
		agentAuth.POST("/media/upload/audio", middleware.AgentWritePermission(), a.handlers.Media.UploadAudio)
		agentAuth.POST("/media/upload/video", middleware.AgentWritePermission(), a.handlers.Media.UploadVideo)
		agentAuth.POST("/media/upload/document", middleware.AgentWritePermission(), a.handlers.Media.UploadDocument)

		// 訊息操作（撤回、刪除）
		agentAuth.POST("/messages/:message_id/revoke", middleware.AgentWritePermission(), a.handlers.AgentOperations.RevokeMessage)
		agentAuth.POST("/messages/:message_id/delete-for-me", middleware.AgentWritePermission(), a.handlers.AgentOperations.DeleteMessageForMe)

		// 對話歸檔（復用 admin ChatHandler，middleware 做權限檢查）
		agentChatAccess := middleware.AgentChatAccessMiddleware(a.services.AgentOperations)
		agentAuth.POST("/chats/:chatId/archive", middleware.AgentWritePermission(), agentChatAccess, a.handlers.WhatsApp.ArchiveChat)
		agentAuth.POST("/chats/:chatId/unarchive", middleware.AgentWritePermission(), agentChatAccess, a.handlers.WhatsApp.UnarchiveChat)

		// SSE（Agent 專用，必帶 account_ids，逗號分隔）
		agentAuth.GET("/sse", realtime.SSE.HandleAgentSSE)

		// 翻譯（復用 admin 的 handler）
		agentAuth.POST("/translation/translate", a.handlers.Translation.Translate)
		agentAuth.POST("/translation/batch", a.handlers.Translation.BatchTranslate)
		agentAuth.GET("/translation/config", a.handlers.Translation.GetTranslationConfig)
		agentAuth.PUT("/translation/config", a.handlers.Translation.UpdateTranslationConfig)

		// 推荐码（Agent 端）
		agentAuth.GET("/accounts/:id/referral-profile", agentAccountPathAccess, a.handlers.Referral.GetReferralProfile)
		agentAuth.POST("/accounts/:id/referral-code", middleware.AgentWritePermission(), agentAccountPathAccess, a.handlers.Referral.GenerateReferralCode)

		// User Data
		agentAuth.GET("/user-data", a.handlers.AgentOperations.GetUserDataList)
		agentAuth.GET("/user-data/:phone", a.handlers.AgentOperations.GetUserDataByPhone)
	}
}

// setupProcurementRoutes 设置采购合同路由
func (a *App) setupProcurementRoutes(api *gin.RouterGroup) {
	// 公开路由
	publicGroup := api.Group("/public/contracts")
	{
		publicGroup.GET("/:id", a.handlers.Contract.GetPublicContract)
		publicGroup.PUT("/:id", a.handlers.Contract.UpdateContract)
		publicGroup.POST("/:id/submit", a.handlers.Contract.SubmitContract)
	}

	// 管理员路由
	adminGroup := api.Group("/admin/contracts")
	adminGroup.Use(middleware.AuthMiddleware(a.services.Auth))
	{
		adminGroup.POST("", a.handlers.Contract.CreateContract)
		adminGroup.GET("", a.handlers.Contract.ListContracts)
		adminGroup.PUT("/:id", a.handlers.Contract.UpdateContract)
		adminGroup.DELETE("/:id", a.handlers.Contract.DeleteContract)
		adminGroup.POST("/generate-sample", a.handlers.Contract.GenerateSample)
	}
}
