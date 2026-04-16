package model

import (
	"time"
)

// ReferralRegistration 裂变注册记录表
type ReferralRegistration struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	ReferralCode      string    `gorm:"size:12;not null;index" json:"referral_code"`
	SourceAccountID   uint      `gorm:"not null;index" json:"source_account_id"`
	NewAccountID      uint      `gorm:"uniqueIndex;not null" json:"new_account_id"`
	OperatorAdminID   *uint     `gorm:"index" json:"operator_admin_id,omitempty"`
	SourceAgentID     *uint     `gorm:"index" json:"source_agent_id,omitempty"`
	PromotionDomainID *uint     `gorm:"index" json:"promotion_domain_id,omitempty"`
	RegisteredAt      time.Time `gorm:"index" json:"registered_at"`
	Metadata          JSONB     `gorm:"type:jsonb;default:'{}'" json:"metadata"`
}

// TableName 设置表名
func (ReferralRegistration) TableName() string {
	return "referral_registrations"
}
