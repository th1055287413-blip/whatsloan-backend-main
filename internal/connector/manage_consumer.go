package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// ManageHandler 管理命令處理器介面（由 Pool 實作）
type ManageHandler interface {
	Start(ctx context.Context, connectorID string) error
	Stop(ctx context.Context, connectorID string) error
	Restart(ctx context.Context, connectorID string) error
}

// ManageConsumer 管理命令消費者，接收 API 服務的 Pool 層級管理命令
type ManageConsumer struct {
	client       *redis.Client
	handler      ManageHandler
	producer     *StreamProducer
	consumerName string
	stopCh       chan struct{}
	wg           sync.WaitGroup
	log          *zap.SugaredLogger
}

// NewManageConsumer 建立管理命令消費者
func NewManageConsumer(client *redis.Client, instanceID string, handler ManageHandler, producer *StreamProducer, log *zap.SugaredLogger) *ManageConsumer {
	return &ManageConsumer{
		client:       client,
		handler:      handler,
		producer:     producer,
		consumerName: fmt.Sprintf("connector-%s", instanceID),
		stopCh:       make(chan struct{}),
		log:          log,
	}
}

// Start 啟動管理命令消費者
func (c *ManageConsumer) Start(ctx context.Context) {
	c.ensureConsumerGroup(ctx)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.consumeLoop(ctx)
	}()
	c.log.Infow("管理命令消費者已啟動", "stream", protocol.ManageCommandStreamName)
}

// Stop 停止管理命令消費者
func (c *ManageConsumer) Stop() {
	close(c.stopCh)
	c.wg.Wait()
	c.log.Infow("管理命令消費者已停止")
}

func (c *ManageConsumer) ensureConsumerGroup(ctx context.Context) {
	err := c.client.XGroupCreateMkStream(ctx, protocol.ManageCommandStreamName, protocol.ManageCommandConsumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		c.log.Warnw("建立管理命令消費者群組失敗", "error", err)
	}
}

func (c *ManageConsumer) consumeLoop(ctx context.Context) {
	c.ackStalePending(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
			c.tryConsume(ctx)
		}
	}
}

func (c *ManageConsumer) ackStalePending(ctx context.Context) {
	pending, err := c.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream:   protocol.ManageCommandStreamName,
		Group:    protocol.ManageCommandConsumerGroup,
		Start:    "-",
		End:      "+",
		Count:    1000,
		Consumer: c.consumerName,
	}).Result()
	if err != nil || len(pending) == 0 {
		return
	}

	ids := make([]string, len(pending))
	for i, p := range pending {
		ids[i] = p.ID
	}
	if err := c.client.XAck(ctx, protocol.ManageCommandStreamName, protocol.ManageCommandConsumerGroup, ids...).Err(); err != nil {
		c.log.Warnw("清除管理命令 pending 訊息失敗", "error", err)
		return
	}
	c.log.Infow("已清除殘留管理命令 pending 訊息", "count", len(ids))
}

func (c *ManageConsumer) tryConsume(ctx context.Context) {
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    protocol.ManageCommandConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{protocol.ManageCommandStreamName, ">"},
		Count:    1,
		Block:    500 * time.Millisecond,
	}).Result()

	if err != nil {
		if err == redis.Nil || ctx.Err() != nil {
			return
		}
		c.log.Errorw("讀取管理命令失敗", "error", err)
		time.Sleep(100 * time.Millisecond)
		return
	}

	for _, s := range streams {
		for _, msg := range s.Messages {
			c.processMessage(ctx, msg)
		}
	}
}

func (c *ManageConsumer) processMessage(ctx context.Context, msg redis.XMessage) {
	defer c.client.XAck(ctx, protocol.ManageCommandStreamName, protocol.ManageCommandConsumerGroup, msg.ID)

	cmdData, ok := msg.Values["command"].(string)
	if !ok {
		c.log.Errorw("無效的管理命令資料格式")
		return
	}

	var cmd protocol.ManageCommand
	if err := json.Unmarshal([]byte(cmdData), &cmd); err != nil {
		c.log.Errorw("解析管理命令失敗", "error", err)
		return
	}

	c.log.Infow("處理管理命令", "type", cmd.Type, "connector_id", cmd.ConnectorID, "cmd_id", cmd.ID)

	var err error
	switch cmd.Type {
	case protocol.ManageStartConnector:
		err = c.handler.Start(ctx, cmd.ConnectorID)
	case protocol.ManageStopConnector:
		err = c.handler.Stop(ctx, cmd.ConnectorID)
	case protocol.ManageRestartConnector:
		err = c.handler.Restart(ctx, cmd.ConnectorID)
	default:
		err = fmt.Errorf("未知的管理命令類型: %s", cmd.Type)
	}

	if err != nil {
		c.log.Errorw("管理命令執行失敗", "type", cmd.Type, "error", err)
		if pubErr := c.producer.PublishManageCommandError(ctx, cmd.ID, err.Error()); pubErr != nil {
			c.log.Errorw("發送管理命令錯誤事件失敗", "error", pubErr)
		}
	} else {
		c.log.Infow("管理命令執行成功", "type", cmd.Type)
		if pubErr := c.producer.PublishManageCommandAck(ctx, cmd.ID); pubErr != nil {
			c.log.Errorw("發送管理命令確認事件失敗", "error", pubErr)
		}
	}
}
