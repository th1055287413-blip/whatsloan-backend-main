// Mock Connector - 模擬 Connector 產生大量事件來測試 API
//
// 使用方式:
//
//	go run ./cmd/mock-connector [flags]
//
// 範例:
//
//	# 使用資料庫中前 5 個帳號，產生 1000 個聊天室同步事件
//	go run ./cmd/mock-connector --accounts 5 --chats 1000 --messages 0
//
//	# 使用指定帳號 ID（逗號分隔）
//	go run ./cmd/mock-connector --account-ids 1,2,3,4,5 --chats 100
//
//	# 產生大量訊息
//	go run ./cmd/mock-connector --accounts 3 --chats 50 --messages 5000 --message-rate 200
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"whatsapp_golang/internal/connector"
	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	// 連線參數（預設從環境變數/.env 讀取）
	redisAddr = flag.String("redis", "", "Redis 地址 (預設從 REDIS_HOST:REDIS_PORT 讀取)")
	redisPass = flag.String("redis-pass", "", "Redis 密碼 (預設從 REDIS_PASSWORD 讀取)")
	redisDB   = flag.Int("redis-db", -1, "Redis DB (預設從 REDIS_DB 讀取)")
	dbDSN     = flag.String("db", "", "資料庫 DSN (預設從 DB_* 環境變數讀取)")

	// 模擬參數
	connectorID     = flag.String("connector-id", "", "Connector ID (預設自動生成)")
	accountCount    = flag.Int("accounts", 5, "使用前 N 個帳號")
	accountIDs      = flag.String("account-ids", "", "指定帳號 ID（逗號分隔，優先於 --accounts）")
	chatsPerAccount = flag.Int("chats", 100, "每個帳號的聊天室數量")
	messageCount    = flag.Int("messages", 500, "總訊息數量")
	messageRate = flag.Int("message-rate", 50, "每秒訊息數")

	// 行為控制
	skipConnect  = flag.Bool("skip-connect", false, "跳過連線事件")
	skipChats    = flag.Bool("skip-chats", false, "跳過聊天室同步")
	skipMessages = flag.Bool("skip-messages", false, "跳過訊息產生")
	keepHeartbeat = flag.Bool("keep-heartbeat", false, "持續發送心跳（不自動結束）")
	verbose       = flag.Bool("verbose", false, "顯示詳細輸出")
)

// 全域變數
var (
	db            *gorm.DB
	rdb           *redis.Client
	producer      *connector.StreamProducer
	testAccounts  []uint
	mockConnector string
)

