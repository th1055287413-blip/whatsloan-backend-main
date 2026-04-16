package analyzer

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	// AnalysisQueueKey Redis 隊列 key
	AnalysisQueueKey = "analysis:queue"
	// MaxQueueSize 隊列最大長度
	MaxQueueSize = 10000
)

// AnalysisQueue 分析隊列
type AnalysisQueue struct {
	redis *redis.Client
}

// NewAnalysisQueue 建立分析隊列
func NewAnalysisQueue(redisClient *redis.Client) *AnalysisQueue {
	return &AnalysisQueue{redis: redisClient}
}

// Enqueue 將任務加入隊列
func (q *AnalysisQueue) Enqueue(ctx context.Context, task *AnalysisTask) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}

	// LPUSH + LTRIM 保持隊列大小
	pipe := q.redis.Pipeline()
	pipe.LPush(ctx, AnalysisQueueKey, data)
	pipe.LTrim(ctx, AnalysisQueueKey, 0, MaxQueueSize-1)
	_, err = pipe.Exec(ctx)
	return err
}

// Dequeue 取出任務（阻塞）
func (q *AnalysisQueue) Dequeue(ctx context.Context, timeout time.Duration) (*AnalysisTask, error) {
	result, err := q.redis.BRPop(ctx, timeout, AnalysisQueueKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // 超時，無任務
		}
		return nil, err
	}

	var task AnalysisTask
	if err := json.Unmarshal([]byte(result[1]), &task); err != nil {
		return nil, err
	}

	return &task, nil
}

// Len 取得隊列長度
func (q *AnalysisQueue) Len(ctx context.Context) (int64, error) {
	return q.redis.LLen(ctx, AnalysisQueueKey).Result()
}

// Clear 清空隊列
func (q *AnalysisQueue) Clear(ctx context.Context) error {
	return q.redis.Del(ctx, AnalysisQueueKey).Err()
}
