package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	WorkgroupTypeSales     = "sales"
	WorkgroupTypeMarketing = "marketing"
	WorkgroupTypeAdmin     = "admin"

	WorkgroupStatusActive   = "active"
	WorkgroupStatusDisabled = "disabled"
	WorkgroupStatusArchived = "archived"

	WorkgroupCodeAdmin = "admin"
	WorkgroupNameAdmin = "管理員"

	WorkgroupAutoAssignedBy uint = 0
)

// Workgroup 工作組
type Workgroup struct {
	ID                uint           `gorm:"primaryKey" json:"id"`
	Code              string         `gorm:"size:50;uniqueIndex" json:"code"`
	Name              string         `gorm:"size:100;not null" json:"name"`
	Type              string         `gorm:"size:20;not null;default:'sales'" json:"type"` // sales, marketing
	Description       string         `gorm:"type:text" json:"description"`
	Status            string         `gorm:"size:20;default:'active'" json:"status"`                      // active, disabled
	AccountVisibility string         `gorm:"size:20;default:'assigned'" json:"account_visibility"` // assigned: 組員只看分配的, shared: 全組共享
	CreatedBy         uint           `gorm:"not null" json:"created_by"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

// WorkgroupAccount 工作組帳號分配
type WorkgroupAccount struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	WorkgroupID     uint      `gorm:"not null;index" json:"workgroup_id"`
	AccountID       uint      `gorm:"not null" json:"account_id"`
	WorkgroupType   string    `gorm:"size:20;not null;default:''" json:"workgroup_type"` // 冗餘欄位，用於 unique constraint
	AssignedAgentID *uint     `gorm:"index" json:"assigned_agent_id,omitempty"`
	AssignedBy      uint      `gorm:"not null" json:"assigned_by"`
	AssignedAt      time.Time `json:"assigned_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (Workgroup) TableName() string {
	return "workgroups"
}

func (WorkgroupAccount) TableName() string {
	return "workgroup_accounts"
}
