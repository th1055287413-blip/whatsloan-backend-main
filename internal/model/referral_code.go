package model

import "time"

// ReferralCode 推荐码配置表
type ReferralCode struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	AccountID         uint      `gorm:"uniqueIndex;not null" json:"account_id"`
	ReferralCode      string    `gorm:"uniqueIndex;size:12;not null" json:"referral_code"`
	PromotionDomainID *uint     `json:"promotion_domain_id,omitempty"`
	LandingPath       string    `gorm:"size:255;default:'/'" json:"landing_path"`
	ShareURL          string    `gorm:"type:text;not null" json:"share_url"`
	QRCodeURL         *string   `gorm:"type:text" json:"qr_code_url,omitempty"`
	CreatedByAdminID  *uint     `json:"created_by_admin_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// TableName 设置表名
func (ReferralCode) TableName() string {
	return "referral_codes"
}
