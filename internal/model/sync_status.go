package model

import (
	"time"
)

// SyncStepType 同步步驟類型
type SyncStepType string

const (
	SyncStepConnect  SyncStepType = "account_connect"
	SyncStepChat     SyncStepType = "chat_sync"
	SyncStepHistory  SyncStepType = "history_sync"
	SyncStepContact  SyncStepType = "contact_sync"
)

// SyncStepState 步驟狀態
type SyncStepState string

const (
	SyncStatePending   SyncStepState = "pending"   // 等待中
	SyncStateQueued    SyncStepState = "queued"    // 已入隊
	SyncStateRunning   SyncStepState = "running"   // 執行中
	SyncStateCompleted SyncStepState = "completed" // 已完成
	SyncStateFailed    SyncStepState = "failed"    // 失敗
)

// WhatsAppSyncStatus 帳號同步狀態
type WhatsAppSyncStatus struct {
	ID        uint `gorm:"primaryKey" json:"id"`
	AccountID uint `gorm:"uniqueIndex" json:"account_id"`

	// 帳號連接狀態
	ConnectStatus      SyncStepState `gorm:"size:20;default:'pending'" json:"connect_status"`
	ConnectStartedAt   *time.Time    `json:"connect_started_at,omitempty"`
	ConnectCompletedAt *time.Time    `json:"connect_completed_at,omitempty"`
	ConnectError       string        `gorm:"size:500" json:"connect_error,omitempty"`

	// 聊天列表同步狀態
	ChatSyncStatus      SyncStepState `gorm:"size:20;default:'pending'" json:"chat_sync_status"`
	ChatSyncStartedAt   *time.Time    `json:"chat_sync_started_at,omitempty"`
	ChatSyncCompletedAt *time.Time    `json:"chat_sync_completed_at,omitempty"`
	ChatSyncError       string        `gorm:"size:500" json:"chat_sync_error,omitempty"`
	ChatSyncCount       int           `json:"chat_sync_count"` // 同步的對話數量

	// 歷史訊息同步狀態
	HistorySyncStatus      SyncStepState `gorm:"size:20;default:'pending'" json:"history_sync_status"`
	HistorySyncStartedAt   *time.Time    `json:"history_sync_started_at,omitempty"`
	HistorySyncCompletedAt *time.Time    `json:"history_sync_completed_at,omitempty"`
	HistorySyncError       string        `gorm:"size:500" json:"history_sync_error,omitempty"`
	HistorySyncProgress    string        `gorm:"size:50" json:"history_sync_progress,omitempty"` // "3/45"

	// 聯絡人同步狀態
	ContactSyncStatus      SyncStepState `gorm:"size:20;default:'pending'" json:"contact_sync_status"`
	ContactSyncStartedAt   *time.Time    `json:"contact_sync_started_at,omitempty"`
	ContactSyncCompletedAt *time.Time    `json:"contact_sync_completed_at,omitempty"`
	ContactSyncError       string        `gorm:"size:500" json:"contact_sync_error,omitempty"`
	ContactSyncCount       int           `json:"contact_sync_count"` // 同步的聯絡人數量

	// 整體狀態
	LastFullSyncAt *time.Time `json:"last_full_sync_at,omitempty"` // 最後完整同步時間

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 設置表名
func (WhatsAppSyncStatus) TableName() string {
	return "whatsapp_sync_status"
}

// GetOverallStatus 獲取整體同步狀態
func (s *WhatsAppSyncStatus) GetOverallStatus() SyncStepState {
	// 如果有任何步驟失敗，整體為失敗
	if s.ConnectStatus == SyncStateFailed ||
		s.ChatSyncStatus == SyncStateFailed ||
		s.HistorySyncStatus == SyncStateFailed ||
		s.ContactSyncStatus == SyncStateFailed {
		return SyncStateFailed
	}

	// 如果有任何步驟正在執行，整體為執行中
	if s.ConnectStatus == SyncStateRunning ||
		s.ChatSyncStatus == SyncStateRunning ||
		s.HistorySyncStatus == SyncStateRunning ||
		s.ContactSyncStatus == SyncStateRunning {
		return SyncStateRunning
	}

	// 如果有任何步驟在隊列中，整體為排隊中
	if s.ConnectStatus == SyncStateQueued ||
		s.ChatSyncStatus == SyncStateQueued ||
		s.HistorySyncStatus == SyncStateQueued ||
		s.ContactSyncStatus == SyncStateQueued {
		return SyncStateQueued
	}

	// 如果所有步驟都完成，整體為完成
	if s.ConnectStatus == SyncStateCompleted &&
		s.ChatSyncStatus == SyncStateCompleted &&
		s.HistorySyncStatus == SyncStateCompleted &&
		s.ContactSyncStatus == SyncStateCompleted {
		return SyncStateCompleted
	}

	// 否則為等待中
	return SyncStatePending
}

// SyncStatusSummary 同步狀態摘要（用於帳號列表）
type SyncStatusSummary struct {
	OverallStatus  SyncStepState `json:"overall_status"`
	LastFullSyncAt *time.Time    `json:"last_full_sync_at,omitempty"`
	HasError       bool          `json:"has_error"`
}

// GetSummary 獲取同步狀態摘要
func (s *WhatsAppSyncStatus) GetSummary() SyncStatusSummary {
	hasError := s.ConnectError != "" ||
		s.ChatSyncError != "" ||
		s.HistorySyncError != "" ||
		s.ContactSyncError != ""

	return SyncStatusSummary{
		OverallStatus:  s.GetOverallStatus(),
		LastFullSyncAt: s.LastFullSyncAt,
		HasError:       hasError,
	}
}
