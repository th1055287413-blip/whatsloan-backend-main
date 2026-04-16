package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// Operation type constants
const (
	OpLogin          = "login"
	OpLogout         = "logout"
	OpLoginFailed    = "login_failed"
	OpCreate         = "create"
	OpUpdate         = "update"
	OpDelete         = "delete"
	OpPermissionDeny = "permission_deny"
	OpStatusChange   = "status_change"
	OpPasswordReset  = "password_reset"
	OpConfigChange   = "config_change"
	OpArchive        = "archive"
	OpUnarchive      = "unarchive"
	OpSend           = "send"
	OpRevoke         = "revoke"
	OpExecute        = "execute"
	OpPause          = "pause"
	OpResume         = "resume"
)

// Resource type constants
const (
	ResAdminUser       = "admin_user"
	ResRole            = "role"
	ResPermission      = "permission"
	ResChannel         = "channel"
	ResPromotionDomain = "promotion_domain"
	ResSensitiveWord   = "sensitive_word"
	ResTag             = "tag"
	ResConfig          = "config"
	ResSession         = "session"
	ResBatchSend       = "batch_send"
	ResChat            = "chat"
	ResMessage         = "message"
	ResConnector       = "connector"
	ResContact         = "contact"
	ResAccount         = "account"
	ResWorkgroup       = "workgroup"
	ResWorkgroupAcct   = "workgroup_account"
	ResAgent           = "agent"
)

// Status constants
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
)

// JSONB is a custom type for handling PostgreSQL JSONB columns
type JSONB map[string]interface{}

// Value implements driver.Valuer for JSONB
func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements sql.Scanner for JSONB
func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to scan JSONB: type assertion to []byte failed")
	}
	return json.Unmarshal(bytes, j)
}

// AdminOperationLog represents an operation log entry
type AdminOperationLog struct {
	ID uint `json:"id" gorm:"primaryKey"`

	// Operation identification
	OperationType string `json:"operation_type" gorm:"size:50;not null;index:idx_op_logs_type"`

	// Operator info
	OperatorID       *uint  `json:"operator_id" gorm:"index:idx_op_logs_operator;index:idx_op_logs_filter"`
	OperatorUsername string `json:"operator_username" gorm:"size:50"`

	// Agent info (nullable, only set for agent operations)
	AgentID     *uint  `json:"agent_id" gorm:"index:idx_op_logs_agent;index:idx_op_logs_wg_agent"`
	WorkgroupID *uint  `json:"workgroup_id" gorm:"index:idx_op_logs_wg_agent"`
	AgentName   string `json:"agent_name" gorm:"size:50"`

	// Resource info
	ResourceType string `json:"resource_type" gorm:"size:50;not null;index:idx_op_logs_resource;index:idx_op_logs_filter"`
	ResourceID   string `json:"resource_id" gorm:"size:100;index:idx_op_logs_resource"`
	ResourceName string `json:"resource_name" gorm:"size:255"`

	// Change tracking
	BeforeValue JSONB `json:"before_value" gorm:"type:jsonb"`
	AfterValue  JSONB `json:"after_value" gorm:"type:jsonb"`

	// Request context
	IPAddress     string `json:"ip_address" gorm:"size:45"`
	UserAgent     string `json:"user_agent" gorm:"type:text"`
	RequestPath   string `json:"request_path" gorm:"size:500"`
	RequestMethod string `json:"request_method" gorm:"size:10"`

	// Status
	Status       string `json:"status" gorm:"size:20;default:success"`
	ErrorMessage string `json:"error_message" gorm:"type:text"`
	ExtraData    JSONB  `json:"extra_data" gorm:"type:jsonb"`

	CreatedAt time.Time `json:"created_at" gorm:"index:idx_op_logs_created;index:idx_op_logs_filter"`
}

// TableName specifies the table name for GORM
func (AdminOperationLog) TableName() string {
	return "admin_operation_logs"
}

// LogEntry is a simplified struct for creating log entries
type LogEntry struct {
	OperationType    string
	OperatorID       *uint
	OperatorUsername string
	AgentID          *uint
	WorkgroupID      *uint
	AgentName        string
	ResourceType     string
	ResourceID       string
	ResourceName     string
	BeforeValue      interface{}
	AfterValue       interface{}
	Status           string
	ErrorMessage     string
	ExtraData        map[string]interface{}
}

// OperationLogFilter contains filter options for querying logs
type OperationLogFilter struct {
	Page          int    `form:"page" binding:"min=1"`
	PageSize      int    `form:"page_size" binding:"min=1,max=100"`
	OperationType string `form:"operation_type"`
	OperatorID    *uint  `form:"operator_id"`
	ResourceType  string `form:"resource_type"`
	ResourceID    string `form:"resource_id"`
	AgentID       *uint  `form:"agent_id"`
	WorkgroupID   *uint  `form:"workgroup_id"`
	Status        string `form:"status"`
	StartTime     string `form:"start_time"`
	EndTime       string `form:"end_time"`
}

// SetDefaults sets default values for pagination
func (f *OperationLogFilter) SetDefaults() {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 {
		f.PageSize = 20
	}
	if f.PageSize > 100 {
		f.PageSize = 100
	}
}
