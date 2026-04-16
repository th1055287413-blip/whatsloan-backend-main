package whatsmeow

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"whatsapp_golang/internal/protocol"

	"github.com/go-redis/redis/v8"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.uber.org/zap"
	"golang.org/x/net/proxy"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// ProxyConfig 代理配置
type ProxyConfig struct {
	Type     string // socks5 | http
	Host     string
	Port     int
	Username string
	Password string
}

// Manager is the integrated whatsmeow account manager
type Manager struct {
	connectorID   string
	publisher     EventPublisher
	redis         *redis.Client
	db            *gorm.DB // For querying account info
	container     *sqlstore.Container
	clients       map[uint]*whatsmeow.Client
	accountInfo   map[uint]*AccountInfo
	mu            sync.RWMutex
	loginSessions map[string]*loginSession // sessionID -> loginSession
	loginMu       sync.RWMutex
	log           *zap.SugaredLogger // 結構化 logger

	// Local media storage directory
	mediaDir string

	// Proxy configuration (for all clients created by this manager)
	proxyConfig *ProxyConfig

	// Per-account event workers（異步處理 whatsmeow 事件，避免阻塞 node handler）
	eventWorkers map[uint]*eventWorker

	// Full-sync archive event buffer（重連時暫存歸檔事件，HandleSyncChats 結束後批次發送）
	archiveBuf   map[uint][]protocol.ChatArchiveItem
	archiveBufMu sync.Mutex

	// Avatar sync control
	avatarSyncSem     chan struct{} // Global concurrency control semaphore
	avatarSyncRunning map[uint]bool // Accounts currently syncing avatars
	avatarSyncMu      sync.Mutex    // Protects avatarSyncRunning
}

// AccountInfo represents account information
type AccountInfo struct {
	AccountID   uint
	JID         types.JID
	PhoneNumber string
	PushName    string
	Connected   bool
	LastSeen    time.Time
}

// NewManagerWithProxy creates a new whatsmeow manager with proxy support.
// container and db are injected (owned by caller); Manager does not close them.
func NewManagerWithProxy(
	connectorID string,
	publisher EventPublisher,
	redis *redis.Client,
	container *sqlstore.Container,
	db *gorm.DB,
	mediaDir string,
	proxyConfig *ProxyConfig,
	log *zap.SugaredLogger,
) (*Manager, error) {
	store.DeviceProps.HistorySyncConfig.RecentSyncDaysLimit = proto.Uint32(30)

	return &Manager{
		connectorID:       connectorID,
		publisher:         publisher,
		redis:             redis,
		db:                db,
		container:         container,
		clients:           make(map[uint]*whatsmeow.Client),
		accountInfo:       make(map[uint]*AccountInfo),
		loginSessions:     make(map[string]*loginSession),
		log:               log,
		mediaDir:          mediaDir,
		proxyConfig:       proxyConfig,
		eventWorkers:      make(map[uint]*eventWorker),
		archiveBuf:        make(map[uint][]protocol.ChatArchiveItem),
		avatarSyncSem:     make(chan struct{}, 3),
		avatarSyncRunning: make(map[uint]bool),
	}, nil
}

// buildProxyURL constructs proxy URL from ProxyConfig
// Supports formats: socks5://user:pass@host:port or http://host:port
func buildProxyURL(p *ProxyConfig) string {
	if p == nil || p.Host == "" || p.Port == 0 {
		return ""
	}

	proxyType := p.Type
	if proxyType == "" {
		proxyType = "socks5"
	}

	if p.Username != "" && p.Password != "" {
		return fmt.Sprintf("%s://%s:%s@%s:%d", proxyType, p.Username, p.Password, p.Host, p.Port)
	}
	return fmt.Sprintf("%s://%s:%d", proxyType, p.Host, p.Port)
}

