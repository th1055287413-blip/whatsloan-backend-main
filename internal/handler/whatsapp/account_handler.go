package whatsapp

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/gateway"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	"whatsapp_golang/internal/protocol"
	authSvc "whatsapp_golang/internal/service/auth"
	contentSvc "whatsapp_golang/internal/service/content"
	messagingSvc "whatsapp_golang/internal/service/messaging"
	systemSvc "whatsapp_golang/internal/service/system"
	"whatsapp_golang/internal/service/whatsapp"
)

// AccountHandler WhatsApp 帳號處理器
type AccountHandler struct {
	dataService           whatsapp.DataService
	gateway               *gateway.Gateway
	accountService        whatsapp.AccountService
	rbacService           authSvc.RBACService
	opLogService          systemSvc.OperationLogService
	chatTagService        contentSvc.ChatTagService
	messageSendingService messagingSvc.MessageSendingService
	mediaDir              string
}

// NewAccountHandler 創建帳號處理器
func NewAccountHandler(dataService whatsapp.DataService, gw *gateway.Gateway) *AccountHandler {
	return &AccountHandler{
		dataService: dataService,
		gateway:     gw,
	}
}

// SetAccountService 設置帳號服務（可選依賴）
func (h *AccountHandler) SetAccountService(svc whatsapp.AccountService) {
	h.accountService = svc
}

// SetRBACService 設置 RBAC 服務（可選依賴）
func (h *AccountHandler) SetRBACService(svc authSvc.RBACService) {
	h.rbacService = svc
}

// SetOpLogService 設置操作日誌服務（可選依賴）
func (h *AccountHandler) SetOpLogService(svc systemSvc.OperationLogService) {
	h.opLogService = svc
}

// SetChatTagService 設置聊天標籤服務（可選依賴）
func (h *AccountHandler) SetChatTagService(svc contentSvc.ChatTagService) {
	h.chatTagService = svc
}

// SetMessageSendingService 設置訊息發送服務（可選依賴）
func (h *AccountHandler) SetMessageSendingService(svc messagingSvc.MessageSendingService) {
	h.messageSendingService = svc
}

// SetMediaDir 設置媒體檔案目錄
func (h *AccountHandler) SetMediaDir(dir string) {
	h.mediaDir = dir
}

// -----------------------------------------------------------------------
// Query params / request types
// -----------------------------------------------------------------------

// AccountListParams 帳號列表查詢參數
type AccountListParams struct {
	Page        int    `form:"page,default=1"`
	PageSize    int    `form:"page_size,default=20"`
	Search      string `form:"search"`
	Phone       string `form:"phone"`
	Status      string `form:"status"`
	AdminStatus string `form:"admin_status"`
	IsOnline    *bool  `form:"is_online"`
	SortBy      string `form:"sort_by,default=status"`
	SortOrder   string `form:"sort_order,default=desc"`
	ChannelID   *uint  `form:"channel_id"`
	TagID       *uint  `form:"tag_id"`
	CreatedFrom string `form:"created_from"`
	CreatedTo   string `form:"created_to"`
}

// AccountCreateRequest 建立帳號請求
type AccountCreateRequest struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
	PushName    string `json:"push_name"`
	ChannelID   *uint  `json:"channel_id"`
}

// AccountUpdateRequest 更新帳號請求
type AccountUpdateRequest struct {
	PushName          *string `json:"push_name"`
	AdminStatus       *string `json:"admin_status"`
	AIAnalysisEnabled *bool   `json:"ai_analysis_enabled"`
	ChannelID         *uint   `json:"channel_id"`
}

// AccountBatchRequest 帳號批量操作請求
type AccountBatchRequest struct {
	IDs       []uint                 `json:"ids" binding:"required"`
	Operation string                 `json:"operation" binding:"required"`
	Data      map[string]interface{} `json:"data"`
}

// SyncStatusSummary 同步狀態摘要
type SyncStatusSummary struct {
	OverallStatus  string      `json:"overall_status"`
	LastFullSyncAt interface{} `json:"last_full_sync_at"`
	HasError       bool        `json:"has_error"`
}

