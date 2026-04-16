package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"whatsapp_golang/internal/config"
	"whatsapp_golang/internal/logger"

	"github.com/go-redis/redis/v8"
	"golang.org/x/time/rate"
)

// SyncTaskType 同步任務類型
type SyncTaskType string

const (
	// TaskTypeAccountConnect 帳號連接任務
	TaskTypeAccountConnect SyncTaskType = "account_connect"
	// TaskTypeChatSync 聊天列表同步任務
	TaskTypeChatSync SyncTaskType = "chat_sync"
	// TaskTypeHistorySync 歷史訊息同步任務
	TaskTypeHistorySync SyncTaskType = "history_sync"
	// TaskTypeContactSync 聯絡人同步任務
	TaskTypeContactSync SyncTaskType = "contact_sync"
)

// SyncTaskPriority 任務優先級
type SyncTaskPriority int

const (
	PriorityHigh   SyncTaskPriority = 1 // 高優先級：帳號連接
	PriorityMedium SyncTaskPriority = 2 // 中優先級：聊天列表同步
	PriorityLow    SyncTaskPriority = 3 // 低優先級：歷史同步
)

// SyncTask 同步任務
type SyncTask struct {
	ID         string           `json:"id"`
	Type       SyncTaskType     `json:"type"`
	AccountID  uint             `json:"account_id"`
	ChatJID    string           `json:"chat_jid,omitempty"`
	Priority   SyncTaskPriority `json:"priority"`
	Count      int              `json:"count,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	Attempts   int              `json:"attempts"`
	ChatIndex  int              `json:"chat_index,omitempty"`  // 當前聊天索引（從 0 開始）
	TotalChats int              `json:"total_chats,omitempty"` // 總聊天數
}

const (
	// StreamNameHigh 高優先級 Stream（帳號連接、聊天同步）
	StreamNameHigh = "whatsapp:sync:high"
	// StreamNameLow 低優先級 Stream（歷史訊息同步）
	StreamNameLow = "whatsapp:sync:low"
	// ConsumerGroup 消費者群組名稱
	ConsumerGroup = "sync-consumer-group"
	// ConsumerName 消費者名稱
	ConsumerName = "sync-consumer-1"
	// MaxStreamLen 最大 Stream 長度（約保留最近 10000 筆）
	MaxStreamLen = 10000
	// DisconnectedAccountsKey 已斷線帳號集合的 Redis Key
	DisconnectedAccountsKey = "whatsapp:sync:disconnected_accounts"
)

// SyncTaskHandler 同步任務處理器介面
type SyncTaskHandler interface {
	HandleAccountConnect(ctx context.Context, accountID uint) error
	HandleChatSync(ctx context.Context, accountID uint) error
	HandleHistorySync(ctx context.Context, accountID uint, chatJID string, count int, chatIndex int, totalChats int) error
	HandleContactSync(ctx context.Context, accountID uint) error
}

// SyncQueue Redis Stream 同步隊列
type SyncQueue struct {
	client  *redis.Client
	config  *config.Config
	handler SyncTaskHandler
	limiter *rate.Limiter
	stopCh  chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
}

// SyncQueueConfig 隊列配置
type SyncQueueConfig struct {
	// RateLimit 每秒最大任務數
	RateLimit float64
	// BurstSize 突發容量
	BurstSize int
}

// DefaultSyncQueueConfig 預設配置
var DefaultSyncQueueConfig = SyncQueueConfig{
	RateLimit: 0.2, // 每 5 秒 1 個任務
	BurstSize: 1,
}

// NewSyncQueue 建立同步隊列
func NewSyncQueue(client *redis.Client, cfg *config.Config, handler SyncTaskHandler, queueCfg *SyncQueueConfig) (*SyncQueue, error) {
	if client == nil {
		return nil, fmt.Errorf("Redis client 不能為空")
	}

	if queueCfg == nil {
		queueCfg = &DefaultSyncQueueConfig
	}

	// 建立限速器
	limiter := rate.NewLimiter(rate.Limit(queueCfg.RateLimit), queueCfg.BurstSize)

	sq := &SyncQueue{
		client:  client,
		config:  cfg,
		handler: handler,
		limiter: limiter,
		stopCh:  make(chan struct{}),
	}

	// 確保消費者群組存在
	if err := sq.ensureConsumerGroup(context.Background()); err != nil {
		logger.Warnw("建立消費者群組失敗（可能已存在）", "error", err)
	}

	logger.Infow("Redis Stream 同步隊列已初始化", "rate_limit", queueCfg.RateLimit)

	return sq, nil
}

// ensureConsumerGroup 確保消費者群組存在（兩個 Stream 都要建立）
func (q *SyncQueue) ensureConsumerGroup(ctx context.Context) error {
	// 為高優先級 Stream 建立消費者群組
	err := q.client.XGroupCreateMkStream(ctx, StreamNameHigh, ConsumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}

	// 為低優先級 Stream 建立消費者群組
	err = q.client.XGroupCreateMkStream(ctx, StreamNameLow, ConsumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}

	return nil
}

// getStreamByPriority 根據優先級返回對應的 Stream 名稱
func (q *SyncQueue) getStreamByPriority(priority SyncTaskPriority) string {
	if priority <= PriorityMedium {
		return StreamNameHigh
	}
	return StreamNameLow
}

// EnqueueTask 將任務加入隊列（根據優先級選擇 Stream）
func (q *SyncQueue) EnqueueTask(ctx context.Context, task *SyncTask) error {
	// 設置任務 ID 和時間
	if task.ID == "" {
		task.ID = fmt.Sprintf("%s-%d-%d", task.Type, task.AccountID, time.Now().UnixNano())
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}

	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("序列化任務失敗: %w", err)
	}

	// 根據優先級選擇 Stream
	streamName := q.getStreamByPriority(task.Priority)

	q.mu.Lock()
	defer q.mu.Unlock()

	// 使用 XADD 加入對應的 Stream，並限制 Stream 長度
	_, err = q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamName,
		MaxLen: MaxStreamLen,
		Approx: true, // 使用近似裁剪，效能更好
		Values: map[string]interface{}{
			"task":     string(data),
			"priority": int(task.Priority), // 轉換為 int 以便 Redis 序列化
		},
	}).Result()

	if err != nil {
		logger.Errorw("發送同步任務失敗", "error", err)
		return fmt.Errorf("發送同步任務失敗: %w", err)
	}

	return nil
}

// EnqueueAccountConnect 加入帳號連接任務
func (q *SyncQueue) EnqueueAccountConnect(ctx context.Context, accountID uint) error {
	return q.EnqueueTask(ctx, &SyncTask{
		Type:      TaskTypeAccountConnect,
		AccountID: accountID,
		Priority:  PriorityHigh,
	})
}

// EnqueueChatSync 加入聊天同步任務
func (q *SyncQueue) EnqueueChatSync(ctx context.Context, accountID uint) error {
	return q.EnqueueTask(ctx, &SyncTask{
		Type:      TaskTypeChatSync,
		AccountID: accountID,
		Priority:  PriorityMedium,
	})
}

// EnqueueHistorySync 加入歷史訊息同步任務
func (q *SyncQueue) EnqueueHistorySync(ctx context.Context, accountID uint, chatJID string, count int, chatIndex int, totalChats int) error {
	return q.EnqueueTask(ctx, &SyncTask{
		Type:       TaskTypeHistorySync,
		AccountID:  accountID,
		ChatJID:    chatJID,
		Count:      count,
		Priority:   PriorityLow,
		ChatIndex:  chatIndex,
		TotalChats: totalChats,
	})
}

// EnqueueContactSync 加入聯絡人同步任務
func (q *SyncQueue) EnqueueContactSync(ctx context.Context, accountID uint) error {
	return q.EnqueueTask(ctx, &SyncTask{
		Type:      TaskTypeContactSync,
		AccountID: accountID,
		Priority:  PriorityLow,
	})
}

// Start 啟動消費者
func (q *SyncQueue) Start(ctx context.Context) {
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		q.consumeLoop(ctx)
	}()
	logger.Infow("Redis Stream 同步隊列消費者已啟動")
}

// consumeLoop 消費迴圈（優先級調度：先處理高優先級，再處理低優先級）
// 高優先級任務（AccountConnect、ChatSync）不受限速器約束，立即執行
// 低優先級任務（HistorySync、ContactSync）受限速器控制
func (q *SyncQueue) consumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			logger.Infow("同步隊列消費者收到停止信號")
			return
		case <-q.stopCh:
			logger.Infow("同步隊列消費者已停止")
			return
		default:
			// 高優先級隊列：不等限速器，立即處理
			if q.tryConsumeFromStream(ctx, StreamNameHigh) {
				continue
			}

			// 低優先級隊列：需要等待限速器
			if err := q.limiter.Wait(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}

			if !q.tryConsumeFromStream(ctx, StreamNameLow) {
				// 兩個隊列都沒有任務，短暫等待
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}

// tryConsumeFromStream 嘗試從指定 Stream 消費一個任務
// 返回 true 表示成功處理了一個任務
func (q *SyncQueue) tryConsumeFromStream(ctx context.Context, streamName string) bool {
	streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    ConsumerGroup,
		Consumer: ConsumerName,
		Streams:  []string{streamName, ">"},
		Count:    1,
		Block:    100 * time.Millisecond, // 短暫阻塞，快速切換檢查另一個隊列
	}).Result()

	if err != nil {
		if err == redis.Nil {
			// 沒有新訊息
			return false
		}
		if ctx.Err() != nil {
			return false
		}
		logger.Errorw("讀取同步任務失敗", "stream", streamName, "error", err)
		return false
	}

	// 處理訊息
	for _, stream := range streams {
		for _, msg := range stream.Messages {
			q.processMessage(ctx, streamName, msg)
			return true
		}
	}

	return false
}

// processMessage 處理訊息
func (q *SyncQueue) processMessage(ctx context.Context, streamName string, msg redis.XMessage) {
	taskData, ok := msg.Values["task"].(string)
	if !ok {
		logger.Errorw("無效的任務資料格式")
		q.ackMessage(ctx, streamName, msg.ID)
		return
	}

	var task SyncTask
	if err := json.Unmarshal([]byte(taskData), &task); err != nil {
		logger.Errorw("解析同步任務失敗", "error", err)
		q.ackMessage(ctx, streamName, msg.ID)
		return
	}

	logger.Infow("處理同步任務",
		"type", task.Type, "account_id", task.AccountID,
		"priority", task.Priority, "stream", streamName)

	var err error
	switch task.Type {
	case TaskTypeAccountConnect:
		err = q.handler.HandleAccountConnect(ctx, task.AccountID)
	case TaskTypeChatSync:
		err = q.handler.HandleChatSync(ctx, task.AccountID)
	case TaskTypeHistorySync:
		count := task.Count
		if count == 0 {
			count = 100
		}
		err = q.handler.HandleHistorySync(ctx, task.AccountID, task.ChatJID, count, task.ChatIndex, task.TotalChats)
	case TaskTypeContactSync:
		err = q.handler.HandleContactSync(ctx, task.AccountID)
	default:
		logger.Warnw("未知的同步任務類型", "type", task.Type)
	}

	if err != nil {
		logger.Errorw("同步任務執行失敗",
			"type", task.Type, "account_id", task.AccountID, "error", err)
	} else {
		logger.Infow("同步任務完成",
			"type", task.Type, "account_id", task.AccountID)
	}

	// 確認訊息已處理
	q.ackMessage(ctx, streamName, msg.ID)
}

// ackMessage 確認訊息已處理
func (q *SyncQueue) ackMessage(ctx context.Context, streamName string, msgID string) {
	if err := q.client.XAck(ctx, streamName, ConsumerGroup, msgID).Err(); err != nil {
		logger.Errorw("確認訊息失敗", "error", err)
	}
}

// Stop 停止消費者
func (q *SyncQueue) Stop() {
	close(q.stopCh)
	q.wg.Wait()
	logger.Infow("Redis Stream 同步隊列消費者已關閉")
}

// GetPendingCount 獲取待處理任務數量（兩個 Stream 總和）
func (q *SyncQueue) GetPendingCount(ctx context.Context) (int64, error) {
	high, low, err := q.GetPendingCountByPriority(ctx)
	return high + low, err
}

// GetPendingCountByPriority 獲取各優先級待處理任務數量
func (q *SyncQueue) GetPendingCountByPriority(ctx context.Context) (high int64, low int64, err error) {
	high = q.getStreamPendingCount(ctx, StreamNameHigh)
	low = q.getStreamPendingCount(ctx, StreamNameLow)
	return high, low, nil
}

// getStreamPendingCount 獲取指定 Stream 的待處理任務數量
func (q *SyncQueue) getStreamPendingCount(ctx context.Context, streamName string) int64 {
	info, err := q.client.XInfoGroups(ctx, streamName).Result()
	if err != nil {
		return 0
	}
	for _, group := range info {
		if group.Name == ConsumerGroup {
			return group.Pending
		}
	}
	return 0
}

// GetStreamLength 獲取 Stream 長度（兩個 Stream 總和）
func (q *SyncQueue) GetStreamLength(ctx context.Context) (int64, error) {
	highLen, _ := q.client.XLen(ctx, StreamNameHigh).Result()
	lowLen, _ := q.client.XLen(ctx, StreamNameLow).Result()
	return highLen + lowLen, nil
}

// MarkAccountDisconnected 標記帳號為已斷線
func (q *SyncQueue) MarkAccountDisconnected(ctx context.Context, accountID uint) error {
	return q.client.SAdd(ctx, DisconnectedAccountsKey, accountID).Err()
}

// UnmarkAccountDisconnected 取消帳號的斷線標記
func (q *SyncQueue) UnmarkAccountDisconnected(ctx context.Context, accountID uint) error {
	return q.client.SRem(ctx, DisconnectedAccountsKey, accountID).Err()
}

// IsAccountDisconnected 檢查帳號是否已標記為斷線
func (q *SyncQueue) IsAccountDisconnected(ctx context.Context, accountID uint) bool {
	result, err := q.client.SIsMember(ctx, DisconnectedAccountsKey, accountID).Result()
	if err != nil {
		return false
	}
	return result
}

// ClearAccountTasks 清理指定帳號的所有待處理任務
// 返回清理的任務數量
func (q *SyncQueue) ClearAccountTasks(ctx context.Context, accountID uint) (int64, error) {
	var cleared int64

	// 清理兩個 Stream 中的任務
	for _, streamName := range []string{StreamNameHigh, StreamNameLow} {
		count, err := q.clearAccountTasksFromStream(ctx, streamName, accountID)
		if err != nil {
			logger.Warnw("清理 Stream 中帳號任務失敗",
				"stream", streamName, "account_id", accountID, "error", err)
			continue
		}
		cleared += count
	}

	return cleared, nil
}

// clearAccountTasksFromStream 從指定 Stream 清理帳號的待處理任務
func (q *SyncQueue) clearAccountTasksFromStream(ctx context.Context, streamName string, accountID uint) (int64, error) {
	var cleared int64

	// 獲取待處理訊息列表
	pending, err := q.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: streamName,
		Group:  ConsumerGroup,
		Start:  "-",
		End:    "+",
		Count:  1000, // 一次最多處理 1000 筆
	}).Result()

	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, fmt.Errorf("獲取待處理訊息失敗: %w", err)
	}

	// 遍歷待處理訊息
	for _, p := range pending {
		// 讀取訊息內容
		msgs, err := q.client.XRange(ctx, streamName, p.ID, p.ID).Result()
		if err != nil || len(msgs) == 0 {
			continue
		}

		// 解析任務
		taskData, ok := msgs[0].Values["task"].(string)
		if !ok {
			continue
		}

		var task SyncTask
		if err := json.Unmarshal([]byte(taskData), &task); err != nil {
			continue
		}

		// 檢查是否為目標帳號的任務
		if task.AccountID == accountID {
			// ACK 該訊息（相當於跳過執行）
			if err := q.client.XAck(ctx, streamName, ConsumerGroup, p.ID).Err(); err != nil {
				logger.Warnw("ACK 訊息失敗", "msg_id", p.ID, "error", err)
				continue
			}
			cleared++
		}
	}

	return cleared, nil
}
