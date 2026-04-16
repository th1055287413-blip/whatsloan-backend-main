package model

import (
	"time"
)

// PromotionDomain 推廣域名模型
type PromotionDomain struct {
	ID        uint          `gorm:"primaryKey" json:"id"`
	Name      string        `gorm:"size:100;not null" json:"name"`
	Domain    string        `gorm:"size:255;uniqueIndex:uk_domain;not null" json:"domain"`
	Status    string        `gorm:"size:20;not null;default:'enabled'" json:"status"` // enabled, disabled
	Pixels    ChannelPixels `gorm:"type:jsonb;default:'[]'" json:"pixels"`
	Remark    string        `gorm:"type:text" json:"remark"`
	CreatedAt time.Time     `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time     `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt *time.Time    `gorm:"column:deleted_at;index" json:"deleted_at,omitempty"`
}

// TableName 設置表名
func (PromotionDomain) TableName() string {
	return "promotion_domains"
}

// IsEnabled 檢查域名是否啟用
func (p *PromotionDomain) IsEnabled() bool {
	return p.Status == "enabled"
}

// PromotionDomainListItem 域名列表項（包含統計信息）
type PromotionDomainListItem struct {
	PromotionDomain
	ChannelCount int64 `json:"channel_count"` // 關聯的渠道數量
}

type PromotionDomainCreateRequest struct {
	Name   string        `json:"name" binding:"required,max=100"`
	Domain string        `json:"domain" binding:"required,max=255"`
	Pixels ChannelPixels `json:"pixels"`
	Remark string        `json:"remark" binding:"max=500"`
}

type PromotionDomainUpdateRequest struct {
	Name   string         `json:"name" binding:"omitempty,max=100"`
	Domain string         `json:"domain" binding:"omitempty,max=255"`
	Pixels *ChannelPixels `json:"pixels"`
	Remark string         `json:"remark" binding:"max=500"`
}

// PromotionDomainStatusRequest 更新域名狀態請求
type PromotionDomainStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=enabled disabled"`
}

// PromotionDomainListQuery 域名列表查詢參數
type PromotionDomainListQuery struct {
	Page     int    `form:"page" binding:"omitempty,min=1"`                    // 頁碼
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`       // 每頁數量
	Status   string `form:"status" binding:"omitempty,oneof=enabled disabled"` // 狀態篩選
	Keyword  string `form:"keyword" binding:"max=100"`                         // 關鍵詞搜索
}

// PromotionDomainSimple 簡化的域名信息（用於下拉選擇）
type PromotionDomainSimple struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
}
