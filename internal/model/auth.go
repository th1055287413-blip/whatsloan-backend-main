package model

import (
	"time"
)

// AdminUser 管理员用户模型 - 存储在PostgreSQL中
type AdminUser struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	Username      string     `gorm:"size:50;uniqueIndex;not null" json:"username"`
	Password      string     `gorm:"column:password;size:255;not null" json:"-"` // BCrypt哈希
	Salt          string     `gorm:"size:32;not null" json:"-"`                  // 密码盐值
	Status        string     `gorm:"size:20;default:'active'" json:"status"`     // active, inactive, locked
	LastLoginAt   *time.Time `gorm:"column:last_login_at" json:"last_login_at"`
	LoginAttempts int        `gorm:"column:login_attempts;default:0" json:"-"`
	LockedUntil   *time.Time `gorm:"column:locked_until" json:"-"`
	Avatar        string     `gorm:"size:500" json:"avatar"`
	RealName      string     `gorm:"column:real_name;size:100" json:"real_name"`
	Phone         string     `gorm:"size:20" json:"phone"`
	Department    string     `gorm:"size:100" json:"department"`
	Position      string     `gorm:"size:100" json:"position"`
	ChannelID     *uint      `gorm:"column:channel_id;index" json:"channel_id,omitempty"` // 关联的渠道ID
	CreatedAt     time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt     *time.Time `gorm:"column:deleted_at" json:"deleted_at,omitempty"`

	// 兼容旧代码的字段
	Role        string `gorm:"-" json:"role"` // 临时字段,角色从user_roles表获取
	LastLoginIP string `gorm:"column:last_login_ip" json:"last_login_ip"`
	ChannelName string `gorm:"-" json:"channel_name,omitempty"` // 渠道名称（关联查询后填充）
}

// LoginSession 登录会话模型 - 存储在Redis中
type LoginSession struct {
	UserID    uint      `json:"user_id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	LoginIP   string    `json:"login_ip"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// TableName 设置表名
func (AdminUser) TableName() string {
	return "admin_users"
}

// HasRole 检查用户是否有指定角色
func (u *AdminUser) HasRole(roleName string) bool {
	return u.Role == roleName
}

// IsActive 检查用户是否处于活跃状态
func (u *AdminUser) IsActive() bool {
	return u.Status == "active"
}

// AdminTodayStats 管理员今日统计
type AdminTodayStats struct {
	TodayConversations int64 `json:"today_conversations"` // 今日对话人数
	TodayMessages      int64 `json:"today_messages"`      // 今日发送消息数
}
