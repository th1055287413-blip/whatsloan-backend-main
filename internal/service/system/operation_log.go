package system

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"whatsapp_golang/internal/model"
)

// OperationLogService defines the interface for operation logging
type OperationLogService interface {
	// Log creates a new operation log entry
	Log(entry *model.LogEntry, c *gin.Context) error

	// LogAsync creates a new operation log entry asynchronously
	LogAsync(entry *model.LogEntry, c *gin.Context)

	// GetList retrieves operation logs with pagination and filters
	GetList(filter *model.OperationLogFilter) ([]*model.AdminOperationLog, int64, error)

	// GetByID retrieves a single operation log by ID
	GetByID(id uint) (*model.AdminOperationLog, error)
}

type operationLogService struct {
	db *gorm.DB
}

// NewOperationLogService creates a new operation log service instance
func NewOperationLogService(db *gorm.DB) OperationLogService {
	return &operationLogService{db: db}
}

// Log creates a new operation log entry
func (s *operationLogService) Log(entry *model.LogEntry, c *gin.Context) error {
	log := s.buildLog(entry, c)
	return s.db.Create(log).Error
}

// LogAsync creates a new operation log entry asynchronously
func (s *operationLogService) LogAsync(entry *model.LogEntry, c *gin.Context) {
	log := s.buildLog(entry, c)
	go func() {
		_ = s.db.Create(log).Error
	}()
}

// buildLog converts LogEntry to AdminOperationLog with context information
func (s *operationLogService) buildLog(entry *model.LogEntry, c *gin.Context) *model.AdminOperationLog {
	log := &model.AdminOperationLog{
		OperationType:    entry.OperationType,
		OperatorID:       entry.OperatorID,
		OperatorUsername: entry.OperatorUsername,
		AgentID:          entry.AgentID,
		WorkgroupID:      entry.WorkgroupID,
		AgentName:        entry.AgentName,
		ResourceType:     entry.ResourceType,
		ResourceID:       entry.ResourceID,
		ResourceName:     entry.ResourceName,
		Status:           entry.Status,
		ErrorMessage:     entry.ErrorMessage,
		CreatedAt:        time.Now(),
	}

	// Set default status
	if log.Status == "" {
		log.Status = model.StatusSuccess
	}

	// Convert before/after values to JSONB
	if entry.BeforeValue != nil {
		log.BeforeValue = toJSONB(entry.BeforeValue)
	}
	if entry.AfterValue != nil {
		log.AfterValue = toJSONB(entry.AfterValue)
	}
	if entry.ExtraData != nil {
		log.ExtraData = entry.ExtraData
	}

	// Extract context information if gin.Context is provided
	if c != nil {
		log.IPAddress = c.ClientIP()
		log.UserAgent = c.GetHeader("User-Agent")
		log.RequestPath = c.Request.URL.Path
		log.RequestMethod = c.Request.Method

		// Extract operator info from context if not provided
		if log.OperatorID == nil {
			if userID, exists := c.Get("user_id"); exists {
				if uid, ok := userID.(uint); ok {
					log.OperatorID = &uid
				}
			}
		}
		if log.OperatorUsername == "" {
			if username, exists := c.Get("username"); exists {
				if uname, ok := username.(string); ok {
					log.OperatorUsername = uname
				}
			}
		}

		// Extract agent info from context if not provided
		if log.AgentID == nil {
			if agentID, exists := c.Get("agent_id"); exists {
				if aid, ok := agentID.(uint); ok {
					log.AgentID = &aid
				}
			}
		}
		if log.WorkgroupID == nil {
			if wgID, exists := c.Get("workgroup_id"); exists {
				if wid, ok := wgID.(uint); ok {
					log.WorkgroupID = &wid
				}
			}
		}
		if log.AgentName == "" {
			if agent, exists := c.Get("agent"); exists {
				if a, ok := agent.(*model.Agent); ok {
					log.AgentName = a.Username
				}
			}
		}
	}

	return log
}

// GetList retrieves operation logs with pagination and filters
func (s *operationLogService) GetList(filter *model.OperationLogFilter) ([]*model.AdminOperationLog, int64, error) {
	filter.SetDefaults()

	var logs []*model.AdminOperationLog
	var total int64

	query := s.db.Model(&model.AdminOperationLog{})

	// Apply filters
	if filter.OperationType != "" {
		query = query.Where("operation_type = ?", filter.OperationType)
	}
	if filter.OperatorID != nil {
		query = query.Where("operator_id = ?", *filter.OperatorID)
	}
	if filter.ResourceType != "" {
		query = query.Where("resource_type = ?", filter.ResourceType)
	}
	if filter.ResourceID != "" {
		query = query.Where("resource_id = ?", filter.ResourceID)
	}
	if filter.AgentID != nil {
		query = query.Where("agent_id = ?", *filter.AgentID)
	}
	if filter.WorkgroupID != nil {
		query = query.Where("workgroup_id = ?", *filter.WorkgroupID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.StartTime != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", filter.StartTime); err == nil {
			query = query.Where("created_at >= ?", t)
		} else if t, err := time.Parse("2006-01-02", filter.StartTime); err == nil {
			query = query.Where("created_at >= ?", t)
		}
	}
	if filter.EndTime != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", filter.EndTime); err == nil {
			query = query.Where("created_at <= ?", t)
		} else if t, err := time.Parse("2006-01-02", filter.EndTime); err == nil {
			// Add a day to include the entire end date
			t = t.Add(24*time.Hour - time.Second)
			query = query.Where("created_at <= ?", t)
		}
	}

	// Count total
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Paginate and order
	offset := (filter.Page - 1) * filter.PageSize
	if err := query.Offset(offset).Limit(filter.PageSize).Order("created_at DESC").Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// GetByID retrieves a single operation log by ID
func (s *operationLogService) GetByID(id uint) (*model.AdminOperationLog, error) {
	var log model.AdminOperationLog
	if err := s.db.First(&log, id).Error; err != nil {
		return nil, err
	}
	return &log, nil
}

// toJSONB converts any value to JSONB
func toJSONB(v interface{}) model.JSONB {
	if v == nil {
		return nil
	}

	// If already a map, return directly
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}

	// If already JSONB, return directly
	if j, ok := v.(model.JSONB); ok {
		return j
	}

	// Otherwise, serialize to JSON and back to get a map
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}

	var result model.JSONB
	if err := json.Unmarshal(data, &result); err != nil {
		// If unmarshal to map fails, wrap the value
		return model.JSONB{"value": v}
	}
	return result
}