// TestProxyConnection 測試 proxy 連線是否有效
// 嘗試透過 proxy 連線到 WhatsApp 伺服器，並驗證出口 IP
func TestProxyConnection(p *ProxyConfig, timeout time.Duration) error {
	if p == nil {
		return nil
	}

	proxyURL := buildProxyURL(p)
	if proxyURL == "" {
		return fmt.Errorf("無效的 proxy 配置")
	}

	// 解析 proxy URL
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return fmt.Errorf("解析 proxy URL 失敗: %w", err)
	}

	// 建立 HTTP Transport，根據 proxy 類型設定
	var transport *http.Transport

	switch parsed.Scheme {
	case "socks5":
		var auth *proxy.Auth
		if parsed.User != nil {
			password, _ := parsed.User.Password()
			auth = &proxy.Auth{
				User:     parsed.User.Username(),
				Password: password,
			}
		}
		dialer, err := proxy.SOCKS5("tcp", parsed.Host, auth, &net.Dialer{Timeout: timeout})
		if err != nil {
			return fmt.Errorf("建立 SOCKS5 dialer 失敗: %w", err)
		}
		transport = &http.Transport{
			Dial: dialer.Dial,
		}
	case "http", "https":
		transport = &http.Transport{
			Proxy: http.ProxyURL(parsed),
		}
	default:
		return fmt.Errorf("不支援的 proxy 類型: %s", parsed.Scheme)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}

	// 測試連線並查詢出口 IP
	actualIP, err := getExternalIP(client)
	if err != nil {
		return fmt.Errorf("proxy 連線測試失敗: %w", err)
	}

	// 如果 Host 是 IP 格式，驗證出口 IP
	if net.ParseIP(p.Host) != nil {
		if actualIP != p.Host {
			return fmt.Errorf("出口 IP 不符: 預期 %s，實際 %s", p.Host, actualIP)
		}
	}

	return nil
}

// getExternalIP 通過 HTTP client 查詢外部 IP
func getExternalIP(client *http.Client) (string, error) {
	// 嘗試多個 IP 查詢服務
	services := []string{
		"https://ifconfig.me/ip",
		"https://api.ipify.org",
		"https://icanhazip.com",
	}

	var lastErr error
	for _, svc := range services {
		resp, err := client.Get(svc)
		if err != nil {
			lastErr = err
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip, nil
		}
		lastErr = fmt.Errorf("無效的 IP 回應: %s", ip)
	}

	return "", fmt.Errorf("所有 IP 查詢服務都失敗: %w", lastErr)
}

// createClientWithProxy creates a whatsmeow client with optional proxy
func (m *Manager) createClientWithProxy(device *store.Device) *whatsmeow.Client {
	client := whatsmeow.NewClient(device, waLog.Stdout("WhatsApp", "INFO", true))
	client.EnableAutoReconnect = true

	if m.proxyConfig != nil {
		proxyURL := buildProxyURL(m.proxyConfig)
		if proxyURL != "" {
			if err := client.SetProxyAddress(proxyURL); err != nil {
				m.log.Errorw("設定代理失敗", "error", err)
			} else {
				m.log.Infow("WhatsApp client 已啟用代理", "host", m.proxyConfig.Host, "port", m.proxyConfig.Port)
			}
		}
	} else {
		m.log.Infow("WhatsApp client 未配置代理（直連）")
	}

	return client
}

// GetAccountIDs returns all managed account IDs
func (m *Manager) GetAccountIDs() []uint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]uint, 0, len(m.clients))
	for id := range m.clients {
		ids = append(ids, id)
	}
	return ids
}

// GetAccountCount returns the number of accounts
func (m *Manager) GetAccountCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

// IsAccountManaged checks if an account is managed by this Connector
func (m *Manager) IsAccountManaged(accountID uint) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.clients[accountID]
	return exists
}

// getConnectedClient 取得已連線的 client；若不在記憶體中，嘗試從 session store 自動恢復（不需重新配對）
func (m *Manager) getConnectedClient(ctx context.Context, accountID uint) (*whatsmeow.Client, error) {
	m.mu.RLock()
	client, exists := m.clients[accountID]
	m.mu.RUnlock()

	if exists {
		if !client.IsConnected() {
			return nil, fmt.Errorf("帳號 %d 未連線", accountID)
		}
		return client, nil
	}

	// client 不在記憶體中，嘗試從 session store 恢復
	m.log.Infow("帳號不在記憶體中，嘗試自動恢復連線", "account_id", accountID)

	if err := m.HandleConnect(ctx, &protocol.Command{AccountID: accountID}); err != nil {
		return nil, fmt.Errorf("帳號 %d 自動恢復失敗: %w", accountID, err)
	}

	m.mu.RLock()
	client, exists = m.clients[accountID]
	m.mu.RUnlock()

	if !exists || !client.IsConnected() {
		return nil, fmt.Errorf("帳號 %d 恢復後仍未連線", accountID)
	}

	m.log.Infow("帳號已自動恢復連線", "account_id", accountID)
	return client, nil
}

// 連線限流相關常量
const (
	maxConcurrentConnections = 20 // 同時連線的最大帳號數
)

