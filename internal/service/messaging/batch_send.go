package messaging

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"gorm.io/gorm"
	"whatsapp_golang/internal/gateway"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	"whatsapp_golang/internal/service/whatsapp"
)

// WebSocketBroadcaster WebSocket 广播接口
type WebSocketBroadcaster interface {
	BroadcastBatchSendProgress(accountID uint, data interface{})
}

// BatchSendService 批量发送服务接口
type BatchSendService interface {
	CreateBatchTask(accountID uint, messageContent string, sendInterval int, recipients []model.BatchSendRecipient, createdBy string, adminID *uint) (*model.BatchSendTask, error)
	ExecuteBatchSend(taskID uint) error
	GetTaskByID(taskID uint) (*model.BatchSendTask, error)
	ListTasks(page, pageSize int) ([]model.BatchSendTask, int64, error)
	GetTaskRecipients(taskID uint) ([]model.BatchSendRecipient, error)
	GetPendingRecipients(taskID uint) ([]model.BatchSendRecipient, error)
	UpdateTask(task *model.BatchSendTask) error
	UpdateRecipient(recipient *model.BatchSendRecipient) error
	DeleteTask(taskID uint) error
	PauseTask(taskID uint) error
	ResumeTask(taskID uint) error
}

// batchSendServiceImpl 批量发送服务实现
type batchSendServiceImpl struct {
	db                    *gorm.DB
	dataService           whatsapp.DataService
	gateway               *gateway.Gateway
	websocket             WebSocketBroadcaster
	runningTasks          sync.Map // 用于存储正在运行的任务goroutine
	messageSendingService MessageSendingService
}

// NewBatchSendService 创建批量发送服务
func NewBatchSendService(db *gorm.DB, dataService whatsapp.DataService, gw *gateway.Gateway, websocket WebSocketBroadcaster) *batchSendServiceImpl {
	return &batchSendServiceImpl{
		db:          db,
		dataService: dataService,
		gateway:     gw,
		websocket:   websocket,
	}
}

// SetMessageSendingService 設置訊息發送服務
func (s *batchSendServiceImpl) SetMessageSendingService(svc MessageSendingService) {
	s.messageSendingService = svc
}