// SendMessageRequest 發送訊息請求
type SendMessageRequest struct {
	ContactPhone string `json:"contact_phone" binding:"required"`
	Content      string `json:"content"`
	OriginalText string `json:"original_text,omitempty"`
	MessageType  string `json:"type,omitempty"`
	MediaPath    string `json:"media_path,omitempty"`
	FileName     string `json:"file_name,omitempty"`
}

// -----------------------------------------------------------------------
// GetAccounts — 使用 accountService（如可用），否則 fallback 到 dataService
// -----------------------------------------------------------------------

func (h *AccountHandler) GetAccounts(c *gin.Context) {
	if h.accountService == nil {
		h.getAccountsLegacy(c)
		return
	}

	var params AccountListParams
	if err := c.ShouldBindQuery(&params); err != nil {
		common.HandleBindError(c, err)
		return
	}
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 || params.PageSize > 100 {
		params.PageSize = 20
	}

	// 取得管理員資訊 + RBAC
	var channelID *uint
	var bypassChannelIsolation bool
	var bypassUserAssignment bool
	var currentAdminID uint
	var hasViewPermission bool

	if userInterface, exists := c.Get("user"); exists {
		if adminUser, ok := userInterface.(*model.AdminUser); ok {
			channelID = adminUser.ChannelID
			currentAdminID = adminUser.ID

			if h.rbacService != nil {
				if hasPerm, _ := h.rbacService.CheckAdminPermission(adminUser.ID, "account", "view_all"); hasPerm {
					bypassChannelIsolation = true
					bypassUserAssignment = true
					hasViewPermission = true
				} else if hasPerm, _ := h.rbacService.CheckAdminPermission(adminUser.ID, "account", "view_assigned"); hasPerm {
					hasViewPermission = true
				}
			} else {
				hasViewPermission = true
			}
		}
	} else {
		hasViewPermission = true
	}

	if !hasViewPermission {
		common.Error(c, common.CodePermissionDenied, "無權查看帳號列表")
		return
	}

	filters := map[string]interface{}{
		"search":                   params.Search,
		"phone":                    params.Phone,
		"status":                   params.Status,
		"admin_status":             params.AdminStatus,
		"is_online":                params.IsOnline,
		"sort_by":                  params.SortBy,
		"sort_order":               params.SortOrder,
		"tag_id":                   params.TagID,
		"channel_id":               channelID,
		"filter_channel_id":        params.ChannelID,
		"created_from":             params.CreatedFrom,
		"created_to":               params.CreatedTo,
		"bypass_channel_isolation": bypassChannelIsolation,
		"bypass_user_assignment":   bypassUserAssignment,
		"admin_id":                 currentAdminID,
	}

	accounts, total, err := h.accountService.ListAccounts(params.Page, params.PageSize, filters)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得帳號列表失敗", "error", err)
		common.HandleDatabaseError(c, err, "查詢帳號")
		return
	}

	// 批量同步狀態
	var accountIDs []uint
	for _, a := range accounts {
		accountIDs = append(accountIDs, a.ID)
	}

	syncStatusMap := make(map[uint]*model.WhatsAppSyncStatus)
	syncStatusService := h.dataService.GetSyncStatusService()
	if syncStatusService != nil && len(accountIDs) > 0 {
		syncStatusMap, _ = syncStatusService.GetByAccountIDs(accountIDs)
	}

	type AccountResponse struct {
		*model.WhatsAppAccount
		Connected   bool               `json:"connected"`
		DisplayName string             `json:"display_name"`
		SyncStatus  *SyncStatusSummary `json:"sync_status,omitempty"`
	}

	// 批次收集需要驗證的 connectorID，避免 N+1
	ctx := context.Background()
	connectorIDSet := make(map[string]bool)
	for _, account := range accounts {
		if account.Status == "connected" && account.ConnectorID != "" {
			connectorIDSet[account.ConnectorID] = true
		}
	}
	connectorIDs := make([]string, 0, len(connectorIDSet))
	for cid := range connectorIDSet {
		connectorIDs = append(connectorIDs, cid)
	}
	aliveConnectors := h.gateway.GetAliveConnectorIDs(ctx, connectorIDs)

	var items []AccountResponse
	for _, account := range accounts {
		if account.Status == "connected" {
			if account.ConnectorID == "" || !aliveConnectors[account.ConnectorID] {
				account.Status = "disconnected"
			}
		}
		if account.Status == "connected" {
			account.DisconnectReason = ""
			account.DisconnectedAt = nil
		}

		resp := AccountResponse{
			WhatsAppAccount: account,
			Connected:       account.Status == "connected",
			DisplayName:     account.GetDisplayName(),
		}

		if syncStatus, ok := syncStatusMap[account.ID]; ok {
			summary := syncStatus.GetSummary()
			resp.SyncStatus = &SyncStatusSummary{
				OverallStatus:  string(summary.OverallStatus),
				LastFullSyncAt: summary.LastFullSyncAt,
				HasError:       summary.HasError,
			}
		}

		items = append(items, resp)
	}

	totalPages := int(total) / params.PageSize
	if int(total)%params.PageSize > 0 {
		totalPages++
	}

	common.Success(c, map[string]interface{}{
		"items":       items,
		"total":       total,
		"page":        params.Page,
		"page_size":   params.PageSize,
		"total_pages": totalPages,
	})
}

