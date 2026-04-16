package whatsapp

import "time"

// SyncType 同步類型
type SyncType string

const (
	SyncTypeChat    SyncType = "chat"    // 聊天列表同步
	SyncTypeHistory SyncType = "history" // 歷史消息同步
	SyncTypeContact SyncType = "contact" // 聯絡人同步
)

// SyncConfig 統一同步配置
// 所有同步相關的配置都集中在這裡，避免硬編碼散落各處
type SyncConfig struct {
	// === 全局開關 ===
	Enabled  bool // 是否啟用同步功能
	TestMode bool // 測試模式：停用所有主動同步，僅維持連接

	// === 定時器間隔 ===
	ChatSyncInterval      time.Duration // 聊天列表定期同步間隔 (預設 30m)
	ContactUpdateInterval time.Duration // 聯絡人名稱更新間隔 (預設 10m)
	PresenceInterval      time.Duration // 在線狀態發送間隔 (預設 2m)
	PresenceEnabled       bool          // 是否發送在線狀態 (預設 false，避免用戶帳號因系統而一直顯示在線)

	// === 限速配置 ===
	RateLimit float64 // 每秒任務數 (預設 0.2 = 每5秒1個任務)
	BurstSize int     // 突發大小 (預設 1)

	// === 新帳號配置 ===
	NewAccountSyncDelay time.Duration // 新帳號連接後延遲多久開始同步 (預設 30s)

	// === 歷史同步配置 ===
	MaxChatsToSync      int           // 最多同步多少個聊天的歷史 (預設 25)
	MessagesPerChat     int           // 每個聊天同步多少條消息 (預設 100)
	HistorySyncInterval time.Duration // 歷史同步任務之間的間隔 (預設 2s)

	// === 頭像同步配置 ===
	AvatarSyncEnabled bool          // 是否啟用頭像同步
	AvatarBatchSize   int           // 頭像批量更新大小 (預設 10)
	AvatarSyncDelay   time.Duration // 頭像同步延遲 (預設 100ms)
}

// DefaultSyncConfig 預設同步配置
// 修改這裡的 TestMode 即可統一控制所有同步行為
var DefaultSyncConfig = &SyncConfig{
	// 全局開關
	Enabled:  true,
	TestMode: false, // ← 統一測試模式開關：true=停用所有主動同步

	// 定時器間隔
	ChatSyncInterval:      30 * time.Minute,
	ContactUpdateInterval: 10 * time.Minute,
	PresenceInterval:      2 * time.Minute,
	PresenceEnabled:       false,

	// 限速配置
	RateLimit: 2, // 每5秒1個任務
	BurstSize: 1,

	// 新帳號配置
	NewAccountSyncDelay: 10 * time.Second, // 連接後立即同步

	// 歷史同步配置
	MaxChatsToSync:      25,
	MessagesPerChat:     100,
	HistorySyncInterval: 2 * time.Second,

	// 頭像同步配置
	AvatarSyncEnabled: false, // 預設關閉，避免 API 請求過多
	AvatarBatchSize:   10,
	AvatarSyncDelay:   100 * time.Millisecond,
}

// NewSyncConfig 創建同步配置（使用預設值）
func NewSyncConfig() *SyncConfig {
	// 返回預設配置的副本，避免修改全局預設
	config := *DefaultSyncConfig
	return &config
}

// IsTestMode 檢查是否為測試模式
func (c *SyncConfig) IsTestMode() bool {
	return c.TestMode || !c.Enabled
}

// ShouldSync 檢查是否應該執行同步
func (c *SyncConfig) ShouldSync() bool {
	return c.Enabled && !c.TestMode
}

// ShouldSyncAvatar 檢查是否應該同步頭像
func (c *SyncConfig) ShouldSyncAvatar() bool {
	return c.ShouldSync() && c.AvatarSyncEnabled
}