// CreateBatchTask 创建批量发送任务
func (s *batchSendServiceImpl) CreateBatchTask(
	accountID uint,
	messageContent string,
	sendInterval int,
	recipients []model.BatchSendRecipient,
	createdBy string,
	adminID *uint,
) (*model.BatchSendTask, error) {
	// 1. 验证账号
	account, err := s.dataService.GetAccount(accountID)
	if err != nil {
		return nil, errors.New("账号不存在")
	}

	// 检查账号连接状态（透過 Gateway）
	if s.gateway == nil {
		return nil, errors.New("Gateway 未初始化")
	}
	ctx := context.Background()
	if !s.gateway.IsAccountConnected(ctx, accountID) {
		return nil, errors.New("账号未连接")
	}

	// 2. 验证接收人数量
	if len(recipients) == 0 {
		return nil, errors.New("接收人列表不能为空")
	}

	// 3. 验证消息内容
	if messageContent == "" {
		return nil, errors.New("消息内容不能为空")
	}

	// 4. 验证发送间隔
	if sendInterval < 1 || sendInterval > 10 {
		sendInterval = 2 // 默认2秒
	}

	// 5. 创建任务 - 使用事务保证数据一致性
	task := &model.BatchSendTask{
		AccountID:        accountID,
		MessageContent:   messageContent,
		SendInterval:     sendInterval,
		TotalCount:       len(recipients),
		Status:           "pending",
		CreatedBy:        createdBy,
		CreatedByAdminID: adminID,
	}

	// 使用事务创建任务和接收人记录
	err = s.db.Transaction(func(tx *gorm.DB) error {
		// 创建任务
		if err := tx.Create(task).Error; err != nil {
			logger.Errorw("創建批量發送任務失敗", "error", err, "account_id", account.ID)
			return errors.New("创建任务失败")
		}

		// 创建接收人记录
		for i := range recipients {
			recipients[i].TaskID = task.ID
		}

		if err := tx.Create(&recipients).Error; err != nil {
			logger.Errorw("創建接收人記錄失敗", "error", err, "task_id", task.ID)
			return errors.New("创建接收人记录失败")
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	logger.Infow("批量發送任務創建成功",
		"task_id", task.ID,
		"account_id", accountID,
		"recipient_count", len(recipients),
		"created_by", createdBy,
	)

	return task, nil
}

// ExecuteBatchSend 执行批量发送
func (s *batchSendServiceImpl) ExecuteBatchSend(taskID uint) error {
	// 1. 获取任务详情
	task, err := s.GetTaskByID(taskID)
	if err != nil {
		return err
	}

	// 2. 验证任务状态
	if task.Status != "pending" && task.Status != "paused" {
		return fmt.Errorf("任务状态不允许执行: %s", task.Status)
	}

	// 3. 验证账号连接状态（透過 Gateway）
	if s.gateway == nil {
		return errors.New("Gateway 未初始化")
	}
	ctx := context.Background()
	if !s.gateway.IsAccountConnected(ctx, task.AccountID) {
		return errors.New("账号未连接")
	}

	// 4. 获取待发送的接收人列表
	recipients, err := s.GetPendingRecipients(taskID)
	if err != nil {
		return err
	}

	if len(recipients) == 0 {
		// 如果没有待发送的消息,标记为完成
		task.Status = "completed"
		s.UpdateTask(task)
		return nil
	}

	// 5. 更新任务状态为 running
	task.Status = "running"
	if err := s.UpdateTask(task); err != nil {
		return err
	}

	// 6. 启动 goroutine 异步发送
	go s.executeSendLoop(task, recipients)

	logger.Infow("批量發送任務開始執行", "task_id", taskID, "pending_count", len(recipients))
	return nil
}

// executeSendLoop 执行发送循环
func (s *batchSendServiceImpl) executeSendLoop(task *model.BatchSendTask, recipients []model.BatchSendRecipient) {
	// 保存原始taskID,避免在recover中访问可能为nil的task
	taskID := task.ID

	defer func() {
		if r := recover(); r != nil {
			// 获取stack trace
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			stackTrace := string(buf[:n])

			logger.Errorw("批量發送任務異常",
				"task_id", taskID,
				"panic", r,
				"stack", stackTrace,
			)

			// 安全地更新任务状态
			if task != nil {
				task.Status = "failed"
				s.UpdateTask(task)
			} else {
				// 如果task为nil,直接更新数据库
				s.db.Model(&model.BatchSendTask{}).Where("id = ?", taskID).
					Update("status", "failed")
			}
		}
		// 从运行中任务列表移除
		s.runningTasks.Delete(taskID)
	}()

	// 添加到运行中任务列表
	s.runningTasks.Store(task.ID, true)

	// 修复循环变量指针问题 - 使用索引遍历
	for i := range recipients {
		recipient := recipients[i] // 创建副本,避免指针问题

		// 检查任务是否被暂停
		currentTask, taskErr := s.GetTaskByID(task.ID)
		if taskErr != nil {
			logger.Errorw("取得任務狀態失敗", "task_id", task.ID, "error", taskErr)
			break
		}
		if currentTask.Status == "paused" {
			logger.Infow("批量發送任務已暫停", "task_id", task.ID)
			break
		}

		logger.Infow("發送訊息",
			"task_id", task.ID,
			"recipient_index", i+1,
			"total_recipients", len(recipients),
			"chat_jid", recipient.ChatJID,
			"chat_name", recipient.ChatName,
		)

		// 发送消息（透過統一訊息發送服務，記錄管理員 ID）
		ctx := context.Background()
		var err error
		if s.messageSendingService != nil {
			err = s.messageSendingService.SendTextMessage(ctx, task.AccountID, recipient.ChatJID, task.MessageContent, task.CreatedByAdminID)
		} else if s.gateway != nil {
			err = s.gateway.SendMessageAsAdmin(ctx, task.AccountID, recipient.ChatJID, task.MessageContent, task.CreatedByAdminID)
		} else {
			err = fmt.Errorf("no message sending service available")
		}

		// 更新接收人发送状态
		now := time.Now()
		if err != nil {
			recipient.SendStatus = "failed"
			recipient.ErrorMessage = err.Error()
			logger.Errorw("訊息發送失敗",
				"task_id", task.ID,
				"chat_jid", recipient.ChatJID,
				"error", err,
			)
			// 使用数据库原子操作更新计数器 - 修复并发竞态条件
			s.db.Model(&model.BatchSendTask{}).Where("id = ?", task.ID).
				Update("failed_count", gorm.Expr("failed_count + ?", 1))
		} else {
			recipient.SendStatus = "sent"
			recipient.SentAt = &now
			logger.Infow("訊息發送成功",
				"task_id", task.ID,
				"chat_jid", recipient.ChatJID,
			)
			// 使用数据库原子操作更新计数器 - 修复并发竞态条件
			s.db.Model(&model.BatchSendTask{}).Where("id = ?", task.ID).
				Update("success_count", gorm.Expr("success_count + ?", 1))
		}

		// 更新接收人状态
		s.UpdateRecipient(&recipient)

		// 重新获取任务以获取最新的计数器值
		updatedTask, err := s.GetTaskByID(task.ID)
		if err != nil {
			logger.Errorw("重新取得任務失敗", "task_id", task.ID, "error", err)
		} else {
			task = updatedTask
		}

		// WebSocket 推送进度
		if s.websocket != nil {
			logger.Debugw("準備廣播進度", "task_id", task.ID, "recipient", recipient.ChatName)
			s.broadcastProgress(task, &recipient)
			logger.Debugw("廣播進度成功", "task_id", task.ID)
		}

		// 延迟等待(避免频率限制)
		if i < len(recipients)-1 { // 最后一条不需要等待
			time.Sleep(time.Duration(task.SendInterval) * time.Second)
		}
	}

	// 7. 更新任务状态为 completed
	currentTask, err := s.GetTaskByID(task.ID)
	if err != nil {
		logger.Errorw("取得任務失敗", "task_id", task.ID, "error", err)
		return
	}

	if currentTask.Status != "paused" {
		currentTask.Status = "completed"
		s.UpdateTask(currentTask)
		logger.Infow("批量發送任務完成",
			"task_id", currentTask.ID,
			"success_count", currentTask.SuccessCount,
			"failed_count", currentTask.FailedCount,
		)

		// 发送完成通知
		if s.websocket != nil {
			s.broadcastProgress(currentTask, nil)
		}
	}
}

// broadcastProgress 广播发送进度
func (s *batchSendServiceImpl) broadcastProgress(task *model.BatchSendTask, currentRecipient *model.BatchSendRecipient) {
	// 添加nil检查
	if task == nil {
		logger.Errorw("broadcastProgress: task is nil")
		return
	}

	data := map[string]interface{}{
		"task_id":       task.ID,
		"status":        task.Status,
		"total_count":   task.TotalCount,
		"success_count": task.SuccessCount,
		"failed_count":  task.FailedCount,
	}

	if currentRecipient != nil {
		data["current_recipient"] = currentRecipient.ChatName
		data["current_jid"] = currentRecipient.ChatJID
	}

	// 通过 WebSocket 广播进度
	if s.websocket != nil {
		s.websocket.BroadcastBatchSendProgress(task.AccountID, data)
	}
}

// GetTaskByID 获取任务详情
func (s *batchSendServiceImpl) GetTaskByID(taskID uint) (*model.BatchSendTask, error) {
	var task model.BatchSendTask
	if err := s.db.First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("任务不存在")
		}
		return nil, err
	}
	return &task, nil
}

// ListTasks 查询任务列表
func (s *batchSendServiceImpl) ListTasks(page, pageSize int) ([]model.BatchSendTask, int64, error) {
	var tasks []model.BatchSendTask
	var total int64

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}

	offset := (page - 1) * pageSize

	if err := s.db.Model(&model.BatchSendTask{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := s.db.Order("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&tasks).Error; err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

// GetTaskRecipients 获取任务接收人列表
func (s *batchSendServiceImpl) GetTaskRecipients(taskID uint) ([]model.BatchSendRecipient, error) {
	var recipients []model.BatchSendRecipient
	if err := s.db.Where("task_id = ?", taskID).Order("id ASC").Find(&recipients).Error; err != nil {
		return nil, err
	}
	return recipients, nil
}

// GetPendingRecipients 获取待发送的接收人列表
func (s *batchSendServiceImpl) GetPendingRecipients(taskID uint) ([]model.BatchSendRecipient, error) {
	var recipients []model.BatchSendRecipient
	if err := s.db.Where("task_id = ? AND send_status = ?", taskID, "pending").
		Order("id ASC").
		Find(&recipients).Error; err != nil {
		return nil, err
	}
	return recipients, nil
}

// UpdateTask 更新任务
func (s *batchSendServiceImpl) UpdateTask(task *model.BatchSendTask) error {
	task.UpdatedAt = time.Now()
	return s.db.Save(task).Error
}

// UpdateRecipient 更新接收人
func (s *batchSendServiceImpl) UpdateRecipient(recipient *model.BatchSendRecipient) error {
	return s.db.Save(recipient).Error
}

// DeleteTask 删除任务
func (s *batchSendServiceImpl) DeleteTask(taskID uint) error {
	// 检查任务是否正在运行
	if _, running := s.runningTasks.Load(taskID); running {
		return errors.New("任务正在运行中,无法删除")
	}

	return s.db.Delete(&model.BatchSendTask{}, taskID).Error
}

// PauseTask 暂停任务
func (s *batchSendServiceImpl) PauseTask(taskID uint) error {
	task, err := s.GetTaskByID(taskID)
	if err != nil {
		return err
	}

	if task.Status != "running" {
		return errors.New("只能暂停运行中的任务")
	}

	task.Status = "paused"
	logger.Infow("暫停批量發送任務", "task_id", taskID)
	return s.UpdateTask(task)
}

// ResumeTask 恢复任务
func (s *batchSendServiceImpl) ResumeTask(taskID uint) error {
	task, err := s.GetTaskByID(taskID)
	if err != nil {
		return err
	}

	if task.Status != "paused" {
		return errors.New("只能恢复已暂停的任务")
	}

	// 调用 ExecuteBatchSend 继续发送
	logger.Infow("恢復批量發送任務", "task_id", taskID)
	return s.ExecuteBatchSend(taskID)
}