// RestoreAllSessions restores all logged-in sessions from whatsmeow session store
// This method is called when Connector starts, automatically restoring logged-in account connections
func (m *Manager) RestoreAllSessions(ctx context.Context) error {
	m.log.Infow("正在從 session store 恢復已登入的帳號")

	// Get all existing devices from whatsmeow session store
	devices, err := m.container.GetAllDevices(ctx)
	if err != nil {
		return fmt.Errorf("取得所有 device 失敗: %w", err)
	}

	if len(devices) == 0 {
		m.log.Infow("沒有已儲存的 session")
		return nil
	}

	m.log.Infow("找到已儲存的 session，開始批次連線", "session_count", len(devices), "max_concurrent", maxConcurrentConnections)

	// 使用 semaphore 限制同時連線數
	sem := make(chan struct{}, maxConcurrentConnections)
	var wg sync.WaitGroup
	var restoredCount int64

	for _, device := range devices {
		if device.ID == nil {
			continue
		}

		// 獲取 semaphore slot（阻塞直到有空位）
		sem <- struct{}{}

		wg.Add(1)
		go func(device *store.Device) {
			defer wg.Done()
			defer func() { <-sem }() // 釋放 semaphore slot

			if m.restoreSingleSession(ctx, device) {
				atomic.AddInt64(&restoredCount, 1)
			}
		}(device)
	}

	// 等待所有連線完成
	wg.Wait()

	m.log.Infow("Session 恢復完成", "restored_count", restoredCount)

	return nil
}

// restoreSingleSession 恢復單一帳號的連線
func (m *Manager) restoreSingleSession(ctx context.Context, device *store.Device) bool {
	jid := *device.ID
	phoneNumber := jid.User

	// Query account ID by phone number
	var account struct {
		ID uint
	}
	err := m.db.Table("whatsapp_accounts").
		Select("id").
		Where("phone_number = ?", phoneNumber).
		First(&account).Error

	if err != nil {
		m.log.Warnw("找不到電話號碼對應的帳號，跳過", "phone", phoneNumber)
		return false
	}

	accountID := account.ID

	// Check if already connected
	m.mu.RLock()
	_, alreadyConnected := m.clients[accountID]
	m.mu.RUnlock()
	if alreadyConnected {
		m.log.Debugw("帳號已經連線，跳過", "account_id", accountID)
		return false
	}

	// 使用 Redis 分布式鎖確保帳號分配的原子性
	accountLockKey := fmt.Sprintf("wa:account:lock:%d", accountID)
	lockAcquired, err := m.redis.SetNX(ctx, accountLockKey, m.connectorID, 30*time.Second).Result()
	if err != nil {
		m.log.Warnw("獲取帳號鎖失敗", "account_id", accountID, "error", err)
		return false
	}
	if !lockAcquired {
		m.log.Debugw("帳號正在被其他實例處理，跳過", "account_id", accountID)
		return false
	}

	// 獲得鎖後，再次檢查帳號是否已被分配
	var existingConnector string
	m.db.Table("whatsapp_accounts").
		Select("connector_id").
		Where("id = ?", accountID).
		Scan(&existingConnector)
	if existingConnector != "" && existingConnector != m.connectorID {
		m.log.Debugw("帳號已綁定到其他 Connector，跳過", "account_id", accountID, "other_connector", existingConnector)
		m.redis.Del(ctx, accountLockKey) // 釋放鎖
		return false
	}

	// Update JID mapping in Redis
	if err := m.redis.HSet(ctx, "wa:account:jid", fmt.Sprintf("%d", accountID), jid.String()).Err(); err != nil {
		m.log.Warnw("更新帳號的 JID 映射失敗", "account_id", accountID, "error", err)
	}

	// Set routing in DB (assign account to this Connector)
	if err := m.db.Table("whatsapp_accounts").Where("id = ?", accountID).Update("connector_id", m.connectorID).Error; err != nil {
		m.log.Warnw("設定帳號路由失敗", "account_id", accountID, "error", err)
		m.redis.Del(ctx, accountLockKey) // 釋放鎖
		return false
	}

	// Create client with proxy and connect
	client := m.createClientWithProxy(device)

	// 先啟動 event worker 再註冊 event handler
	m.mu.Lock()
	m.startEventWorker(accountID)
	m.mu.Unlock()

	client.AddEventHandler(m.createEventHandler(accountID))

	if err := client.Connect(); err != nil {
		m.log.Warnw("連接帳號失敗", "account_id", accountID, "error", err)
		m.stopEventWorker(accountID)
		if dbErr := m.db.Table("whatsapp_accounts").Where("id = ?", accountID).Update("connector_id", "").Error; dbErr != nil {
			m.log.Warnw("清理 connector_id 失敗", "account_id", accountID, "error", dbErr)
		}
		m.redis.Del(ctx, accountLockKey) // 釋放鎖
		return false
	}

	// 連線成功後釋放帳號鎖（帳號已被 connector_id 綁定）
	m.redis.Del(ctx, accountLockKey)

	// Save client
	m.mu.Lock()
	m.clients[accountID] = client
	m.accountInfo[accountID] = &AccountInfo{
		AccountID:   accountID,
		JID:         jid,
		PhoneNumber: phoneNumber,
		Connected:   true,
		LastSeen:    time.Now(),
	}
	m.mu.Unlock()

	// Publish Connected event (includes device_id)
	m.publisher.PublishConnected(ctx, accountID, &protocol.ConnectedPayload{
		PhoneNumber: phoneNumber,
		DeviceID:    jid.String(),
	})

	m.log.Infow("帳號已恢復連線", "account_id", accountID, "jid", jid.String(), "phone", phoneNumber)
	return true
}