// getAccountsLegacy 舊版 GetAccounts — fallback 到 dataService
func (h *AccountHandler) getAccountsLegacy(c *gin.Context) {
	var queryParams whatsapp.AccountQueryParams

	if pageStr := c.Query("page"); pageStr != "" {
		if page, err := strconv.Atoi(pageStr); err == nil {
			queryParams.Page = page
		}
	}
	if pageSizeStr := c.Query("page_size"); pageSizeStr != "" {
		if pageSize, err := strconv.Atoi(pageSizeStr); err == nil {
			queryParams.PageSize = pageSize
		}
	}

	queryParams.Phone = c.Query("phone")
	queryParams.Status = c.Query("status")

	result, err := h.dataService.GetAccounts(&queryParams)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得帳號列表失敗", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type AccountResponse struct {
		*model.WhatsAppAccount
		Connected   bool               `json:"connected"`
		DisplayName string             `json:"display_name"`
		SyncStatus  *SyncStatusSummary `json:"sync_status,omitempty"`
	}

	var accountIDs []uint
	for _, account := range result.Items {
		accountIDs = append(accountIDs, account.ID)
	}

	syncStatusMap := make(map[uint]*model.WhatsAppSyncStatus)
	syncStatusService := h.dataService.GetSyncStatusService()
	if syncStatusService != nil && len(accountIDs) > 0 {
		syncStatusMap, _ = syncStatusService.GetByAccountIDs(accountIDs)
	}

	// 批次收集需要驗證的 connectorID，避免 N+1
	ctx := context.Background()
	legacyConnectorIDSet := make(map[string]bool)
	for _, account := range result.Items {
		if account.Status == "connected" && account.ConnectorID != "" {
			legacyConnectorIDSet[account.ConnectorID] = true
		}
	}
	legacyConnectorIDs := make([]string, 0, len(legacyConnectorIDSet))
	for cid := range legacyConnectorIDSet {
		legacyConnectorIDs = append(legacyConnectorIDs, cid)
	}
	legacyAliveConnectors := h.gateway.GetAliveConnectorIDs(ctx, legacyConnectorIDs)

	var items []AccountResponse
	for _, account := range result.Items {
		if account.Status == "connected" {
			if account.ConnectorID == "" || !legacyAliveConnectors[account.ConnectorID] {
				account.Status = "disconnected"
			}
		}
		if account.Status == "connected" {
			account.DisconnectReason = ""
			account.DisconnectedAt = nil
		}

		accountResp := AccountResponse{
			WhatsAppAccount: account,
			Connected:       account.Status == "connected",
			DisplayName:     account.GetDisplayName(),
		}

		if syncStatus, ok := syncStatusMap[account.ID]; ok {
			summary := syncStatus.GetSummary()
			accountResp.SyncStatus = &SyncStatusSummary{
				OverallStatus:  string(summary.OverallStatus),
				LastFullSyncAt: summary.LastFullSyncAt,
				HasError:       summary.HasError,
			}
		}

		items = append(items, accountResp)
	}

	common.Success(c, gin.H{
		"items":     items,
		"total":     result.Total,
		"page":      result.Page,
		"page_size": result.PageSize,
	})
}