func main() {
	// 載入 .env 檔案
	godotenv.Load()

	flag.Parse()

	// 初始化連線參數
	initConfig()

	fmt.Printf("=== Mock Connector 啟動 ===\n")
	fmt.Printf("Connector ID: %s\n", mockConnector)
	fmt.Printf("Redis: %s (DB %d)\n", *redisAddr, *redisDB)
	fmt.Printf("模擬參數:\n")
	fmt.Printf("  - 每帳號聊天室: %d\n", *chatsPerAccount)
	fmt.Printf("  - 總訊息數: %d (速率: %d/s)\n", *messageCount, *messageRate)
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 連接資料庫
	if err := connectDB(); err != nil {
		fmt.Printf("資料庫連線失敗: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("資料庫連線成功")

	// 連接 Redis
	if err := connectRedis(ctx); err != nil {
		fmt.Printf("Redis 連線失敗: %v\n", err)
		os.Exit(1)
	}
	defer rdb.Close()
	fmt.Println("Redis 連線成功")

	// 建立 producer
	producer = connector.NewStreamProducer(rdb, mockConnector)

	// 取得測試帳號
	if err := loadTestAccounts(); err != nil {
		fmt.Printf("載入測試帳號失敗: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("測試帳號: %v (共 %d 個)\n", testAccounts, len(testAccounts))

	// 註冊 connector 並分配帳號
	if err := registerConnectorAndAccounts(ctx); err != nil {
		fmt.Printf("註冊 Connector 失敗: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Connector 已註冊，帳號已分配")

	// 啟動心跳
	var wg sync.WaitGroup
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	wg.Add(1)
	go func() {
		defer wg.Done()
		runHeartbeat(heartbeatCtx)
	}()

	// 處理中斷信號
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n收到中斷信號，正在停止...")
		cancel()
	}()

	// 執行模擬
	stats := runSimulation(ctx)

	// 列印統計
	fmt.Println()
	fmt.Println("=== 模擬完成 ===")
	fmt.Printf("已發送事件:\n")
	fmt.Printf("  - 連線事件: %d\n", stats.connected)
	fmt.Printf("  - 聊天室同步: %d 個帳號, 共 %d 個 chat\n", stats.chatsSyncAccounts, stats.chatsTotal)
	fmt.Printf("  - 訊息: %d\n", stats.messages)
	fmt.Printf("  - 錯誤: %d\n", stats.errors)

	if *keepHeartbeat {
		fmt.Println("\n持續發送心跳中... (Ctrl+C 結束)")
		<-ctx.Done()
	}

	// 清理
	heartbeatCancel()
	wg.Wait()
	cleanup(context.Background())
	fmt.Println("Connector 已登出，帳號路由已清除")
}

func initConfig() {
	// Redis 設定
	if *redisAddr == "" {
		host := getEnv("REDIS_HOST", "localhost")
		port := getEnv("REDIS_PORT", "6379")
		*redisAddr = fmt.Sprintf("%s:%s", host, port)
	}
	if *redisPass == "" {
		*redisPass = os.Getenv("REDIS_PASSWORD")
	}
	if *redisDB < 0 {
		if dbStr := os.Getenv("REDIS_DB"); dbStr != "" {
			if db, err := strconv.Atoi(dbStr); err == nil {
				*redisDB = db
			}
		}
		if *redisDB < 0 {
			*redisDB = 0
		}
	}

	// 資料庫 DSN
	if *dbDSN == "" {
		host := getEnv("DB_HOST", "localhost")
		port := getEnv("DB_PORT", "5432")
		user := getEnv("DB_USER", "postgres")
		pass := os.Getenv("DB_PASSWORD")
		name := getEnv("DB_NAME", "whatsapp")
		*dbDSN = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			host, port, user, pass, name)
	}

	// Connector ID
	if *connectorID == "" {
		mockConnector = fmt.Sprintf("mock-%s", uuid.New().String()[:8])
	} else {
		mockConnector = *connectorID
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func connectDB() error {
	var err error
	db, err = gorm.Open(postgres.Open(*dbDSN), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	return err
}

func connectRedis(ctx context.Context) error {
	rdb = redis.NewClient(&redis.Options{
		Addr:     *redisAddr,
		Password: *redisPass,
		DB:       *redisDB,
	})
	return rdb.Ping(ctx).Err()
}

func loadTestAccounts() error {
	// 優先使用指定的帳號 ID
	if *accountIDs != "" {
		ids := strings.Split(*accountIDs, ",")
		for _, idStr := range ids {
			idStr = strings.TrimSpace(idStr)
			if id, err := strconv.ParseUint(idStr, 10, 32); err == nil {
				testAccounts = append(testAccounts, uint(id))
			}
		}
		if len(testAccounts) == 0 {
			return fmt.Errorf("無效的帳號 ID: %s", *accountIDs)
		}
		return nil
	}

	// 從資料庫載入前 N 個帳號
	var accounts []struct {
		ID uint
	}
	if err := db.Table("whatsapp_accounts").
		Select("id").
		Order("id ASC").
		Limit(*accountCount).
		Scan(&accounts).Error; err != nil {
		return err
	}

	if len(accounts) == 0 {
		return fmt.Errorf("資料庫中沒有帳號，請先創建帳號或使用 --account-ids 指定")
	}

	for _, acc := range accounts {
		testAccounts = append(testAccounts, acc.ID)
	}

	return nil
}

func registerConnectorAndAccounts(ctx context.Context) error {
	// 加入 connector 集合
	if err := rdb.SAdd(ctx, protocol.ConnectorsSetKey, mockConnector).Err(); err != nil {
		return err
	}

	// 設定心跳
	heartbeatKey := protocol.GetConnectorHeartbeatKey(mockConnector)
	if err := rdb.Set(ctx, heartbeatKey, time.Now().Unix(), 30*time.Second).Err(); err != nil {
		return err
	}

	// 更新帳號的 connector_id 到資料庫
	for _, accountID := range testAccounts {
		if err := db.Table("whatsapp_accounts").
			Where("id = ?", accountID).
			Update("connector_id", mockConnector).Error; err != nil {
			return fmt.Errorf("更新帳號 %d 的 connector_id 失敗: %w", accountID, err)
		}
	}

	return nil
}

func cleanup(ctx context.Context) {
	// 清除帳號的 connector_id
	for _, accountID := range testAccounts {
		db.Table("whatsapp_accounts").
			Where("id = ?", accountID).
			Update("connector_id", "")
	}

	// 移除 connector 註冊
	rdb.SRem(ctx, protocol.ConnectorsSetKey, mockConnector)
	rdb.Del(ctx, protocol.GetConnectorHeartbeatKey(mockConnector))
}

type stats struct {
	connected         int64
	chatsSyncAccounts int64
	chatsTotal        int64
	messages          int64
	errors            int64
}

func runSimulation(ctx context.Context) *stats {
	s := &stats{}

	// 1. 發送連線事件
	if !*skipConnect {
		fmt.Println("\n[1/3] 發送連線事件...")
		for _, accountID := range testAccounts {
			select {
			case <-ctx.Done():
				return s
			default:
			}

			if err := sendConnectedEvent(ctx, accountID); err != nil {
				atomic.AddInt64(&s.errors, 1)
				if *verbose {
					fmt.Printf("  連線事件失敗: account=%d, err=%v\n", accountID, err)
				}
			} else {
				atomic.AddInt64(&s.connected, 1)
				if *verbose {
					fmt.Printf("  帳號已連線: %d\n", accountID)
				}
			}
		}
		fmt.Printf("  完成: %d 個帳號已連線\n", s.connected)
	}

	// 2. 發送聊天室同步
	if !*skipChats && *chatsPerAccount > 0 {
		fmt.Println("\n[2/3] 發送聊天室同步...")
		for _, accountID := range testAccounts {
			select {
			case <-ctx.Done():
				return s
			default:
			}

			chats := generateChats(accountID, *chatsPerAccount)

			if err := sendChatsUpdated(ctx, accountID, chats); err != nil {
				atomic.AddInt64(&s.errors, 1)
				if *verbose {
					fmt.Printf("  聊天室同步失敗: account=%d, err=%v\n", accountID, err)
				}
			} else {
				atomic.AddInt64(&s.chatsSyncAccounts, 1)
				atomic.AddInt64(&s.chatsTotal, int64(len(chats)))
				if *verbose {
					fmt.Printf("  帳號 %d: 同步 %d 個聊天室\n", accountID, len(chats))
				}
			}
		}
		fmt.Printf("  完成: %d 個帳號, 共 %d 個聊天室\n", s.chatsSyncAccounts, s.chatsTotal)
	}

	// 3. 發送訊息
	if !*skipMessages && *messageCount > 0 {
		fmt.Println("\n[3/3] 發送訊息...")
		sendMessages(ctx, s)
		fmt.Printf("  完成: %d 條訊息\n", s.messages)
	}

	return s
}

func runHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 更新心跳 key
			heartbeatKey := protocol.GetConnectorHeartbeatKey(mockConnector)
			rdb.Set(ctx, heartbeatKey, time.Now().Unix(), 30*time.Second)

			// 發送心跳事件
			producer.PublishHeartbeat(ctx, &protocol.HeartbeatPayload{
				AccountCount: len(testAccounts),
				AccountIDs:   testAccounts,
				Uptime:       60,
				MemoryMB:     100,
				Version:      "mock-1.0.0",
			})
		}
	}
}

func sendConnectedEvent(ctx context.Context, accountID uint) error {
	// 從資料庫取得帳號資訊
	var account struct {
		PhoneNumber string
		PushName    string
	}
	db.Table("whatsapp_accounts").
		Select("phone_number, push_name").
		Where("id = ?", accountID).
		Scan(&account)

	phoneNumber := account.PhoneNumber
	if phoneNumber == "" {
		phoneNumber = fmt.Sprintf("8869%08d", accountID)
	}
	pushName := account.PushName
	if pushName == "" {
		pushName = fmt.Sprintf("MockUser_%d", accountID)
	}

	return producer.PublishConnected(ctx, accountID, &protocol.ConnectedPayload{
		PhoneNumber: phoneNumber,
		PushName:    pushName,
		Platform:    "mock",
		DeviceID:    fmt.Sprintf("mock-device-%d", accountID),
	})
}

func generateChats(accountID uint, count int) []protocol.ChatInfo {
	chats := make([]protocol.ChatInfo, count)

	// 80% 個人聊天, 20% 群組
	groupCount := count / 5

	for i := 0; i < count; i++ {
		isGroup := i < groupCount
		if isGroup {
			chats[i] = protocol.ChatInfo{
				JID:     fmt.Sprintf("%d-%d@g.us", accountID, i),
				Name:    fmt.Sprintf("群組_%d_%d", accountID, i),
				IsGroup: true,
				Avatar:  fmt.Sprintf("https://mock-avatar.example.com/group/%d/%d.jpg", accountID, i),
			}
		} else {
			phoneNum := 886900000000 + int64(accountID)*10000 + int64(i)
			chats[i] = protocol.ChatInfo{
				JID:     fmt.Sprintf("%d@s.whatsapp.net", phoneNum),
				Name:    fmt.Sprintf("聯絡人_%d_%d", accountID, i),
				IsGroup: false,
				Avatar:  fmt.Sprintf("https://mock-avatar.example.com/contact/%d/%d.jpg", accountID, i),
			}
		}
	}

	return chats
}

func sendChatsUpdated(ctx context.Context, accountID uint, chats []protocol.ChatInfo) error {
	// 分批發送，每批最多 500 個
	batchSize := 500
	for i := 0; i < len(chats); i += batchSize {
		end := i + batchSize
		if end > len(chats) {
			end = len(chats)
		}

		batch := chats[i:end]
		if err := producer.PublishChatsUpdated(ctx, accountID, &protocol.ChatsUpdatedPayload{
			Chats: batch,
		}); err != nil {
			return err
		}
	}
	return nil
}

func sendMessages(ctx context.Context, s *stats) {
	if *messageRate <= 0 {
		*messageRate = 50
	}

	interval := time.Second / time.Duration(*messageRate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	contents := []string{
		"你好！",
		"今天天氣真好",
		"這是一條測試訊息",
		"Mock Connector 產生的訊息",
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
		"測試中文訊息：一二三四五六七八九十",
		"Hello World! This is a test message from mock connector.",
		"訊息內容可以很長，這是為了測試系統處理長訊息的能力。這是一條相對較長的訊息，包含了更多的文字內容。",
	}

	for i := 0; i < *messageCount; i++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		accountIdx := rand.Intn(len(testAccounts))
		accountID := testAccounts[accountIdx]
		chatIdx := rand.Intn(*chatsPerAccount)

		var chatJID, senderJID string
		isGroup := chatIdx < *chatsPerAccount/5

		if isGroup {
			chatJID = fmt.Sprintf("%d-%d@g.us", accountID, chatIdx)
			senderJID = fmt.Sprintf("8869%08d@s.whatsapp.net", rand.Intn(100000000))
		} else {
			phoneNum := 886900000000 + int64(accountID)*10000 + int64(chatIdx)
			chatJID = fmt.Sprintf("%d@s.whatsapp.net", phoneNum)
			senderJID = chatJID
		}

		payload := &protocol.MessageReceivedPayload{
			MessageID:   uuid.New().String(),
			ChatJID:     chatJID,
			SenderJID:   senderJID,
			SenderName:  fmt.Sprintf("Mock User %d", rand.Intn(1000)),
			Content:     contents[rand.Intn(len(contents))],
			ContentType: "text",
			Timestamp:   time.Now().UnixMilli(),
			IsGroup:     isGroup,
			IsFromMe:    rand.Float32() < 0.3, // 30% 是自己發的
		}

		if err := producer.PublishMessageReceived(ctx, accountID, payload); err != nil {
			atomic.AddInt64(&s.errors, 1)
		} else {
			atomic.AddInt64(&s.messages, 1)
		}

		if *verbose && i%100 == 0 {
			fmt.Printf("  已發送 %d/%d 條訊息\n", i+1, *messageCount)
		}
	}
}
