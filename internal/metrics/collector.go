package metrics

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
)

// BusinessCollector 實作 prometheus.Collector，收集 Redis stream 和 Connector 業務指標
type BusinessCollector struct {
	redis *redis.Client

	streamLength  *prometheus.Desc
	streamPending *prometheus.Desc

	connectorAccountCount *prometheus.Desc
	connectorUptime       *prometheus.Desc
	eventWorkerQueueDepth *prometheus.Desc

	commandStreamLength *prometheus.Desc
}

func NewBusinessCollector(redisClient *redis.Client) *BusinessCollector {
	return &BusinessCollector{
		redis: redisClient,

		streamLength: prometheus.NewDesc(
			"redis_stream_length",
			"Current length of Redis stream",
			[]string{"stream"}, nil,
		),
		streamPending: prometheus.NewDesc(
			"redis_stream_pending",
			"Number of pending messages in consumer group",
			[]string{"stream"}, nil,
		),
		connectorAccountCount: prometheus.NewDesc(
			"connector_account_count",
			"Number of accounts per connector",
			[]string{"connector_id"}, nil,
		),
		connectorUptime: prometheus.NewDesc(
			"connector_uptime_seconds",
			"Connector uptime in seconds",
			[]string{"connector_id"}, nil,
		),
		eventWorkerQueueDepth: prometheus.NewDesc(
			"event_worker_queue_depth",
			"Event worker queue depth per connector",
			[]string{"connector_id"}, nil,
		),
		commandStreamLength: prometheus.NewDesc(
			"command_stream_length",
			"Length of connector command stream",
			[]string{"connector_id", "priority"}, nil,
		),
	}
}

func (c *BusinessCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.streamLength
	ch <- c.streamPending
	ch <- c.connectorAccountCount
	ch <- c.connectorUptime
	ch <- c.eventWorkerQueueDepth
	ch <- c.commandStreamLength
}

func (c *BusinessCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c.collectEventStreams(ctx, ch)
	c.collectConnectors(ctx, ch)
	c.collectCommandStreams(ctx, ch)
}

func (c *BusinessCollector) collectEventStreams(ctx context.Context, ch chan<- prometheus.Metric) {
	streams := []struct {
		name   string
		stream string
		group  string
	}{
		{"priority_events", protocol.PriorityEventStreamName, protocol.PriorityEventConsumerGroup},
		{"message_events", protocol.MessageEventStreamName, protocol.MessageEventConsumerGroup},
		{"events", protocol.EventStreamName, protocol.EventConsumerGroup},
	}

	for _, s := range streams {
		length, err := c.redis.XLen(ctx, s.stream).Result()
		if err == nil {
			ch <- prometheus.MustNewConstMetric(c.streamLength, prometheus.GaugeValue, float64(length), s.name)
		}

		pending, err := c.redis.XPending(ctx, s.stream, s.group).Result()
		if err == nil && pending != nil {
			ch <- prometheus.MustNewConstMetric(c.streamPending, prometheus.GaugeValue, float64(pending.Count), s.name)
		}
	}
}

func (c *BusinessCollector) collectConnectors(ctx context.Context, ch chan<- prometheus.Metric) {
	ids, err := c.redis.SMembers(ctx, protocol.ConnectorsSetKey).Result()
	if err != nil || len(ids) == 0 {
		return
	}

	for _, id := range ids {
		info, err := c.redis.HGetAll(ctx, protocol.GetConnectorInfoKey(id)).Result()
		if err != nil || len(info) == 0 {
			continue
		}

		if v, ok := info["account_count"]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				ch <- prometheus.MustNewConstMetric(c.connectorAccountCount, prometheus.GaugeValue, float64(n), id)
			}
		}
		if v, ok := info["start_time"]; ok {
			if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
				uptime := time.Since(time.Unix(ts, 0)).Seconds()
				ch <- prometheus.MustNewConstMetric(c.connectorUptime, prometheus.GaugeValue, uptime, id)
			}
		}
		if v, ok := info["event_worker_stats"]; ok {
			var stats map[uint]int
			if json.Unmarshal([]byte(v), &stats) == nil {
				totalQueued := 0
				for _, q := range stats {
					totalQueued += q
				}
				ch <- prometheus.MustNewConstMetric(c.eventWorkerQueueDepth, prometheus.GaugeValue, float64(totalQueued), id)
			}
		}
	}
}

func (c *BusinessCollector) collectCommandStreams(ctx context.Context, ch chan<- prometheus.Metric) {
	ids, err := c.redis.SMembers(ctx, protocol.ConnectorsSetKey).Result()
	if err != nil || len(ids) == 0 {
		return
	}

	for _, id := range ids {
		pStream := protocol.GetPriorityCommandStreamName(id)
		bStream := protocol.GetBulkCommandStreamName(id)

		if pLen, err := c.redis.XLen(ctx, pStream).Result(); err == nil {
			ch <- prometheus.MustNewConstMetric(c.commandStreamLength, prometheus.GaugeValue, float64(pLen), id, "priority")
		}
		if bLen, err := c.redis.XLen(ctx, bStream).Result(); err == nil {
			ch <- prometheus.MustNewConstMetric(c.commandStreamLength, prometheus.GaugeValue, float64(bLen), id, "bulk")
		}
	}
}
