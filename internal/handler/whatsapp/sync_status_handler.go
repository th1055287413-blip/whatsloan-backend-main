package whatsapp

import (
	"net/http"
	"strconv"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/service/whatsapp"

	"github.com/gin-gonic/gin"
)

// SyncStatusHandler 同步狀態處理器
type SyncStatusHandler struct {
	dataService whatsapp.DataService
}

// NewSyncStatusHandler 創建同步狀態處理器
func NewSyncStatusHandler(dataService whatsapp.DataService) *SyncStatusHandler {
	return &SyncStatusHandler{
		dataService: dataService,
	}
}

// SyncStepResponse 同步步驟回應
type SyncStepResponse struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Status      string      `json:"status"`
	StartedAt   interface{} `json:"started_at"`
	CompletedAt interface{} `json:"completed_at"`
	Error       string      `json:"error,omitempty"`
	Count       int         `json:"count,omitempty"`
	Progress    string      `json:"progress,omitempty"`
}

// SyncStatusResponse 同步狀態回應
type SyncStatusResponse struct {
	AccountID      uint               `json:"account_id"`
	Steps          []SyncStepResponse `json:"steps"`
	OverallStatus  string             `json:"overall_status"`
	LastFullSyncAt interface{}        `json:"last_full_sync_at"`
}

// GetSyncStatus 獲取帳號同步狀態
// @Summary 獲取帳號同步狀態
// @Description 獲取指定帳號的同步進度詳情
// @Tags WhatsApp
// @Accept json
// @Produce json
// @Param id path int true "帳號ID"
// @Success 200 {object} SyncStatusResponse
// @Router /accounts/{id}/sync-status [get]
func (h *SyncStatusHandler) GetSyncStatus(c *gin.Context) {
	accountIDStr := c.Param("id")
	accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無效的帳號 ID"})
		return
	}

	syncStatusService := h.dataService.GetSyncStatusService()
	if syncStatusService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "同步狀態服務不可用"})
		return
	}

	status, err := syncStatusService.GetOrCreate(uint(accountID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取同步狀態失敗"})
		return
	}

	response := SyncStatusResponse{
		AccountID:      uint(accountID),
		OverallStatus:  string(status.GetOverallStatus()),
		LastFullSyncAt: status.LastFullSyncAt,
		Steps: []SyncStepResponse{
			{
				Name:        "帳號連接",
				Type:        "account_connect",
				Status:      string(status.ConnectStatus),
				StartedAt:   status.ConnectStartedAt,
				CompletedAt: status.ConnectCompletedAt,
				Error:       status.ConnectError,
			},
			{
				Name:        "聊天列表同步",
				Type:        "chat_sync",
				Status:      string(status.ChatSyncStatus),
				StartedAt:   status.ChatSyncStartedAt,
				CompletedAt: status.ChatSyncCompletedAt,
				Error:       status.ChatSyncError,
				Count:       status.ChatSyncCount,
			},
			{
				Name:        "歷史訊息同步",
				Type:        "history_sync",
				Status:      string(status.HistorySyncStatus),
				StartedAt:   status.HistorySyncStartedAt,
				CompletedAt: status.HistorySyncCompletedAt,
				Error:       status.HistorySyncError,
				Progress:    status.HistorySyncProgress,
			},
			{
				Name:        "聯絡人同步",
				Type:        "contact_sync",
				Status:      string(status.ContactSyncStatus),
				StartedAt:   status.ContactSyncStartedAt,
				CompletedAt: status.ContactSyncCompletedAt,
				Error:       status.ContactSyncError,
				Count:       status.ContactSyncCount,
			},
		},
	}

	common.Success(c, response)
}