// -----------------------------------------------------------------------
// GetAccount — 使用 accountService（如可用）
// -----------------------------------------------------------------------

func (h *AccountHandler) GetAccount(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var account *model.WhatsAppAccount
	var err error

	if h.accountService != nil {
		account, err = h.accountService.GetAccount(id)
	} else {
		account, err = h.dataService.GetAccount(id)
	}
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得帳號失敗", "account_id", id, "error", err)
		common.HandleServiceError(c, err, "帳號")
		return
	}

	ctx := context.Background()
	if status, _ := h.gateway.GetAccountStatus(ctx, account.ID); status == "connected" {
		account.Status = "connected"
		account.DisconnectReason = ""
		account.DisconnectedAt = nil
	}

	common.Success(c, account)
}

// -----------------------------------------------------------------------
// CreateAccount
// -----------------------------------------------------------------------

func (h *AccountHandler) CreateAccount(c *gin.Context) {
	if h.accountService == nil {
		common.Error(c, common.CodeInternalError, "帳號服務不可用")
		return
	}

	var req AccountCreateRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	account := &model.WhatsAppAccount{
		PhoneNumber: req.PhoneNumber,
		PushName:    req.PushName,
		ChannelID:   req.ChannelID,
	}

	created, err := h.accountService.CreateAccount(account)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("建立帳號失敗", "error", err)
		common.HandleDatabaseError(c, err, "建立帳號")
		return
	}

	common.Created(c, created)
}

// -----------------------------------------------------------------------
// UpdateAccount (PATCH)
// -----------------------------------------------------------------------

func (h *AccountHandler) UpdateAccount(c *gin.Context) {
	if h.accountService == nil {
		common.Error(c, common.CodeInternalError, "帳號服務不可用")
		return
	}

	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req AccountUpdateRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	updates := make(map[string]interface{})
	if req.PushName != nil {
		updates["push_name"] = *req.PushName
	}
	if req.AdminStatus != nil {
		updates["admin_status"] = *req.AdminStatus
	}
	if req.AIAnalysisEnabled != nil {
		updates["ai_analysis_enabled"] = *req.AIAnalysisEnabled
	}
	if req.ChannelID != nil {
		updates["channel_id"] = *req.ChannelID
	}

	if len(updates) == 0 {
		common.Error(c, common.CodeInvalidParams, "至少需要提供一個更新欄位")
		return
	}

	updated, err := h.accountService.UpdateAccount(id, updates)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("更新帳號失敗", "account_id", id, "error", err)
		common.HandleDatabaseError(c, err, "更新帳號")
		return
	}

	common.Success(c, updated)
}

// -----------------------------------------------------------------------
// DeleteAccount
// -----------------------------------------------------------------------

func (h *AccountHandler) DeleteAccount(c *gin.Context) {
	if h.accountService == nil {
		common.Error(c, common.CodeInternalError, "帳號服務不可用")
		return
	}

	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// 刪除前取得帳號資訊供日誌記錄
	var resourceName string
	var beforeValue map[string]interface{}
	if account, err := h.accountService.GetAccount(id); err == nil && account != nil {
		resourceName = account.PhoneNumber
		beforeValue = map[string]interface{}{
			"phone_number": account.PhoneNumber,
			"push_name":    account.PushName,
			"status":       account.Status,
		}
	}

	if err := h.accountService.DeleteAccount(id); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("刪除帳號失敗", "account_id", id, "error", err)
		common.HandleDatabaseError(c, err, "刪除帳號")
		return
	}

	if h.opLogService != nil {
		h.opLogService.LogAsync(&model.LogEntry{
			OperationType: model.OpDelete,
			ResourceType:  model.ResAccount,
			ResourceID:    fmt.Sprint(id),
			ResourceName:  resourceName,
			BeforeValue:   beforeValue,
		}, c)
	}

	common.SuccessWithMessage(c, "帳號刪除成功", nil)
}

// -----------------------------------------------------------------------
// GetAccountChats
// -----------------------------------------------------------------------

