package model

import "time"

// ReferralDailyStat 裂变统计汇总表（每日聚合）
type ReferralDailyStat struct {
	StatDate          time.Time `gorm:"primaryKey" json:"stat_date"`
	AccountID         uint      `gorm:"primaryKey;index" json:"account_id"`
	ReferralCode      string    `gorm:"size:12;not null" json:"referral_code"`
	PromotionDomainID *uint     `json:"promotion_domain_id,omitempty"`
	RegistrationCount int       `gorm:"default:0" json:"registration_count"`
}

// TableName 设置表名
func (ReferralDailyStat) TableName() string {
	return "referral_daily_stats"
}
