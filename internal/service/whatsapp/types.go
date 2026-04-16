package whatsapp

import (
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"gorm.io/gorm"
	"whatsapp_golang/internal/model"
)

// Service WhatsApp服务接口
type Service interface {
	GetPairingCode(phoneNumber, channelCode string) (string, string, error) // 返回配对代码和会话ID
	VerifyPairingCode(sessionID, phoneNumber, code string) error
	GetQRCode(channelCode string) (string, string, error) // 返回二维码和会话ID
	VerifyQRCode(sessionID string) error
	GetAccounts() ([]*model.WhatsAppAccount, error)
	GetAccount(id uint) (*model.WhatsAppAccount, error)
	GetChats(accountID uint) ([]*model.WhatsAppChat, error)
	GetContacts(accountID uint, page, pageSize int) ([]*model.WhatsAppContact, int64, error)
	GetMessages(chatID uint, limit, offset int) ([]*model.WhatsAppMessage, error)
	GetMessagesByJID(accountID uint, jid string, limit, offset int) ([]*model.WhatsAppMessage, error)
	SendMessage(accountID uint, toJID, content string, adminID *uint) error
	SendMessageWithOriginal(accountID uint, toJID, content string, originalText string, adminID *uint) error
	SendImageMessage(accountID uint, contactPhone, imagePath, caption string, adminID *uint) error
	SendVideoMessage(accountID uint, contactPhone, videoPath, caption string, adminID *uint) error
	SendAudioMessage(accountID uint, contactPhone, audioPath string, adminID *uint) error
	SendDocumentMessage(accountID uint, contactPhone, documentPath, fileName string, adminID *uint) error
	DisconnectAccount(accountID uint) error
	ConnectAccount(accountID uint) error
	GetAccountStatus(accountID uint) (string, error)
	SyncChatsManually(accountID uint) error
	SyncChatHistory(accountID uint, chatJID string, count int) error
	UpdateContactNamesManually(accountID uint) error
	SyncAllData(accountID uint) error // 手动触发完整数据同步

	// 新增接口 - 匹配前端API需求
	GetSessionStatus(sessionID string) (*SessionStatus, error)
	DisconnectSession(sessionID string) error
	RestoreSession(sessionID string) error
	CleanupExpiredSessions() (int, error)

	// GetWhatsAppClient 获取指定账号的 WhatsApp 客户端
	GetWhatsAppClient(accountID uint) (*whatsmeow.Client, error)

	// SetInterceptor 设置消息拦截器
	SetInterceptor(interceptor MessageInterceptor)

	// UpdateAccountProfile 更新单个账号的用户资料(头像和用户名)
	UpdateAccountProfile(accountID uint) error
	// UpdateMissingAccountProfiles 批量更新缺失用户资料的账号
	UpdateMissingAccountProfiles() error
	// UpdateAllContactsAvatars 批量更新所有账号的联系人头像
	UpdateAllContactsAvatars() error

	// GetSyncStatusService 獲取同步狀態服務
	GetSyncStatusService() SyncStatusService
}

// SessionStatus 会话状态信息
type SessionStatus struct {
	Connected    bool      `json:"connected"`
	JID          string    `json:"jid"`
	PushName     string    `json:"push_name"`
	Platform     string    `json:"platform"`
	IsBusiness   bool      `json:"is_business"`
	BusinessName string    `json:"business_name,omitempty"`
	LastSeen     time.Time `json:"last_seen"`
	Avatar       string    `json:"avatar"`    // 用户头像URL
	AvatarID     string    `json:"avatar_id"` // 头像ID
}

