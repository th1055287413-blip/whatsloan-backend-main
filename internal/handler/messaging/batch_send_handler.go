package messaging

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	messagingSvc "whatsapp_golang/internal/service/messaging"
	systemSvc "whatsapp_golang/internal/service/system"
)

// BatchSendHandler 批量發送處理器
type BatchSendHandler struct {
	batchSendService messagingSvc.BatchSendService
	opLogService     systemSvc.OperationLogService
}

// NewBatchSendHandler 創建批量發送處理器
func NewBatchSendHandler(bs messagingSvc.BatchSendService, opLogService systemSvc.OperationLogService) *BatchSendHandler {
	return &BatchSendHandler{
		batchSendService: bs,
		opLogService:     opLogService,
	}
}

// CreateTaskRequest 創建任務請求
type CreateTaskRequest struct {
	AccountID      uint               `json:"account_id" binding:"required"`
	MessageContent string             `json:"message_content" binding:"required"`
	SendInterval   int                `json:"send_interval"`
	Recipients     []RecipientRequest `json:"recipients" binding:"required,min=1"`
}

// RecipientRequest 接收人請求
type RecipientRequest struct {
	ChatJID  string `json:"chat_jid" binding:"required"`
	ChatName string `json:"chat_name"`
}

// CreateTask 創建批量發送任務
func (h *BatchSendHandler) CreateTask(c *gin.Context) {
	var req CreateTaskRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	// 設置預設發送間隔
	if req.SendInterval == 0 {
		req.SendInterval = 2
	}

	// 驗證發送間隔範圍
	if req.SendInterval < 1 || req.SendInterval > 10 {
		common.Error(c, common.CodeInvalidParams, "發送間隔必須在 1-10 秒之間")
		return
	}

	// 轉換接收人列表
	recipients := make([]model.BatchSendRecipient, len(req.Recipients))
	for i, r := range req.Recipients {
		recipients[i] = model.BatchSendRecipient{
			ChatJID:  r.ChatJID,
			ChatName: r.ChatName,
		}
	}

	// 從上下文獲取當前用戶
	username, _ := c.Get("username")
	usernameStr, _ := username.(string)

	// 獲取管理員 ID
	var adminID *uint
	if userInterface, exists := c.Get("user"); exists {
		if user, ok := userInterface.(*model.AdminUser); ok {
			adminID = &user.ID
		}
	}

	task, err := h.batchSendService.CreateBatchTask(
		req.AccountID,
		req.MessageContent,
		req.SendInterval,
		recipients,
		usernameStr,
		adminID,
	)

	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("創建批量發送任務失敗", "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log create operation
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpCreate,
		ResourceType:  model.ResBatchSend,
		ResourceID:    fmt.Sprint(task.ID),
		AfterValue: map[string]interface{}{
			"account_id":       req.AccountID,
			"message_content":  req.MessageContent,
			"recipients_count": len(req.Recipients),
			"send_interval":    req.SendInterval,
		},
	}, c)

	logger.Ctx(c.Request.Context()).Infow("批量發送任務創建成功", "task_id", task.ID, "account_id", req.AccountID)
	common.SuccessWithMessage(c, "任務創建成功", task)
}

// ExecuteTask 執行批量發送任務
func (h *BatchSendHandler) ExecuteTask(c *gin.Context) {
	taskID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.batchSendService.ExecuteBatchSend(taskID); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("執行批量發送任務失敗", "task_id", taskID, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log execute operation
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpStatusChange,
		ResourceType:  model.ResBatchSend,
		ResourceID:    fmt.Sprint(taskID),
		AfterValue: map[string]interface{}{
			"status": "running",
		},
	}, c)

	logger.Ctx(c.Request.Context()).Infow("批量發送任務開始執行", "task_id", taskID)
	common.SuccessWithMessage(c, "任務已開始執行", gin.H{
		"task_id": taskID,
		"status":  "running",
	})
}

// GetTaskList 獲取任務列表
func (h *BatchSendHandler) GetTaskList(c *gin.Context) {
	params := common.ParsePaginationParams(c)

	tasks, total, err := h.batchSendService.ListTasks(params.Page, params.PageSize)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得任務列表失敗", "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.PaginatedList(c, tasks, total, params.Page, params.PageSize)
}

// GetTaskDetail 獲取任務詳情
func (h *BatchSendHandler) GetTaskDetail(c *gin.Context) {
	taskID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	task, err := h.batchSendService.GetTaskByID(taskID)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得任務詳情失敗", "task_id", taskID, "error", err)
		common.HandleNotFoundError(c, "任務")
		return
	}

	recipients, err := h.batchSendService.GetTaskRecipients(taskID)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得接收人列表失敗", "task_id", taskID, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, gin.H{
		"task":       task,
		"recipients": recipients,
	})
}

// DeleteTask 刪除任務
func (h *BatchSendHandler) DeleteTask(c *gin.Context) {
	taskID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.batchSendService.DeleteTask(taskID); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("刪除任務失敗", "task_id", taskID, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log delete operation
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpDelete,
		ResourceType:  model.ResBatchSend,
		ResourceID:    fmt.Sprint(taskID),
	}, c)

	logger.Ctx(c.Request.Context()).Infow("批量發送任務刪除成功", "task_id", taskID)
	common.SuccessWithMessage(c, "任務刪除成功", nil)
}

// PauseTask 暫停任務
func (h *BatchSendHandler) PauseTask(c *gin.Context) {
	taskID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.batchSendService.PauseTask(taskID); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("暫停任務失敗", "task_id", taskID, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log pause operation
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpStatusChange,
		ResourceType:  model.ResBatchSend,
		ResourceID:    fmt.Sprint(taskID),
		AfterValue: map[string]interface{}{
			"status": "paused",
		},
	}, c)

	logger.Ctx(c.Request.Context()).Infow("批量發送任務已暫停", "task_id", taskID)
	common.SuccessWithMessage(c, "任務已暫停", nil)
}

// ResumeTask 恢復任務
func (h *BatchSendHandler) ResumeTask(c *gin.Context) {
	taskID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.batchSendService.ResumeTask(taskID); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("恢復任務失敗", "task_id", taskID, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log resume operation
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpStatusChange,
		ResourceType:  model.ResBatchSend,
		ResourceID:    fmt.Sprint(taskID),
		AfterValue: map[string]interface{}{
			"status": "resumed",
		},
	}, c)

	logger.Ctx(c.Request.Context()).Infow("批量發送任務已恢復", "task_id", taskID)
	common.SuccessWithMessage(c, "任務已恢復", nil)
}