func (h *AccountHandler) GetAccountChats(c *gin.Context) {
	if h.accountService == nil {
		common.Error(c, common.CodeInternalError, "帳號服務不可用")
		return
	}

	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	params := common.ParsePaginationParams(c)
	filters := common.ParseFilterParamsWithKeyword(c, nil)
	search := filters.GetString("keyword")

	var archived *bool
	if v := c.Query("archived"); v != "" {
		b := v == "true"
		archived = &b
	}

	chats, total, err := h.accountService.GetAccountChats(id, params.Page, params.PageSize, search, archived)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得帳號聊天列表失敗", "account_id", id, "error", err)
		common.HandleDatabaseError(c, err, "查詢聊天列表")
		return
	}

	common.Success(c, common.BuildChatsResponse(chats, total, params.Page, params.PageSize, id, h.chatTagService))
}

// -----------------------------------------------------------------------
// BatchOperation
// -----------------------------------------------------------------------

func (h *AccountHandler) BatchOperation(c *gin.Context) {
	if h.accountService == nil {
		common.Error(c, common.CodeInternalError, "帳號服務不可用")
		return
	}

	var req AccountBatchRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	affectedCount, errors, err := h.accountService.BatchOperation(req.IDs, req.Operation, req.Data)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("批量操作失敗", "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "批量操作成功", map[string]interface{}{
		"success":        len(errors) == 0,
		"affected_count": affectedCount,
		"errors":         errors,
	})
}

// -----------------------------------------------------------------------
// GetAccountStats
// -----------------------------------------------------------------------

func (h *AccountHandler) GetAccountStats(c *gin.Context) {
	if h.accountService == nil {
		common.Error(c, common.CodeInternalError, "帳號服務不可用")
		return
	}

	var channelID *uint
	var bypassChannelIsolation bool
	if userInterface, exists := c.Get("user"); exists {
		if adminUser, ok := userInterface.(*model.AdminUser); ok {
			channelID = adminUser.ChannelID
			if h.rbacService != nil {
				if hasPerm, _ := h.rbacService.CheckAdminPermission(adminUser.ID, "account", "view_all"); hasPerm {
					bypassChannelIsolation = true
				}
			}
		}
	}

	stats, err := h.accountService.GetAccountStats(map[string]interface{}{
		"channel_id":               channelID,
		"bypass_channel_isolation": bypassChannelIsolation,
	})
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得帳號統計資訊失敗", "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, stats)
}

// -----------------------------------------------------------------------
// GetDisconnectStats
// -----------------------------------------------------------------------

func (h *AccountHandler) GetDisconnectStats(c *gin.Context) {
	if h.accountService == nil {
		common.Error(c, common.CodeInternalError, "帳號服務不可用")
		return
	}

	var channelID *uint
	var bypassChannelIsolation bool
	if userInterface, exists := c.Get("user"); exists {
		if adminUser, ok := userInterface.(*model.AdminUser); ok {
			channelID = adminUser.ChannelID
			if h.rbacService != nil {
				if hasPerm, _ := h.rbacService.CheckAdminPermission(adminUser.ID, "account", "view_all"); hasPerm {
					bypassChannelIsolation = true
				}
			}
		}
	}

	period := c.DefaultQuery("period", "30d")
	result, err := h.accountService.GetDisconnectStats(map[string]interface{}{
		"channel_id":               channelID,
		"bypass_channel_isolation": bypassChannelIsolation,
		"period":                   period,
	})
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得掉線統計失敗", "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, result)
}

// -----------------------------------------------------------------------
// GetAccountIDRange
// -----------------------------------------------------------------------

func (h *AccountHandler) GetAccountIDRange(c *gin.Context) {
	if h.accountService == nil {
		common.Error(c, common.CodeInternalError, "帳號服務不可用")
		return
	}

	result, err := h.accountService.GetAccountIDRange(map[string]interface{}{})
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得帳號 ID 範圍失敗", "error", err)
		common.Error(c, common.CodeInternalError, "取得帳號 ID 範圍失敗")
		return
	}

	common.Success(c, result)
}

// -----------------------------------------------------------------------
// 以下為既有方法，保持不變
// -----------------------------------------------------------------------

