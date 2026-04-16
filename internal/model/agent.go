package model

import (
	"time"

	"gorm.io/gorm"
)

// Agent 業務員（外部用戶）
type Agent struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	Username    string         `gorm:"size:50;not null;uniqueIndex:idx_agent_wg_username" json:"username"`
	Password    string         `gorm:"size:255;not null" json:"-"`
	WorkgroupID uint           `gorm:"not null;index;uniqueIndex:idx_agent_wg_username" json:"workgroup_id"`
	Role        string         `gorm:"size:20;not null" json:"role"` // leader, member
	Status      string         `gorm:"size:20;default:'active'" json:"status"` // active, inactive
	ReadOnly    bool           `gorm:"default:false" json:"read_only"`
	LastLoginAt *time.Time     `json:"last_login_at"`
	LastLoginIP string         `gorm:"size:50" json:"last_login_ip"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Agent) TableName() string {
	return "agents"
}

// IsLeader 是否為組長
func (a *Agent) IsLeader() bool {
	return a.Role == "leader"
}

// IsActive 是否為啟用狀態
func (a *Agent) IsActive() bool {
	return a.Status == "active"
}

// MaxPinnedChats Agent 釘選聊天數量上限
const MaxPinnedChats = 50

// AgentPinnedChat Agent 釘選的聊天
type AgentPinnedChat struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	AgentID   uint      `gorm:"not null;uniqueIndex:idx_agent_pinned_chat" json:"agent_id"`
	ChatID    uint      `gorm:"not null;uniqueIndex:idx_agent_pinned_chat" json:"chat_id"`
	PinnedAt  time.Time `json:"pinned_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (AgentPinnedChat) TableName() string {
	return "agent_pinned_chats"
}

// AgentSession Agent 登入 session（存 Redis）
type AgentSession struct {
	AgentID     uint   `json:"agent_id"`
	Username    string `json:"username"`
	WorkgroupID uint   `json:"workgroup_id"`
	Role        string `json:"role"`
	LoginIP     string `json:"login_ip"`
	UserAgent   string `json:"user_agent"`
}
