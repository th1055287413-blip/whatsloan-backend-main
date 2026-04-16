package model

import "time"

// BatchSendTask 批量发送任务
type BatchSendTask struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	AccountID        uint      `gorm:"index;not null" json:"account_id"`
	MessageContent   string    `gorm:"type:text;not null" json:"message_content"`
	SendInterval     int       `gorm:"default:2" json:"send_interval"`
	TotalCount       int       `gorm:"default:0" json:"total_count"`
	SuccessCount     int       `gorm:"default:0" json:"success_count"`
	FailedCount      int       `gorm:"default:0" json:"failed_count"`
	Status           string    `gorm:"size:20;default:'pending'" json:"status"` // pending, running, paused, completed, failed
	CreatedBy        string    `gorm:"size:100" json:"created_by,omitempty"`
	CreatedByAdminID *uint     `gorm:"index" json:"created_by_admin_id,omitempty"` // 建立任務的管理員 ID
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// BatchSendRecipient 批量发送接收人
type BatchSendRecipient struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	TaskID       uint       `gorm:"index;not null" json:"task_id"`
	ChatJID      string     `gorm:"column:chat_jid;size:100;not null" json:"chat_jid"`
	ChatName     string     `gorm:"size:100" json:"chat_name,omitempty"`
	SendStatus   string     `gorm:"size:20;default:'pending'" json:"send_status"` // pending, sent, failed
	ErrorMessage string     `gorm:"type:text" json:"error_message,omitempty"`
	SentAt       *time.Time `json:"sent_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// TableName 指定表名
func (BatchSendTask) TableName() string {
	return "batch_send_tasks"
}

// TableName 指定表名
func (BatchSendRecipient) TableName() string {
	return "batch_send_recipients"
}
