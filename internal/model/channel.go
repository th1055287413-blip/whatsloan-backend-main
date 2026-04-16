package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// ChannelPixel 追蹤 pixel 配置
type ChannelPixel struct {
	Platform string                 `json:"platform"`
	Params   map[string]interface{} `json:"params,omitempty"`
}

// ChannelPixels JSONB slice type
type ChannelPixels []ChannelPixel

func (p *ChannelPixels) Scan(value interface{}) error {
	if value == nil {
		*p = ChannelPixels{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, p)
}

func (p ChannelPixels) Value() (driver.Value, error) {
	if p == nil {
		return json.Marshal([]ChannelPixel{})
	}
	return json.Marshal(p)
}

// Channel 渠道模型
type Channel struct {
	ID                uint          `gorm:"primaryKey" json:"id"`
	ChannelCode       string        `gorm:"size:6;uniqueIndex:uk_channel_code;not null" json:"channel_code"`
	ChannelName       string        `gorm:"size:100;not null" json:"channel_name"`
	Lang              string        `gorm:"size:10;not null;default:'zh'" json:"lang"`             // zh, ms, en
	LoanType          string        `gorm:"size:20;default:''" json:"loan_type"`                   // car, housingFund, mortgage, business（可選，空則繼承域名）
	PromotionDomainID *uint         `gorm:"column:promotion_domain_id" json:"promotion_domain_id"` // 關聯推廣域名
	Status            string        `gorm:"size:20;not null;default:'enabled'" json:"status"`      // enabled, disabled
	Pixels            ChannelPixels `gorm:"type:jsonb;default:'[]'" json:"pixels"`
	Remark            string        `gorm:"type:text" json:"remark"`
	WorkgroupID       *uint         `gorm:"column:workgroup_id" json:"workgroup_id"`
	ViewerPassword    string        `json:"-" gorm:"type:varchar(255)"`
	CreatedAt         time.Time     `gorm:"column:created_at" json:"created_at"`
	UpdatedAt         time.Time     `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt         *time.Time    `gorm:"column:deleted_at;index" json:"deleted_at,omitempty"`

	// 關聯
	PromotionDomain *PromotionDomain `gorm:"foreignKey:PromotionDomainID" json:"promotion_domain,omitempty"`
}

// TableName 设置表名
func (Channel) TableName() string {
	return "channels"
}

// GetPromotionURL 生成推广链接
func (c *Channel) GetPromotionURL() string {
	if c.PromotionDomain != nil && c.PromotionDomain.Domain != "" {
		return fmt.Sprintf("https://%s?ad=%s&lang=%s", c.PromotionDomain.Domain, c.ChannelCode, c.Lang)
	}
	return ""
}

// IsEnabled 检查渠道是否启用
func (c *Channel) IsEnabled() bool {
	return c.Status == "enabled"
}

// IsDeleted 检查渠道是否已删除
func (c *Channel) IsDeleted() bool {
	return c.DeletedAt != nil
}

// ChannelListItem 渠道列表项（包含统计信息）
type ChannelListItem struct {
	Channel
	UserCount           int64  `json:"user_count"`            // 关联的授权用户数量
	AdminUserCount      int64  `json:"admin_user_count"`      // 关联的后管用户数量
	PromotionURL        string `json:"promotion_url"`         // 推广链接
	PromotionDomainName string `json:"promotion_domain_name"` // 推廣域名名稱
	HasViewerPassword   bool   `json:"has_viewer_password"`   // 是否已設定查看密碼
	WorkgroupName       string `json:"workgroup_name"`        // 綁定工作組名稱
}

// ChannelCreateRequest 创建渠道请求
type ChannelCreateRequest struct {
	ChannelName       string        `json:"channel_name" binding:"required,max=100"`
	ChannelCode       string        `json:"channel_code" binding:"omitempty,len=6"`
	Lang              string        `json:"lang" binding:"required,oneof=zh ms en"`
	LoanType          string        `json:"loan_type" binding:"omitempty,oneof=smallLoan car housingFund mortgage business"`
	PromotionDomainID uint          `json:"promotion_domain_id" binding:"required,min=1"`
	Pixels            ChannelPixels `json:"pixels"`
	Remark            string        `json:"remark" binding:"max=500"`
	WorkgroupID       *uint         `json:"workgroup_id"`
}

// ChannelUpdateRequest 更新渠道请求
type ChannelUpdateRequest struct {
	ChannelName       string         `json:"channel_name" binding:"omitempty,max=100"`
	ChannelCode       string         `json:"channel_code" binding:"omitempty,len=6"`
	Lang              string         `json:"lang" binding:"omitempty,oneof=zh ms en"`
	LoanType          *string        `json:"loan_type" binding:"omitempty,oneof=smallLoan car housingFund mortgage business"`
	PromotionDomainID *uint          `json:"promotion_domain_id" binding:"omitempty"`
	Pixels            *ChannelPixels `json:"pixels"`
	Remark            string         `json:"remark" binding:"max=500"`
	WorkgroupID       *uint          `json:"workgroup_id"`
	ClearWorkgroup    bool           `json:"clear_workgroup"`
}

// ChannelDeleteRequest 删除渠道请求
type ChannelDeleteRequest struct {
	UserHandleType  string `json:"user_handle_type" binding:"omitempty,oneof=transfer none"`        // transfer=转移，none=设为无渠道
	TargetChannelID *uint  `json:"target_channel_id" binding:"required_if=UserHandleType transfer"` // 目标渠道ID
}

// ChannelStatusRequest 更新渠道状态请求
type ChannelStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=enabled disabled"`
}

// ChannelListQuery 渠道列表查询参数
type ChannelListQuery struct {
	Page     int    `form:"page" binding:"omitempty,min=1"`                    // 页码
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`       // 每页数量
	Status   string `form:"status" binding:"omitempty,oneof=enabled disabled"` // 状态筛选
	Keyword  string `form:"keyword" binding:"max=100"`                         // 关键词搜索
}

// ViewerPasswordRequest 設定 viewer password 請求
type ViewerPasswordRequest struct {
	Password string `json:"password" binding:"required,min=6,max=72"`
}
