package system

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/protocol"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// MonitorHandler 系統監控處理器，統整 Redis Stream、Event Worker、Connector 狀態
type MonitorHandler struct {
	redis *redis.Client
}

// NewMonitorHandler 建立監控處理器
func NewMonitorHandler(redisClient *redis.Client) *MonitorHandler {
	return &MonitorHandler{redis: redisClient}
}

// --- Response Types ---

type MonitorResponse struct {
	EventStreams   map[string]*StreamInfo              `json:"event_streams"`
	CommandStreams map[string]*ConnectorCommandStreams  `json:"command_streams"`
	Connectors    map[string]*ConnectorMonitor         `json:"connectors"`
}

type StreamInfo struct {
	Stream   string         `json:"stream"`
	Length   int64          `json:"length"`
	Pending  *PendingInfo   `json:"pending,omitempty"`
}

type PendingInfo struct {
	Count     int64  `json:"count"`
	MinID     string `json:"min_id,omitempty"`
	MaxID     string `json:"max_id,omitempty"`
}

type ConnectorCommandStreams struct {
	Priority *StreamInfo `json:"priority"`
	Bulk     *StreamInfo `json:"bulk"`
}

type ConnectorMonitor struct {
	AccountCount       int                    `json:"account_count"`
	UptimeSeconds      int64                  `json:"uptime_seconds"`
	EventWorkerSummary *EventWorkerSummary    `json:"event_worker_summary"`
	EventWorkerQueues  map[uint]int           `json:"event_worker_queues,omitempty"`
}

type EventWorkerSummary struct {
	Total       int `json:"total"`
	MaxQueue    int `json:"max_queue"`
	TotalQueued int `json:"total_queued"`
}

// GetMonitor 取得系統監控資訊
func (h *MonitorHandler) GetMonitor(c *gin.Context) {
	ctx := c.Request.Context()

	resp := &MonitorResponse{
		EventStreams:   h.collectEventStreams(ctx),
		CommandStreams: h.collectCommandStreams(ctx),
		Connectors:    h.collectConnectors(ctx),
	}

	common.Success(c, resp)
}

// collectEventStreams 收集 3 條事件 stream 的狀態
func (h *MonitorHandler) collectEventStreams(ctx context.Context) map[string]*StreamInfo {
	streams := map[string]struct {
		stream string
		group  string
	}{
		"priority_events": {protocol.PriorityEventStreamName, protocol.PriorityEventConsumerGroup},
		"message_events":  {protocol.MessageEventStreamName, protocol.MessageEventConsumerGroup},
		"events":          {protocol.EventStreamName, protocol.EventConsumerGroup},
	}

	result := make(map[string]*StreamInfo, len(streams))
	for key, s := range streams {
		info := &StreamInfo{Stream: s.stream}
		info.Length, _ = h.redis.XLen(ctx, s.stream).Result()
		info.Pending = h.getPending(ctx, s.stream, s.group)
		result[key] = info
	}
	return result
}

// collectCommandStreams 收集每個 Connector 的命令 stream 狀態
func (h *MonitorHandler) collectCommandStreams(ctx context.Context) map[string]*ConnectorCommandStreams {
	ids, err := h.redis.SMembers(ctx, protocol.ConnectorsSetKey).Result()
	if err != nil || len(ids) == 0 {
		return nil
	}

	result := make(map[string]*ConnectorCommandStreams, len(ids))

	for _, id := range ids {
		pStream := protocol.GetPriorityCommandStreamName(id)
		bStream := protocol.GetBulkCommandStreamName(id)

		pLen, _ := h.redis.XLen(ctx, pStream).Result()
		bLen, _ := h.redis.XLen(ctx, bStream).Result()

		result[id] = &ConnectorCommandStreams{
			Priority: &StreamInfo{Stream: pStream, Length: pLen},
			Bulk:     &StreamInfo{Stream: bStream, Length: bLen},
		}
	}
	return result
}

// collectConnectors 收集每個 Connector 的帳號 + event worker 狀態（從 Redis 心跳讀取）
func (h *MonitorHandler) collectConnectors(ctx context.Context) map[string]*ConnectorMonitor {
	ids, err := h.redis.SMembers(ctx, protocol.ConnectorsSetKey).Result()
	if err != nil || len(ids) == 0 {
		return nil
	}

	result := make(map[string]*ConnectorMonitor, len(ids))
	for _, id := range ids {
		info, err := h.redis.HGetAll(ctx, protocol.GetConnectorInfoKey(id)).Result()
		if err != nil || len(info) == 0 {
			continue
		}

		mon := &ConnectorMonitor{}
		if v, ok := info["account_count"]; ok {
			mon.AccountCount, _ = strconv.Atoi(v)
		}
		if v, ok := info["start_time"]; ok {
			if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
				mon.UptimeSeconds = int64(time.Since(time.Unix(ts, 0)).Seconds())
			}
		}
		if v, ok := info["event_worker_stats"]; ok {
			var queues map[uint]int
			if json.Unmarshal([]byte(v), &queues) == nil {
				mon.EventWorkerQueues = queues
				summary := &EventWorkerSummary{Total: len(queues)}
				for _, q := range queues {
					summary.TotalQueued += q
					if q > summary.MaxQueue {
						summary.MaxQueue = q
					}
				}
				mon.EventWorkerSummary = summary
			}
		}
		result[id] = mon
	}
	return result
}

// getPending 取得 consumer group 的 pending 資訊
func (h *MonitorHandler) getPending(ctx context.Context, stream, group string) *PendingInfo {
	pending, err := h.redis.XPending(ctx, stream, group).Result()
	if err != nil || pending.Count == 0 {
		return nil
	}
	return &PendingInfo{
		Count: pending.Count,
		MinID: pending.Lower,
		MaxID: pending.Higher,
	}
}