// AccountService 統一帳號服務介面
// 直接操作 whatsapp_accounts 表，取代 UserService 的 users+accounts JOIN
type AccountService interface {
	ListAccounts(page, pageSize int, filters map[string]interface{}) ([]*model.WhatsAppAccount, int64, error)
	GetAccount(id uint) (*model.WhatsAppAccount, error)
	GetAccountChats(id uint, page, pageSize int, search string, archived *bool) ([]*model.WhatsAppChat, int64, error)
	GetAccountChatCounts(id uint) (*model.ChatCounts, error)
	CreateAccount(account *model.WhatsAppAccount) (*model.WhatsAppAccount, error)
	UpdateAccount(id uint, updates map[string]interface{}) (*model.WhatsAppAccount, error)
	DeleteAccount(id uint) error
	BatchOperation(accountIDs []uint, operation string, data map[string]interface{}) (int, []map[string]interface{}, error)
	GetAccountStats(filters map[string]interface{}) (*AccountStats, error)
	GetAccountIDRange(filters map[string]interface{}) (map[string]interface{}, error)
	GetDisconnectStats(filters map[string]interface{}) (*DisconnectStats, error)
	GetConversationHistory(accountID uint, contactPhone string, page, limit int, targetLanguage string) ([]*model.MessageWithSender, int64, error)
	ClearUnreadCount(accountID uint, contactPhone string) error
	GetDB() *gorm.DB
}

// AccountStats 帳號統計
type AccountStats struct {
	Total        int64 `json:"total"`
	Active       int64 `json:"active"`
	Online       int64 `json:"online"`
	Connected    int64 `json:"connected"`
	Disconnected int64 `json:"disconnected"`
	LoggedOut    int64 `json:"logged_out"`
}

// DisconnectStatsBucket 掉線時長分佈桶
type DisconnectStatsBucket struct {
	Label      string   `json:"label"`
	DaysMin    *float64 `json:"days_min,omitempty"`
	DaysMax    *float64 `json:"days_max,omitempty"`
	Count      int64    `json:"count"`
	Percentage float64  `json:"percentage"`
}

// DisconnectStats 授權後首次掉線時長統計
type DisconnectStats struct {
	Distribution      []DisconnectStatsBucket `json:"distribution"`
	TotalDisconnected int64                   `json:"total_disconnected"`
	NeverDisconnected int64                   `json:"never_disconnected"`
}

// MessageInterceptor 消息拦截器接口
type MessageInterceptor interface {
	CheckMessage(accountID uint, messageID, chatID, senderJID, senderName, receiverName, content string, isFromMe bool, messageTimestamp int64, toJID string, sentByAdminID *uint) error
}

// ParsedMessage 解析后的消息结构
type ParsedMessage struct {
	Content    string
	Type       string
	Metadata   map[string]interface{}
	Skip       bool // 是否跳过（协议消息等）
	NeedsMedia bool // 是否需要下载媒体
}

// qrSession 二维码登录会话
type qrSession struct {
	client      *whatsmeow.Client
	qrChan      <-chan whatsmeow.QRChannelItem
	createdAt   time.Time
	accountID   *uint // 保存关联的账号ID，用于前端轮询查询
	channelCode string
}

// pairingSession 配对登录会话
type pairingSession struct {
	client      *whatsmeow.Client
	phoneNumber string
	createdAt   time.Time
	accountID   *uint  // 保存关联的账号ID，用于前端轮询查询
	channelCode string // 渠道码，用于自动绑定用户到渠道
}

// accountDBLocks 账号级别的数据库操作锁管理器
type accountDBLocks struct {
	locks map[uint]*sync.Mutex
	mu    sync.RWMutex
}

// newAccountDBLocks 创建账号锁管理器
func newAccountDBLocks() *accountDBLocks {
	return &accountDBLocks{
		locks: make(map[uint]*sync.Mutex),
	}
}

// getLock 获取指定账号的锁
func (a *accountDBLocks) getLock(accountID uint) *sync.Mutex {
	a.mu.RLock()
	lock, exists := a.locks[accountID]
	a.mu.RUnlock()

	if exists {
		return lock
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// 再次检查，避免重复创建
	if lock, exists := a.locks[accountID]; exists {
		return lock
	}

	a.locks[accountID] = &sync.Mutex{}
	return a.locks[accountID]
}

// cleanup 清理不再使用的锁（可选，在删除账号时调用）
func (a *accountDBLocks) cleanup(accountID uint) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.locks, accountID)
}
