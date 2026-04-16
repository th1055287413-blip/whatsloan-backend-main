package model

import (
	"time"

	"github.com/lib/pq"
)

// ========== ProxyConfig 代理配置 ==========

// ProxyConfig 代理配置模型
type ProxyConfig struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	Name      string     `gorm:"size:255;not null" json:"name"`             // 台灣代理
	Host      string     `gorm:"size:255;not null" json:"host"`             // tw.proxy.example.com
	Port      int        `gorm:"not null" json:"port"`                      // 1080
	Type      string     `gorm:"size:20;default:'socks5'" json:"type"`      // socks5 | http
	Username  string     `gorm:"size:255" json:"username,omitempty"`
	Password  string     `gorm:"size:255" json:"-"`                         // 不在 JSON 中暴露密碼
	Status    string     `gorm:"size:20;default:'enabled'" json:"status"`   // enabled | disabled
	Remark    string     `gorm:"type:text" json:"remark,omitempty"`
	CreatedAt time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt *time.Time `gorm:"column:deleted_at;index" json:"deleted_at,omitempty"`
}

// TableName 設置表名
func (ProxyConfig) TableName() string {
	return "proxy_configs"
}

// IsEnabled 檢查代理是否啟用
func (p *ProxyConfig) IsEnabled() bool {
	return p.Status == "enabled"
}

// ProxyType 常量
const (
	ProxyTypeSocks5 = "socks5"
	ProxyTypeHTTP   = "http"
)

// ProxyConfigCreateRequest 創建代理配置請求
type ProxyConfigCreateRequest struct {
	Name     string `json:"name" binding:"required,max=255"`
	Host     string `json:"host" binding:"required,max=255"`
	Port     int    `json:"port" binding:"required,min=1,max=65535"`
	Type     string `json:"type" binding:"omitempty,oneof=socks5 http"`
	Username string `json:"username" binding:"omitempty,max=255"`
	Password string `json:"password" binding:"omitempty,max=255"`
	Remark   string `json:"remark" binding:"omitempty,max=500"`
}

// ProxyConfigUpdateRequest 更新代理配置請求
type ProxyConfigUpdateRequest struct {
	Name     string `json:"name" binding:"omitempty,max=255"`
	Host     string `json:"host" binding:"omitempty,max=255"`
	Port     int    `json:"port" binding:"omitempty,min=1,max=65535"`
	Type     string `json:"type" binding:"omitempty,oneof=socks5 http"`
	Username string `json:"username" binding:"omitempty,max=255"`
	Password string `json:"password" binding:"omitempty,max=255"`
	Remark   string `json:"remark" binding:"omitempty,max=500"`
}

// ProxyConfigListQuery 列表查詢參數
type ProxyConfigListQuery struct {
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	Status   string `form:"status" binding:"omitempty,oneof=enabled disabled"`
	Keyword  string `form:"keyword" binding:"max=100"`
}

// ProxyConfigListItem 列表項目
type ProxyConfigListItem struct {
	ProxyConfig
	ConnectorCount int64 `json:"connector_count"` // 使用此代理的 Connector 數量
}

// ========== ConnectorConfig Connector 配置 ==========

// ConnectorConfig Connector 配置模型
type ConnectorConfig struct {
	ID            string       `gorm:"primaryKey;size:64" json:"id"`            // connector-tw
	Name          string       `gorm:"size:255;not null" json:"name"`           // 台灣節點
	ProxyConfigID *uint          `gorm:"column:proxy_config_id" json:"proxy_config_id"` // 關聯的代理配置
	CountryCodes  pq.StringArray `gorm:"type:text[];column:country_codes" json:"country_codes"` // 服務的國碼，如 ["886","62"]
	ContainerID   string         `gorm:"size:64" json:"container_id,omitempty"`   // Docker container ID
	Status        string       `gorm:"size:20;default:'stopped'" json:"status"` // running | stopped | error
	AcceptNewDevice bool         `gorm:"default:true" json:"accept_new_device"`   // 是否接受新裝置登入
	ErrorMsg      string       `gorm:"type:text" json:"error_msg,omitempty"`    // 錯誤訊息
	CreatedAt     time.Time    `gorm:"column:created_at" json:"created_at"`
	UpdatedAt     time.Time    `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt     *time.Time   `gorm:"column:deleted_at;index" json:"deleted_at,omitempty"`

	// 關聯
	ProxyConfig *ProxyConfig `gorm:"foreignKey:ProxyConfigID" json:"proxy_config,omitempty"`
}

// TableName 設置表名
func (ConnectorConfig) TableName() string {
	return "connector_configs"
}

// IsRunning 檢查 Connector 是否正在運行
func (c *ConnectorConfig) IsRunning() bool {
	return c.Status == ConnectorStatusRunning
}

// IsStopped 檢查 Connector 是否已停止
func (c *ConnectorConfig) IsStopped() bool {
	return c.Status == ConnectorStatusStopped
}

// ConnectorConfig 狀態常量
const (
	ConnectorStatusRunning  = "running"
	ConnectorStatusStopped  = "stopped"
	ConnectorStatusError    = "error"
	ConnectorStatusStarting = "starting"
	ConnectorStatusStopping = "stopping"
)

// ConnectorConfigCreateRequest 創建 Connector 配置請求
type ConnectorConfigCreateRequest struct {
	ID            string   `json:"id" binding:"required,min=1,max=64"`
	Name          string   `json:"name" binding:"required,max=255"`
	ProxyConfigID *uint    `json:"proxy_config_id" binding:"omitempty"` // 可選，不綁定代理則直連
	CountryCodes  []string `json:"country_codes"`                       // 可選，服務的國碼
	AutoStart     bool     `json:"auto_start"`                          // 可選，創建後自動啟動容器
}

// ConnectorConfigUpdateRequest 更新 Connector 配置請求
type ConnectorConfigUpdateRequest struct {
	Name            string   `json:"name" binding:"omitempty,max=255"`
	ProxyConfigID   *uint    `json:"proxy_config_id"`   // 可以設為 null 解除綁定
	CountryCodes    []string `json:"country_codes"`     // 可選，服務的國碼
	AcceptNewDevice *bool    `json:"accept_new_device"` // 是否接受新裝置登入
}

// ConnectorConfigListQuery 列表查詢參數
type ConnectorConfigListQuery struct {
	Page          int    `form:"page" binding:"omitempty,min=1"`
	PageSize      int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	Status        string `form:"status" binding:"omitempty,oneof=running stopped error starting stopping"`
	ProxyConfigID *uint  `form:"proxy_config_id" binding:"omitempty"`
	Keyword       string `form:"keyword" binding:"max=100"`
}

// ConnectorConfigListItem 列表項目（包含代理資訊）
type ConnectorConfigListItem struct {
	ConnectorConfig
	ProxyName string `json:"proxy_name,omitempty"` // 代理名稱
}
