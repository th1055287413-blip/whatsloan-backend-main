package gateway_test

import (
	"context"
	"testing"

	"whatsapp_golang/internal/gateway"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	logger.InitSimple("debug")
}

// TestIntegration_RoutingTable 測試路由表操作
func TestIntegration_RoutingTable(t *testing.T) {
	if testing.Short() {
		t.Skip("跳過整合測試")
	}

	ctx := context.Background()

	// 建立 Redis 連線
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	// 測試 Redis 連線
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis 未啟動，跳過整合測試: %v", err)
	}

	// 清理測試資料
	cleanupTestData(ctx, redisClient)
	defer cleanupTestData(ctx, redisClient)

	routing := gateway.NewRoutingService(redisClient, nil)

	t.Run("AssignAndGet", func(t *testing.T) {
		err := routing.AssignAccountToConnector(ctx, 1, "connector-a")
		require.NoError(t, err)

		connectorID, err := routing.GetConnectorForAccount(ctx, 1)
		require.NoError(t, err)
		assert.Equal(t, "connector-a", connectorID)
	})

	t.Run("Remove", func(t *testing.T) {
		err := routing.AssignAccountToConnector(ctx, 2, "connector-b")
		require.NoError(t, err)

		err = routing.RemoveAccountRouting(ctx, 2)
		require.NoError(t, err)

		_, err = routing.GetConnectorForAccount(ctx, 2)
		assert.Error(t, err)
	})

	t.Run("GetAccountsForConnector", func(t *testing.T) {
		err := routing.AssignAccountToConnector(ctx, 10, "connector-x")
		require.NoError(t, err)
		err = routing.AssignAccountToConnector(ctx, 11, "connector-x")
		require.NoError(t, err)
		err = routing.AssignAccountToConnector(ctx, 12, "connector-y")
		require.NoError(t, err)

		accounts, err := routing.GetAccountsForConnector(ctx, "connector-x")
		require.NoError(t, err)
		assert.Len(t, accounts, 2)
		assert.Contains(t, accounts, uint(10))
		assert.Contains(t, accounts, uint(11))
	})
}

// testEventHandler 測試用事件處理器
type testEventHandler struct {
	events chan *protocol.Event
}

func (h *testEventHandler) OnMessageReceived(ctx context.Context, event *protocol.Event, payload *protocol.MessageReceivedPayload) error {
	h.events <- event
	return nil
}

func (h *testEventHandler) OnMessageSent(ctx context.Context, event *protocol.Event, payload *protocol.MessageSentPayload) error {
	h.events <- event
	return nil
}

func (h *testEventHandler) OnReceipt(ctx context.Context, event *protocol.Event, payload *protocol.ReceiptPayload) error {
	h.events <- event
	return nil
}

func (h *testEventHandler) OnConnected(ctx context.Context, event *protocol.Event, payload *protocol.ConnectedPayload) error {
	h.events <- event
	return nil
}

func (h *testEventHandler) OnDisconnected(ctx context.Context, event *protocol.Event, payload *protocol.DisconnectedPayload) error {
	h.events <- event
	return nil
}

func (h *testEventHandler) OnLoggedOut(ctx context.Context, event *protocol.Event, payload *protocol.LoggedOutPayload) error {
	h.events <- event
	return nil
}

func (h *testEventHandler) OnQRCode(ctx context.Context, event *protocol.Event, payload *protocol.QRCodePayload) error {
	h.events <- event
	return nil
}

func (h *testEventHandler) OnPairingCode(ctx context.Context, event *protocol.Event, payload *protocol.PairingCodePayload) error {
	h.events <- event
	return nil
}

func (h *testEventHandler) OnSyncProgress(ctx context.Context, event *protocol.Event, payload *protocol.SyncProgressPayload) error {
	h.events <- event
	return nil
}

func (h *testEventHandler) OnSyncComplete(ctx context.Context, event *protocol.Event, payload *protocol.SyncCompletePayload) error {
	h.events <- event
	return nil
}

// cleanupTestData 清理測試資料
func cleanupTestData(ctx context.Context, client *redis.Client) {
	// 清理路由表
	client.Del(ctx, protocol.RoutingHashKey)

	// 清理 Connector 集合
	client.Del(ctx, protocol.ConnectorsSetKey)

	// 清理事件 Stream
	client.Del(ctx, protocol.EventStreamName)

	// 清理命令 Stream（舊 + 新）
	client.Del(ctx, protocol.GetCommandStreamName("test-connector-1"))
	client.Del(ctx, protocol.GetCommandStreamName("test-connector-2"))
	client.Del(ctx, protocol.GetPriorityCommandStreamName("test-connector-1"))
	client.Del(ctx, protocol.GetPriorityCommandStreamName("test-connector-2"))
	client.Del(ctx, protocol.GetBulkCommandStreamName("test-connector-1"))
	client.Del(ctx, protocol.GetBulkCommandStreamName("test-connector-2"))

	// 清理心跳 Key
	client.Del(ctx, protocol.GetConnectorHeartbeatKey("test-connector-1"))
	client.Del(ctx, protocol.GetConnectorHeartbeatKey("test-connector-2"))
}