// HandleConnect handles the connect command
func (m *Manager) HandleConnect(ctx context.Context, cmd *protocol.Command) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if client, exists := m.clients[cmd.AccountID]; exists && client.IsConnected() {
		m.log.Warnw("帳號已連線", "account_id", cmd.AccountID)
		return nil
	}

	// Get device from session store
	// Note: Need a way to map accountID to JID
	// Currently using Redis to store this mapping
	jidStr, err := m.redis.HGet(ctx, "wa:account:jid", fmt.Sprintf("%d", cmd.AccountID)).Result()
	if err != nil {
		return fmt.Errorf("找不到帳號 %d 的 JID 對應: %w", cmd.AccountID, err)
	}

	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return fmt.Errorf("無效的 JID: %w", err)
	}

	// Get device from session store
	device, err := m.container.GetDevice(ctx, jid)
	if err != nil {
		return fmt.Errorf("取得 device 失敗: %w", err)
	}
	if device == nil {
		return fmt.Errorf("帳號 %d 的 device 不存在", cmd.AccountID)
	}

	// Create client with proxy
	client := m.createClientWithProxy(device)

	// 先啟動 event worker 再註冊 event handler，確保事件不遺漏
	m.startEventWorker(cmd.AccountID)

	// Register event handler
	client.AddEventHandler(m.createEventHandler(cmd.AccountID))

	// Connect
	if err := client.Connect(); err != nil {
		// 清理 event worker（channel 為空，stop 不會觸發 handler，不會有鎖競爭）
		if w, ok := m.eventWorkers[cmd.AccountID]; ok {
			delete(m.eventWorkers, cmd.AccountID)
			w.stop()
		}
		return fmt.Errorf("連接失敗: %w", err)
	}

	// Save client
	m.clients[cmd.AccountID] = client
	m.accountInfo[cmd.AccountID] = &AccountInfo{
		AccountID:   cmd.AccountID,
		JID:         jid,
		PhoneNumber: jid.User,
		Connected:   true,
		LastSeen:    time.Now(),
	}

	m.log.Infow("帳號已連線", "account_id", cmd.AccountID, "jid", jid.String())

	return nil
}

// HandleDisconnect handles the disconnect command
func (m *Manager) HandleDisconnect(ctx context.Context, cmd *protocol.Command) error {
	m.mu.Lock()
	client, exists := m.clients[cmd.AccountID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("帳號 %d 不存在", cmd.AccountID)
	}

	// Disconnect
	client.Disconnect()

	// Remove
	delete(m.clients, cmd.AccountID)
	delete(m.accountInfo, cmd.AccountID)
	m.mu.Unlock()

	// 停止 event worker（在 mu 鎖外，因為 stop 會等待 goroutine 結束）
	m.stopEventWorker(cmd.AccountID)

	m.log.Infow("帳號已斷線", "account_id", cmd.AccountID)

	// Publish Disconnected event
	if err := m.publisher.PublishDisconnected(ctx, cmd.AccountID, &protocol.DisconnectedPayload{
		Reason: "manual_disconnect",
	}); err != nil {
		m.log.Warnw("發送 Disconnected 事件失敗", "account_id", cmd.AccountID, "error", err)
	}

	return nil
}

