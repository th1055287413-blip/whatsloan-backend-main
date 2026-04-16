package whatsapp

import (
	"fmt"
	"time"

	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// SyncStatusService 同步狀態服務接口
type SyncStatusService interface {
	GetOrCreate(accountID uint) (*model.WhatsAppSyncStatus, error)
	GetByAccountID(accountID uint) (*model.WhatsAppSyncStatus, error)
	GetByAccountIDs(accountIDs []uint) (map[uint]*model.WhatsAppSyncStatus, error)
	MarkQueued(accountID uint, step model.SyncStepType) error
	MarkRunning(accountID uint, step model.SyncStepType) error
	MarkCompleted(accountID uint, step model.SyncStepType, extra map[string]interface{}) error
	MarkFailed(accountID uint, step model.SyncStepType, errMsg string) error
	ResetAllSteps(accountID uint) error
	UpdateHistoryProgress(accountID uint, current, total int) error
}

// syncStatusService 同步狀態服務實現
type syncStatusService struct {
	db *gorm.DB
}

// NewSyncStatusService 建立同步狀態服務
func NewSyncStatusService(db *gorm.DB) SyncStatusService {
	return &syncStatusService{db: db}
}

// GetOrCreate 獲取或建立同步狀態記錄
func (s *syncStatusService) GetOrCreate(accountID uint) (*model.WhatsAppSyncStatus, error) {
	var status model.WhatsAppSyncStatus
	err := s.db.Where("account_id = ?", accountID).First(&status).Error
	if err == gorm.ErrRecordNotFound {
		// 檢查帳號當前狀態，如果已連接則初始化為已完成
		var account model.WhatsAppAccount
		initialStatus := model.SyncStatePending
		if s.db.First(&account, accountID).Error == nil && account.Status == "connected" {
			initialStatus = model.SyncStateCompleted
		}

		status = model.WhatsAppSyncStatus{
			AccountID:         accountID,
			ConnectStatus:     initialStatus,
			ChatSyncStatus:    initialStatus,
			HistorySyncStatus: initialStatus,
			ContactSyncStatus: initialStatus,
		}

		// 如果初始狀態為 completed，設定時間戳記
		if initialStatus == model.SyncStateCompleted {
			now := time.Now()
			status.ConnectStartedAt = &now
			status.ConnectCompletedAt = &now
			status.ChatSyncStartedAt = &now
			status.ChatSyncCompletedAt = &now
			status.HistorySyncStartedAt = &now
			status.HistorySyncCompletedAt = &now
			status.ContactSyncStartedAt = &now
			status.ContactSyncCompletedAt = &now
			status.LastFullSyncAt = &now
		}

		err = s.db.Create(&status).Error
	}
	return &status, err
}

// GetByAccountID 根據帳號 ID 獲取同步狀態
func (s *syncStatusService) GetByAccountID(accountID uint) (*model.WhatsAppSyncStatus, error) {
	var status model.WhatsAppSyncStatus
	err := s.db.Where("account_id = ?", accountID).First(&status).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &status, err
}

// GetByAccountIDs 批量獲取多個帳號的同步狀態
func (s *syncStatusService) GetByAccountIDs(accountIDs []uint) (map[uint]*model.WhatsAppSyncStatus, error) {
	var statuses []model.WhatsAppSyncStatus
	err := s.db.Where("account_id IN ?", accountIDs).Find(&statuses).Error
	if err != nil {
		return nil, err
	}

	result := make(map[uint]*model.WhatsAppSyncStatus)
	for i := range statuses {
		result[statuses[i].AccountID] = &statuses[i]
	}
	return result, nil
}

// MarkQueued 標記步驟為已入隊
func (s *syncStatusService) MarkQueued(accountID uint, step model.SyncStepType) error {
	// 確保記錄存在
	if _, err := s.GetOrCreate(accountID); err != nil {
		return err
	}

	updates := s.buildStatusUpdates(step, model.SyncStateQueued, "", nil)
	return s.db.Model(&model.WhatsAppSyncStatus{}).
		Where("account_id = ?", accountID).
		Updates(updates).Error
}

// MarkRunning 標記步驟為執行中
func (s *syncStatusService) MarkRunning(accountID uint, step model.SyncStepType) error {
	// 確保記錄存在
	if _, err := s.GetOrCreate(accountID); err != nil {
		return err
	}

	now := time.Now()
	updates := s.buildStatusUpdates(step, model.SyncStateRunning, "", nil)
	updates[s.getStartedAtField(step)] = now
	updates[s.getErrorField(step)] = ""

	return s.db.Model(&model.WhatsAppSyncStatus{}).
		Where("account_id = ?", accountID).
		Updates(updates).Error
}

// MarkCompleted 標記步驟為已完成
func (s *syncStatusService) MarkCompleted(accountID uint, step model.SyncStepType, extra map[string]interface{}) error {
	// 確保記錄存在
	if _, err := s.GetOrCreate(accountID); err != nil {
		return err
	}

	now := time.Now()
	updates := s.buildStatusUpdates(step, model.SyncStateCompleted, "", extra)
	updates[s.getCompletedAtField(step)] = now
	updates[s.getErrorField(step)] = ""

	// 如果是最後一個步驟完成，更新最後完整同步時間
	if step == model.SyncStepContact {
		updates["last_full_sync_at"] = now
	}

	return s.db.Model(&model.WhatsAppSyncStatus{}).
		Where("account_id = ?", accountID).
		Updates(updates).Error
}

// MarkFailed 標記步驟為失敗
func (s *syncStatusService) MarkFailed(accountID uint, step model.SyncStepType, errMsg string) error {
	// 確保記錄存在
	if _, err := s.GetOrCreate(accountID); err != nil {
		return err
	}

	now := time.Now()
	updates := s.buildStatusUpdates(step, model.SyncStateFailed, errMsg, nil)
	updates[s.getCompletedAtField(step)] = now

	return s.db.Model(&model.WhatsAppSyncStatus{}).
		Where("account_id = ?", accountID).
		Updates(updates).Error
}

// ResetAllSteps 重置所有步驟狀態（開始新的完整同步時）
func (s *syncStatusService) ResetAllSteps(accountID uint) error {
	// 確保記錄存在
	if _, err := s.GetOrCreate(accountID); err != nil {
		return err
	}

	return s.db.Model(&model.WhatsAppSyncStatus{}).
		Where("account_id = ?", accountID).
		Updates(map[string]interface{}{
			"connect_status":       model.SyncStatePending,
			"chat_sync_status":     model.SyncStatePending,
			"history_sync_status":  model.SyncStatePending,
			"contact_sync_status":  model.SyncStatePending,
			"connect_error":        "",
			"chat_sync_error":      "",
			"history_sync_error":   "",
			"contact_sync_error":   "",
			"history_sync_progress": "",
		}).Error
}

// UpdateHistoryProgress 更新歷史同步進度
func (s *syncStatusService) UpdateHistoryProgress(accountID uint, current, total int) error {
	progress := fmt.Sprintf("%d/%d", current, total)
	return s.db.Model(&model.WhatsAppSyncStatus{}).
		Where("account_id = ?", accountID).
		Update("history_sync_progress", progress).Error
}

// stepFieldMapping 步驟欄位映射
type stepFieldMapping struct {
	status      string
	startedAt   string
	completedAt string
	error       string
}

// stepFieldMappings 步驟到欄位名稱的映射表
var stepFieldMappings = map[model.SyncStepType]stepFieldMapping{
	model.SyncStepConnect: {"connect_status", "connect_started_at", "connect_completed_at", "connect_error"},
	model.SyncStepChat:    {"chat_sync_status", "chat_sync_started_at", "chat_sync_completed_at", "chat_sync_error"},
	model.SyncStepHistory: {"history_sync_status", "history_sync_started_at", "history_sync_completed_at", "history_sync_error"},
	model.SyncStepContact: {"contact_sync_status", "contact_sync_started_at", "contact_sync_completed_at", "contact_sync_error"},
}

// buildStatusUpdates 建立狀態更新的 map
func (s *syncStatusService) buildStatusUpdates(step model.SyncStepType, state model.SyncStepState, errMsg string, extra map[string]interface{}) map[string]interface{} {
	updates := make(map[string]interface{})
	updates[s.getStatusField(step)] = state

	if errMsg != "" {
		updates[s.getErrorField(step)] = errMsg
	}

	// 合併額外欄位
	for k, v := range extra {
		updates[k] = v
	}

	return updates
}

func (s *syncStatusService) getStatusField(step model.SyncStepType) string {
	if m, ok := stepFieldMappings[step]; ok {
		return m.status
	}
	return ""
}

func (s *syncStatusService) getStartedAtField(step model.SyncStepType) string {
	if m, ok := stepFieldMappings[step]; ok {
		return m.startedAt
	}
	return ""
}

func (s *syncStatusService) getCompletedAtField(step model.SyncStepType) string {
	if m, ok := stepFieldMappings[step]; ok {
		return m.completedAt
	}
	return ""
}

func (s *syncStatusService) getErrorField(step model.SyncStepType) string {
	if m, ok := stepFieldMappings[step]; ok {
		return m.error
	}
	return ""
}