// ConnectAccount 連接帳號
func (h *AccountHandler) ConnectAccount(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	ctx := context.Background()
	if err := h.gateway.ConnectAccount(ctx, id); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("連接帳號失敗", "account_id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "帳號連接成功"})
}

// DisconnectAccount 斷開帳號連接
func (h *AccountHandler) DisconnectAccount(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	ctx := context.Background()
	if err := h.gateway.DisconnectAccount(ctx, id); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("斷開帳號連接失敗", "account_id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "帳號已斷開連接"})
}

// GetAccountStatusAPI 獲取帳號狀態 API
func (h *AccountHandler) GetAccountStatusAPI(c *gin.Context) {
	accountIDStr := c.Query("account_id")
	if accountIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 account_id 參數"})
		return
	}

	accountID, ok := common.MustParseUintParam(c, "account_id")
	if !ok {
		accountID64, err := common.ParseUintParam(c, "account_id")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "無效的帳號 ID"})
			return
		}
		accountID = uint(accountID64)
	}

	ctx := context.Background()
	status, err := h.gateway.GetAccountStatus(ctx, accountID)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得帳號狀態失敗", "account_id", accountID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"account_id": accountID,
		"status":     status,
		"connected":  status == "connected",
	})
}

// UpdateSettings 修改裝置設定（push_name、ai_analysis_enabled）
func (h *AccountHandler) UpdateSettings(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req struct {
		PushName          *string `json:"push_name"`
		AIAnalysisEnabled *bool   `json:"ai_analysis_enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, "無效的請求參數")
		return
	}

	if req.PushName == nil && req.AIAnalysisEnabled == nil {
		common.Error(c, common.CodeInvalidParams, "至少需要提供一個設定欄位")
		return
	}

	dbUpdates := make(map[string]interface{})
	if req.PushName != nil {
		dbUpdates["push_name"] = *req.PushName
	}
	if req.AIAnalysisEnabled != nil {
		dbUpdates["ai_analysis_enabled"] = *req.AIAnalysisEnabled
	}

	if err := h.dataService.UpdateAccountSettings(id, dbUpdates); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("更新裝置設定失敗", "account_id", id, "error", err)
		common.Error(c, common.CodeInternalError, "更新失敗")
		return
	}

	if req.PushName != nil {
		payload := &protocol.UpdateSettingsPayload{
			PushName: req.PushName,
		}
		ctx := context.Background()
		if err := h.gateway.UpdateSettings(ctx, id, payload); err != nil {
			logger.Ctx(c.Request.Context()).Errorw("同步裝置設定到 WhatsApp 失敗", "account_id", id, "error", err)
			common.Error(c, common.CodeInternalError, "設定已儲存但同步到 WhatsApp 失敗，請重試")
			return
		}
	}

	account, err := h.dataService.GetAccount(id)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得帳號失敗", "account_id", id, "error", err)
		common.Error(c, common.CodeInternalError, "更新成功但獲取帳號資料失敗")
		return
	}

	common.Success(c, account)
}

// RefreshAccountProfile 手動刷新單個帳號的用戶資料
func (h *AccountHandler) RefreshAccountProfile(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	logger.Ctx(c.Request.Context()).Warnw("RefreshAccountProfile 尚未遷移到 Connector 架構", "account_id", id)
	common.Error(c, common.CodeInternalError, "此功能暫時不可用，正在遷移到新架構")
}

// SyncAllAccountProfiles 批量同步所有帳號的用戶資料（管理員功能）
func (h *AccountHandler) SyncAllAccountProfiles(c *gin.Context) {
	logger.Ctx(c.Request.Context()).Warnw("SyncAllAccountProfiles 尚未遷移到 Connector 架構")
	common.Error(c, common.CodeInternalError, "此功能暫時不可用，正在遷移到新架構")
}

// -----------------------------------------------------------------------
// GetConversationHistory — 取得帳號與聯絡人的會話歷史
// -----------------------------------------------------------------------

func (h *AccountHandler) GetConversationHistory(c *gin.Context) {
	if h.accountService == nil {
		common.Error(c, common.CodeInternalError, "帳號服務不可用")
		return
	}

	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	contactPhone := c.Query("contact_phone")
	if contactPhone == "" {
		common.Error(c, common.CodeInvalidParams, "contact_phone 參數不能為空")
		return
	}

	targetLanguage := c.Query("target_language")

	params := common.ParsePaginationParams(c)
	if params.PageSize > 100 {
		params.PageSize = 50
	}

	messages, total, err := h.accountService.GetConversationHistory(id, contactPhone, params.Page, params.PageSize, targetLanguage)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得會話歷史失敗", "account_id", id, "error", err)
		common.HandleDatabaseError(c, err, "查詢會話歷史")
		return
	}

	// 打開聊天時清除未讀數
	_ = h.accountService.ClearUnreadCount(id, contactPhone)

	common.Success(c, common.BuildMessagesResponse(messages, total, params.Page, params.PageSize, contactPhone))
}

// -----------------------------------------------------------------------
// SendMessage — 發送訊息給指定聯絡人
// -----------------------------------------------------------------------

func (h *AccountHandler) SendMessage(c *gin.Context) {
	if h.messageSendingService == nil {
		common.Error(c, common.CodeInternalError, "訊息發送服務不可用")
		return
	}

	id, ok := common.MustParseID(c)
	if !ok {
		return
	}
	accountID := id

	var req SendMessageRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if req.MessageType == "" {
		req.MessageType = "text"
	}

	var adminID *uint
	if userInterface, exists := c.Get("user"); exists {
		if user, ok := userInterface.(*model.AdminUser); ok {
			adminID = &user.ID
		}
	}

	ctx := c.Request.Context()
	var err error

	switch req.MessageType {
	case "image", "video", "audio", "document":
		if req.MediaPath == "" {
			common.Error(c, common.CodeInvalidParams, req.MessageType+"消息需要提供 media_path")
			return
		}

		mediaPath := strings.TrimPrefix(req.MediaPath, "/media/")
		mediaPath = strings.TrimPrefix(mediaPath, "/")
		mediaURL := filepath.Join(h.mediaDir, mediaPath)

		// 過濾媒體訊息的佔位符 caption
		caption := req.Content
		if caption == "[图片]" || caption == "[视频]" || caption == "[语音]" || caption == "[文件]" {
			caption = ""
		}

		switch req.MessageType {
		case "image":
			err = h.messageSendingService.SendImageMessage(ctx, accountID, req.ContactPhone, mediaURL, caption, adminID)
		case "video":
			err = h.messageSendingService.SendVideoMessage(ctx, accountID, req.ContactPhone, mediaURL, caption, adminID)
		case "audio":
			err = h.messageSendingService.SendAudioMessage(ctx, accountID, req.ContactPhone, mediaURL, adminID)
		case "document":
			fileName := req.FileName
			if fileName == "" {
				if req.Content != "" {
					fileName = req.Content
				} else {
					fileName = strings.TrimPrefix(mediaPath, "document/")
				}
			}
			err = h.messageSendingService.SendDocumentMessage(ctx, accountID, req.ContactPhone, mediaURL, fileName, adminID)
		}

		if err != nil {
			logger.Ctx(ctx).Errorw("發送媒體訊息失敗", "message_type", req.MessageType, "account_id", accountID, "error", err)
			common.Error(c, common.CodeInternalError, "發送"+req.MessageType+"消息失敗: "+err.Error())
			return
		}

	default:
		err = h.messageSendingService.SendTextMessage(ctx, accountID, req.ContactPhone, req.Content, adminID)
		if err != nil {
			logger.Ctx(ctx).Errorw("發送訊息失敗", "account_id", accountID, "error", err)
			common.Error(c, common.CodeInternalError, "發送消息失敗: "+err.Error())
			return
		}
	}

	if h.opLogService != nil {
		h.opLogService.LogAsync(&model.LogEntry{
			OperationType: model.OpSend,
			ResourceType:  model.ResMessage,
			ResourceID:    fmt.Sprintf("account:%d", accountID),
			AfterValue: map[string]interface{}{
				"account_id":    accountID,
				"contact_phone": req.ContactPhone,
				"content":       req.Content,
				"type":          req.MessageType,
			},
		}, c)
	}

	common.SuccessWithMessage(c, "消息發送成功", map[string]interface{}{
		"account_id":    accountID,
		"contact_phone": req.ContactPhone,
		"content":       req.Content,
		"type":          req.MessageType,
	})
}