// startEventWorker 為帳號啟動 event worker（必須持有 m.mu 寫鎖或在初始化階段呼叫）
func (m *Manager) startEventWorker(accountID uint) {
	if _, exists := m.eventWorkers[accountID]; exists {
		return // 已存在
	}
	w := newEventWorker(accountID, m.log, func(ctx context.Context, evt interface{}) {
		m.dispatchEvent(ctx, accountID, evt)
	})
	m.eventWorkers[accountID] = w
	m.log.Debugw("event worker 已啟動", "account_id", accountID)
}

// stopEventWorker 停止帳號的 event worker（必須在 m.mu 鎖外呼叫，因為 stop 會等待 goroutine 結束）
func (m *Manager) stopEventWorker(accountID uint) {
	m.mu.Lock()
	w, exists := m.eventWorkers[accountID]
	if exists {
		delete(m.eventWorkers, accountID)
	}
	m.mu.Unlock()

	if exists {
		w.stop()
		m.log.Debugw("event worker 已停止", "account_id", accountID)
	}
}

// removeAccount 異步移除帳號（用於 LoggedOut handler，避免在 worker goroutine 內同步 stop 造成死鎖）
// LoggedOut 表示裝置已被手機端永久移除，device 無法再連線，必須從 session store 刪除。
func (m *Manager) removeAccount(accountID uint) {
	m.mu.Lock()
	client, exists := m.clients[accountID]
	if exists {
		client.Disconnect()
		delete(m.clients, accountID)
		delete(m.accountInfo, accountID)
	}
	m.mu.Unlock()

	m.stopEventWorker(accountID)

	// 從 whatsmeow session store 刪除 device（CASCADE 清除 app state、keys 等）
	// LoggedOut 後 device 已永久失效，保留只會導致重新配對後 app state 殘留問題
	if exists && client.Store != nil && client.Store.ID != nil {
		deviceJID := client.Store.ID.String()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Store.Delete(ctx); err != nil {
			m.log.Warnw("刪除已登出的 device 失敗", "account_id", accountID, "jid", deviceJID, "error", err)
		} else {
			m.log.Infow("已刪除已登出的 device", "account_id", accountID, "jid", deviceJID)
		}
	}
}

// cleanupOldDevice 配對前清理該帳號的舊 device。
// 透過 Redis JID mapping 找到舊 device，從 whatsmeow store 刪除（CASCADE 清除 app state）。
func (m *Manager) cleanupOldDevice(ctx context.Context, accountID uint) {
	jidStr, err := m.redis.HGet(ctx, "wa:account:jid", fmt.Sprintf("%d", accountID)).Result()
	if err != nil {
		return // 沒有舊的 JID mapping，不需要清理
	}

	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return
	}

	device, err := m.container.GetDevice(ctx, jid)
	if err != nil || device == nil {
		return // device 已不存在
	}

	if err := device.Delete(ctx); err != nil {
		m.log.Warnw("配對前清理舊 device 失敗", "account_id", accountID, "jid", jidStr, "error", err)
	} else {
		m.log.Infow("配對前已清理舊 device", "account_id", accountID, "jid", jidStr)
	}
}

// Shutdown closes all account connections
func (m *Manager) Shutdown(ctx context.Context) {
	// 先收集所有 worker，在鎖外停止（stop 會阻塞等待 goroutine 結束）
	m.mu.Lock()
	workers := make(map[uint]*eventWorker, len(m.eventWorkers))
	for id, w := range m.eventWorkers {
		workers[id] = w
	}
	m.eventWorkers = make(map[uint]*eventWorker)

	for accountID, client := range m.clients {
		m.log.Infow("關閉帳號連線", "account_id", accountID)
		client.Disconnect()

		// Publish Disconnected event
		m.publisher.PublishDisconnected(ctx, accountID, &protocol.DisconnectedPayload{
			Reason: "connector_shutdown",
		})
	}

	m.clients = make(map[uint]*whatsmeow.Client)
	m.accountInfo = make(map[uint]*AccountInfo)
	m.mu.Unlock()

	// 在鎖外停止所有 workers
	for _, w := range workers {
		w.stop()
	}
}

// EventWorkerStats 回傳各帳號 event worker 的 queue depth，供 health/debug 使用
func (m *Manager) EventWorkerStats() map[uint]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[uint]int, len(m.eventWorkers))
	for id, w := range m.eventWorkers {
		stats[id] = len(w.ch)
	}
	return stats
}
