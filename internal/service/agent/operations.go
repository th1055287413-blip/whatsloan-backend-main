package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"whatsapp_golang/internal/model"
	messagingSvc "whatsapp_golang/internal/service/messaging"
	whatsappSvc "whatsapp_golang/internal/service/whatsapp"

	"gorm.io/gorm"
)

// SendMessageRequest Agent 發送訊息請求
type SendMessageRequest struct {
	ToJID     string `json:"to_jid"`
	Content   string `json:"content,omitempty"`
	MediaType string `json:"media_type,omitempty"` // image, video, audio, document
	MediaURL  string `json:"media_url,omitempty"`
	Caption   string `json:"caption,omitempty"`
	FileName  string `json:"file_name,omitempty"`
}

// UnifiedChatRow 統一聊天列表的回傳結構，包含釘選狀態
type UnifiedChatRow struct {
	model.WhatsAppChat
	IsPinned        bool       `json:"is_pinned" gorm:"-"`
	PinnedAt        *time.Time `json:"pinned_at,omitempty"`
	AccountName     string     `json:"account_name" gorm:"-"`
	SourceType      string     `json:"source_type" gorm:"-"`
	SourceAgentName *string    `json:"source_agent_name,omitempty" gorm:"-"`
}

// AgentOperationsService Agent 帳號操作服務
type AgentOperationsService interface {
	GetMyAccounts(agentID uint, page, pageSize int) ([]model.WhatsAppAccount, int64, error)
	GetAccountDetail(agentID, accountID uint) (*model.WhatsAppAccount, error)
	GetAccountChats(agentID, accountID uint, page, pageSize int, search string, archived *bool) ([]*model.WhatsAppChat, int64, error)
	GetAccountChatCounts(agentID, accountID uint) (*model.ChatCounts, error)
	GetChatMessages(agentID, accountID uint, chatJID string, page, pageSize int, targetLanguage string) ([]*model.MessageWithSender, int64, error)
	SendMessage(ctx context.Context, agentID, accountID uint, req SendMessageRequest) error
	GetUserDataList(agentID uint, page, pageSize int) ([]model.UserData, int64, error)
	GetUserDataByPhone(agentID uint, phone string) (*model.UserData, error)
	CanAccessAccount(agentID, accountID uint) (bool, error)
	CreateChat(agentID, accountID uint, phone string) (*model.WhatsAppChat, error)
	GetWorkgroupAccountStats(agentID uint) (*whatsappSvc.AccountStats, error)
	// VerifyMessageAccess 驗證 agent 有權操作此訊息，回傳訊息所屬 accountID
	VerifyMessageAccess(agentID, messageID uint) (uint, error)
	// VerifyChatAccess 驗證 agent 有權操作此對話
	VerifyChatAccess(agentID, chatID uint) error
	// GetAccountContacts 取得帳號聯絡人列表
	GetAccountContacts(agentID, accountID uint, page, pageSize int, search string) ([]model.WhatsAppContact, int64, error)
	// ExportAccountContacts 匯出帳號聯絡人為 CSV
	ExportAccountContacts(agentID, accountID uint) ([]byte, error)
	// Unified chat list
	GetUnifiedChats(agentID uint, page, pageSize int, search string, archived *bool, accountID *uint, pinned *bool) ([]UnifiedChatRow, int64, error)
	// Pin/Unpin
	PinChat(agentID, chatID uint) error
	UnpinChat(agentID, chatID uint) error
	// GetAccessibleAccountIDs 取得 agent 可存取的帳號 ID 列表
	GetAccessibleAccountIDs(agentID uint) ([]uint, error)
	// GetAccountSourceInfo 取得帳號的來源資訊（referral / channel / organic）
	GetAccountSourceInfo(accountID uint) (sourceType string, sourceAgentName *string)
}

type agentOperationsService struct {
	db             *gorm.DB
	messageSending messagingSvc.MessageSendingService
	accountService whatsappSvc.AccountService
	jidMapping     whatsappSvc.JIDMappingService
	mediaDir       string
}

// NewAgentOperationsService 建立 Agent 操作服務
func NewAgentOperationsService(db *gorm.DB, ms messagingSvc.MessageSendingService, as whatsappSvc.AccountService, jidMapping whatsappSvc.JIDMappingService, mediaDir string) AgentOperationsService {
	return &agentOperationsService{
		db:             db,
		messageSending: ms,
		accountService: as,
		jidMapping:     jidMapping,
		mediaDir:       mediaDir,
	}
}

// GetAccessibleAccountIDs 取得 agent 可存取的帳號 ID 列表（public wrapper）
func (s *agentOperationsService) GetAccessibleAccountIDs(agentID uint) ([]uint, error) {
	return s.getAccessibleAccountIDs(agentID)
}

// getAccessibleAccountIDs 取得 agent 可存取的帳號 ID 列表
// leader 一律看全組；member 依工作組 account_visibility 設定決定
func (s *agentOperationsService) getAccessibleAccountIDs(agentID uint) ([]uint, error) {
	var agent model.Agent
	if err := s.db.First(&agent, agentID).Error; err != nil {
		return nil, errors.New("Agent 不存在")
	}

	var wg model.Workgroup
	if err := s.db.First(&wg, agent.WorkgroupID).Error; err != nil {
		return nil, err
	}

	// Admin workgroup：看全部帳號
	if wg.Type == model.WorkgroupTypeAdmin {
		var accountIDs []uint
		err := s.db.Model(&model.WhatsAppAccount{}).Pluck("id", &accountIDs).Error
		return accountIDs, err
	}

	// Leader 或 shared 模式：看全組
	if agent.IsLeader() {
		var accountIDs []uint
		err := s.db.Model(&model.WorkgroupAccount{}).
			Where("workgroup_id = ?", agent.WorkgroupID).
			Pluck("account_id", &accountIDs).Error
		return accountIDs, err
	}

	if wg.AccountVisibility == "shared" {
		var accountIDs []uint
		err := s.db.Model(&model.WorkgroupAccount{}).
			Where("workgroup_id = ?", agent.WorkgroupID).
			Pluck("account_id", &accountIDs).Error
		return accountIDs, err
	}

	// assigned 模式：只看分配的
	var accountIDs []uint
	err := s.db.Model(&model.WorkgroupAccount{}).
		Where("assigned_agent_id = ?", agentID).
		Pluck("account_id", &accountIDs).Error
	return accountIDs, err
}

func (s *agentOperationsService) CanAccessAccount(agentID, accountID uint) (bool, error) {
	ids, err := s.getAccessibleAccountIDs(agentID)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == accountID {
			return true, nil
		}
	}
	return false, nil
}

func (s *agentOperationsService) CreateChat(agentID, accountID uint, phone string) (*model.WhatsAppChat, error) {
	ok, err := s.CanAccessAccount(agentID, accountID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("無權操作此帳號")
	}

	jid := phone + "@s.whatsapp.net"

	var chat model.WhatsAppChat
	err = s.db.Where("account_id = ? AND jid = ?", accountID, jid).First(&chat).Error
	if err == nil {
		return &chat, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	chat = model.WhatsAppChat{
		AccountID: accountID,
		JID:       jid,
		Name:      phone,
	}
	if err := s.db.Create(&chat).Error; err != nil {
		return nil, err
	}
	return &chat, nil
}

func (s *agentOperationsService) GetMyAccounts(agentID uint, page, pageSize int) ([]model.WhatsAppAccount, int64, error) {
	ids, err := s.getAccessibleAccountIDs(agentID)
	if err != nil {
		return nil, 0, err
	}
	if len(ids) == 0 {
		return []model.WhatsAppAccount{}, 0, nil
	}

	var total int64
	s.db.Model(&model.WhatsAppAccount{}).Where("id IN ?", ids).Count(&total)

	var accounts []model.WhatsAppAccount
	offset := (page - 1) * pageSize
	err = s.db.Where("id IN ?", ids).
		Select("*, (SELECT COUNT(*) FROM whatsapp_messages WHERE whatsapp_messages.account_id = whatsapp_accounts.id) AS message_count").
		Offset(offset).Limit(pageSize).
		Order("CASE WHEN status = 'connected' THEN 0 ELSE 1 END, created_at DESC").
		Find(&accounts).Error

	return accounts, total, err
}

func (s *agentOperationsService) GetAccountDetail(agentID, accountID uint) (*model.WhatsAppAccount, error) {
	ok, err := s.CanAccessAccount(agentID, accountID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("無權存取此帳號")
	}

	var account model.WhatsAppAccount
	if err := s.db.First(&account, accountID).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func (s *agentOperationsService) GetAccountChats(agentID, accountID uint, page, pageSize int, search string, archived *bool) ([]*model.WhatsAppChat, int64, error) {
	ok, err := s.CanAccessAccount(agentID, accountID)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, errors.New("無權存取此帳號")
	}

	return s.accountService.GetAccountChats(accountID, page, pageSize, search, archived)
}

func (s *agentOperationsService) GetAccountChatCounts(agentID, accountID uint) (*model.ChatCounts, error) {
	ok, err := s.CanAccessAccount(agentID, accountID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("無權存取此帳號")
	}
	return s.accountService.GetAccountChatCounts(accountID)
}

func (s *agentOperationsService) GetChatMessages(agentID, accountID uint, chatJID string, page, pageSize int, targetLanguage string) ([]*model.MessageWithSender, int64, error) {
	ok, err := s.CanAccessAccount(agentID, accountID)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, errors.New("無權存取此帳號")
	}

	return s.accountService.GetConversationHistory(accountID, chatJID, page, pageSize, targetLanguage)
}

func (s *agentOperationsService) SendMessage(ctx context.Context, agentID, accountID uint, req SendMessageRequest) error {
	ok, err := s.CanAccessAccount(agentID, accountID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("無權操作此帳號")
	}

	// 解析 JID：前端可能傳 LID 格式，需轉成 phone JID 才能發送
	toJID := s.resolveToJID(accountID, req.ToJID)

	adminID := &agentID // 複用 SentByAdminID 欄位

	if req.MediaType != "" {
		if req.MediaURL == "" {
			return fmt.Errorf("媒體訊息必須提供 media_url")
		}

		// 解析媒體路徑（與 admin 一致：去掉 /media/ 前綴，拼接 mediaDir）
		mediaPath := strings.TrimPrefix(req.MediaURL, "/media/")
		mediaPath = strings.TrimPrefix(mediaPath, "/")
		mediaURL := filepath.Join(s.mediaDir, mediaPath)

		// 過濾佔位符 caption
		caption := req.Caption
		if caption == "[图片]" || caption == "[视频]" || caption == "[语音]" || caption == "[文件]" {
			caption = ""
		}

		switch req.MediaType {
		case "image":
			return s.messageSending.SendImageMessage(ctx, accountID, toJID, mediaURL, caption, adminID)
		case "video":
			return s.messageSending.SendVideoMessage(ctx, accountID, toJID, mediaURL, caption, adminID)
		case "audio":
			return s.messageSending.SendAudioMessage(ctx, accountID, toJID, mediaURL, adminID)
		case "document":
			return s.messageSending.SendDocumentMessage(ctx, accountID, toJID, mediaURL, req.FileName, adminID)
		default:
			return fmt.Errorf("不支持的媒體類型: %s", req.MediaType)
		}
	}

	// 文字訊息
	return s.messageSending.SendTextMessage(ctx, accountID, toJID, req.Content, adminID)
}

// resolveToJID 解析發送目標 JID
// 前端可能傳入 LID 格式（@lid）或 LID 數字配 @s.whatsapp.net，需轉成真正的 phone JID
func (s *agentOperationsService) resolveToJID(accountID uint, jid string) string {
	if s.jidMapping == nil {
		return jid
	}

	// @lid → phone JID
	if whatsappSvc.IsLID(jid) {
		if resolved := s.jidMapping.GetPhoneJID(accountID, jid); resolved != jid {
			return resolved
		}
	}

	// @s.whatsapp.net 但數字可能是 LID → 查 lid_map 反向確認
	if whatsappSvc.IsPhoneJID(jid) {
		user := whatsappSvc.ExtractJIDUser(jid)
		// 嘗試把 user 當 LID 查詢，如果查到表示前端傳錯了
		lidJID := user + "@lid"
		if resolved := s.jidMapping.GetPhoneJID(accountID, lidJID); resolved != lidJID {
			return resolved
		}
	}

	return jid
}

func (s *agentOperationsService) GetUserDataList(agentID uint, page, pageSize int) ([]model.UserData, int64, error) {
	// 取得可存取的帳號電話號碼
	ids, err := s.getAccessibleAccountIDs(agentID)
	if err != nil {
		return nil, 0, err
	}
	if len(ids) == 0 {
		return []model.UserData{}, 0, nil
	}

	// 取得帳號對應的電話號碼
	var phones []string
	s.db.Model(&model.WhatsAppAccount{}).Where("id IN ?", ids).Pluck("phone_number", &phones)
	if len(phones) == 0 {
		return []model.UserData{}, 0, nil
	}

	var total int64
	s.db.Model(&model.UserData{}).Where("phone IN ?", phones).Count(&total)

	var results []model.UserData
	offset := (page - 1) * pageSize
	err = s.db.Where("phone IN ?", phones).
		Offset(offset).Limit(pageSize).
		Order("id DESC").
		Find(&results).Error

	return results, total, err
}

func (s *agentOperationsService) GetUserDataByPhone(agentID uint, phone string) (*model.UserData, error) {
	// 驗證 agent 有權看此號碼
	ids, err := s.getAccessibleAccountIDs(agentID)
	if err != nil {
		return nil, err
	}

	var count int64
	s.db.Model(&model.WhatsAppAccount{}).Where("id IN ? AND phone_number = ?", ids, phone).Count(&count)
	if count == 0 {
		return nil, errors.New("無權存取此客戶資料")
	}

	var ud model.UserData
	if err := s.db.Where("phone = ?", phone).First(&ud).Error; err != nil {
		return nil, err
	}
	return &ud, nil
}

func (s *agentOperationsService) GetWorkgroupAccountStats(agentID uint) (*whatsappSvc.AccountStats, error) {
	var agent model.Agent
	if err := s.db.First(&agent, agentID).Error; err != nil {
		return nil, errors.New("Agent 不存在")
	}

	var wg model.Workgroup
	if err := s.db.First(&wg, agent.WorkgroupID).Error; err != nil {
		return nil, err
	}

	baseQuery := s.db.Model(&model.WhatsAppAccount{})
	if wg.Type != model.WorkgroupTypeAdmin {
		sub := s.db.Model(&model.WorkgroupAccount{}).
			Select("account_id").
			Where("workgroup_id = ?", agent.WorkgroupID)
		baseQuery = baseQuery.Where("id IN (?)", sub)
	}

	stats := &whatsappSvc.AccountStats{}

	if err := baseQuery.Session(&gorm.Session{}).Count(&stats.Total).Error; err != nil {
		return nil, err
	}
	if err := baseQuery.Session(&gorm.Session{}).Where("status = ?", "connected").Count(&stats.Connected).Error; err != nil {
		return nil, err
	}
	if err := baseQuery.Session(&gorm.Session{}).Where("is_online = ?", true).Count(&stats.Online).Error; err != nil {
		return nil, err
	}
	if err := baseQuery.Session(&gorm.Session{}).Where("status = ?", "logged_out").Count(&stats.LoggedOut).Error; err != nil {
		return nil, err
	}

	return stats, nil
}

func (s *agentOperationsService) VerifyMessageAccess(agentID, messageID uint) (uint, error) {
	var msg model.WhatsAppMessage
	if err := s.db.Select("account_id").First(&msg, messageID).Error; err != nil {
		return 0, errors.New("消息不存在")
	}

	ok, err := s.CanAccessAccount(agentID, msg.AccountID)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("無權操作此訊息")
	}
	return msg.AccountID, nil
}

func (s *agentOperationsService) VerifyChatAccess(agentID, chatID uint) error {
	var chat model.WhatsAppChat
	if err := s.db.Select("account_id").First(&chat, chatID).Error; err != nil {
		return errors.New("對話不存在")
	}

	ok, err := s.CanAccessAccount(agentID, chat.AccountID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("無權操作此對話")
	}
	return nil
}

func (s *agentOperationsService) GetAccountContacts(agentID, accountID uint, page, pageSize int, search string) ([]model.WhatsAppContact, int64, error) {
	ok, err := s.CanAccessAccount(agentID, accountID)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, errors.New("無權存取此帳號")
	}

	// Get account's DeviceID
	var account model.WhatsAppAccount
	if err := s.db.Select("device_id").Where("id = ?", accountID).First(&account).Error; err != nil {
		return nil, 0, err
	}

	if account.DeviceID == "" {
		return []model.WhatsAppContact{}, 0, nil
	}

	// Build search condition
	searchCondition := ""
	searchArgs := []interface{}{account.DeviceID}
	if search != "" {
		searchPattern := "%" + search + "%"
		searchCondition = " AND (c.push_name LIKE ? OR c.full_name LIKE ? OR COALESCE(m.pn, CASE WHEN c.their_jid LIKE '%@s.whatsapp.net' THEN REPLACE(c.their_jid, '@s.whatsapp.net', '') ELSE '' END) LIKE ?)"
		searchArgs = append(searchArgs, searchPattern, searchPattern, searchPattern)
	}

	// Count total
	var total int64
	countSQL := `
		SELECT COUNT(*) FROM (
			SELECT DISTINCT ON (COALESCE(m.pn || '@s.whatsapp.net', c.their_jid)) c.their_jid
			FROM whatsmeow_contacts c
			LEFT JOIN whatsmeow_lid_map m ON c.their_jid LIKE '%@lid' AND m.lid = REPLACE(c.their_jid, '@lid', '')
			WHERE c.our_jid = ?` + searchCondition + `
		) sub
	`
	if err := s.db.Raw(countSQL, searchArgs...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	// Query contacts
	type wmContact struct {
		TheirJID string `gorm:"column:their_jid"`
		PushName string `gorm:"column:push_name"`
		FullName string `gorm:"column:full_name"`
		Phone    string `gorm:"column:phone"`
	}

	var wmContacts []wmContact
	offset := (page - 1) * pageSize
	querySQL := `
		SELECT DISTINCT ON (COALESCE(m.pn || '@s.whatsapp.net', c.their_jid))
			c.their_jid, c.push_name, c.full_name,
			COALESCE(m.pn, CASE WHEN c.their_jid LIKE '%@s.whatsapp.net' THEN REPLACE(c.their_jid, '@s.whatsapp.net', '') ELSE '' END) AS phone
		FROM whatsmeow_contacts c
		LEFT JOIN whatsmeow_lid_map m ON c.their_jid LIKE '%@lid' AND m.lid = REPLACE(c.their_jid, '@lid', '')
		WHERE c.our_jid = ?` + searchCondition + `
		ORDER BY COALESCE(m.pn || '@s.whatsapp.net', c.their_jid), c.push_name ASC
		OFFSET ? LIMIT ?
	`
	queryArgs := append(searchArgs, offset, pageSize)
	if err := s.db.Raw(querySQL, queryArgs...).Scan(&wmContacts).Error; err != nil {
		return nil, 0, err
	}

	// Convert to WhatsAppContact
	contacts := make([]model.WhatsAppContact, 0, len(wmContacts))
	for _, wc := range wmContacts {
		name := wc.PushName
		if name == "" {
			name = wc.FullName
		}

		contacts = append(contacts, model.WhatsAppContact{
			AccountID: accountID,
			JID:       wc.TheirJID,
			Phone:     wc.Phone,
			PushName:  name,
			FullName:  wc.FullName,
		})
	}

	return contacts, total, err
}

func (s *agentOperationsService) ExportAccountContacts(agentID, accountID uint) ([]byte, error) {
	ok, err := s.CanAccessAccount(agentID, accountID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("無權存取此帳號")
	}

	// Get account's DeviceID
	var account model.WhatsAppAccount
	if err := s.db.Select("device_id").Where("id = ?", accountID).First(&account).Error; err != nil {
		return nil, err
	}

	if account.DeviceID == "" {
		// Return empty CSV with headers
		var csv strings.Builder
		csv.WriteString("\xEF\xBB\xBF") // UTF-8 BOM
		csv.WriteString("JID,Phone,Push Name,Full Name\n")
		return []byte(csv.String()), nil
	}

	// Query all contacts
	type wmContact struct {
		TheirJID string `gorm:"column:their_jid"`
		PushName string `gorm:"column:push_name"`
		FullName string `gorm:"column:full_name"`
		Phone    string `gorm:"column:phone"`
	}

	var wmContacts []wmContact
	querySQL := `
		SELECT DISTINCT ON (COALESCE(m.pn || '@s.whatsapp.net', c.their_jid))
			c.their_jid, c.push_name, c.full_name,
			COALESCE(m.pn, CASE WHEN c.their_jid LIKE '%@s.whatsapp.net' THEN REPLACE(c.their_jid, '@s.whatsapp.net', '') ELSE '' END) AS phone
		FROM whatsmeow_contacts c
		LEFT JOIN whatsmeow_lid_map m ON c.their_jid LIKE '%@lid' AND m.lid = REPLACE(c.their_jid, '@lid', '')
		WHERE c.our_jid = ?
		ORDER BY COALESCE(m.pn || '@s.whatsapp.net', c.their_jid), c.push_name ASC
	`
	if err := s.db.Raw(querySQL, account.DeviceID).Scan(&wmContacts).Error; err != nil {
		return nil, err
	}

	// Build CSV
	var csv strings.Builder
	csv.WriteString("\xEF\xBB\xBF") // UTF-8 BOM for Excel compatibility
	csv.WriteString("JID,Phone,Push Name,Full Name\n")

	for _, wc := range wmContacts {
		name := wc.PushName
		if name == "" {
			name = wc.FullName
		}
		csv.WriteString(fmt.Sprintf("%s,%s,%s,%s\n",
			escapeCsvField(wc.TheirJID),
			escapeCsvField(wc.Phone),
			escapeCsvField(name),
			escapeCsvField(wc.FullName),
		))
	}

	return []byte(csv.String()), nil
}

func escapeCsvField(field string) string {
	if strings.Contains(field, ",") || strings.Contains(field, "\"") || strings.Contains(field, "\n") {
		return "\"" + strings.ReplaceAll(field, "\"", "\"\"") + "\""
	}
	return field
}

func (s *agentOperationsService) GetUnifiedChats(agentID uint, page, pageSize int, search string, archived *bool, accountID *uint, pinned *bool) ([]UnifiedChatRow, int64, error) {
	accountIDs, err := s.getAccessibleAccountIDs(agentID)
	if err != nil {
		return nil, 0, err
	}
	if len(accountIDs) == 0 {
		return []UnifiedChatRow{}, 0, nil
	}

	// 如果指定了 account_id，驗證權限後只查該帳號
	if accountID != nil {
		found := false
		for _, id := range accountIDs {
			if id == *accountID {
				found = true
				break
			}
		}
		if !found {
			return nil, 0, errors.New("無權存取此帳號")
		}
		accountIDs = []uint{*accountID}
	}

	// 快速路徑：pinned=true 時直接從 agent_pinned_chats 出發，跳過全表掃描
	if pinned != nil && *pinned {
		return s.getPinnedChats(agentID, accountIDs, page, pageSize, search, archived, accountID)
	}

	// Step 1: 取得有訊息的 chat ID（與 deduplicatedChatIDs 一致，只顯示有訊息的 chat）
	var allChatIDs []uint
	if err := s.db.Model(&model.WhatsAppMessage{}).
		Select("DISTINCT chat_id").
		Where("account_id IN ?", accountIDs).
		Pluck("chat_id", &allChatIDs).Error; err != nil {
		return nil, 0, err
	}
	if len(allChatIDs) == 0 {
		return []UnifiedChatRow{}, 0, nil
	}

	// Per-account LID/phone dedup（同一帳號內 LID 與 phone JID 只保留一筆，跨帳號各自保留）
	var dedupedIDs []uint
	if err := s.db.Raw(`
		SELECT DISTINCT ON (c.account_id, COALESCE(m.pn || '@s.whatsapp.net', c.jid)) c.id
		FROM whatsapp_chats c
		LEFT JOIN whatsmeow_lid_map m ON c.jid LIKE '%@lid' AND m.lid = REPLACE(c.jid, '@lid', '')
		WHERE c.account_id IN (?) AND c.id IN (?)
		  AND (c.jid LIKE '%@s.whatsapp.net' OR c.jid LIKE '%@g.us' OR c.jid LIKE '%@lid')
		ORDER BY c.account_id, COALESCE(m.pn || '@s.whatsapp.net', c.jid), c.last_time DESC
	`, accountIDs, allChatIDs).Scan(&dedupedIDs).Error; err != nil {
		return nil, 0, err
	}
	if len(dedupedIDs) == 0 {
		return []UnifiedChatRow{}, 0, nil
	}

	// Step 2: build base query with optional filters
	baseQuery := s.db.Table("whatsapp_chats c").
		Select("c.*, p.pinned_at").
		Joins("LEFT JOIN agent_pinned_chats p ON p.chat_id = c.id AND p.agent_id = ?", agentID).
		Where("c.id IN ?", dedupedIDs)

	if search != "" {
		pattern := "%" + search + "%"
		baseQuery = baseQuery.Where("c.name ILIKE ? OR c.jid ILIKE ?", pattern, pattern)
	}
	if archived != nil {
		baseQuery = baseQuery.Where("c.archived = ?", *archived)
	}
	if pinned != nil {
		if *pinned {
			baseQuery = baseQuery.Where("p.pinned_at IS NOT NULL")
		} else {
			baseQuery = baseQuery.Where("p.pinned_at IS NULL")
		}
	}

	// Step 3: count (用 Session 避免 Count 覆蓋 Select clause)
	var total int64
	if err := baseQuery.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Step 4: fetch with pinned-first ordering
	var rows []UnifiedChatRow
	offset := (page - 1) * pageSize
	if err := baseQuery.
		Order("p.pinned_at DESC NULLS LAST, c.last_time DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	// 從 PinnedAt 推導 IsPinned
	for i := range rows {
		rows[i].IsPinned = rows[i].PinnedAt != nil
	}

	if err := s.enrichUnifiedRows(rows); err != nil {
		return nil, 0, err
	}

	return rows, total, nil
}

// getPinnedChats 快速路徑：直接從 agent_pinned_chats 出發，避免全表掃描
func (s *agentOperationsService) getPinnedChats(agentID uint, accountIDs []uint, page, pageSize int, search string, archived *bool, accountID *uint) ([]UnifiedChatRow, int64, error) {
	baseQuery := s.db.Table("whatsapp_chats c").
		Select("c.*, p.pinned_at").
		Joins("INNER JOIN agent_pinned_chats p ON p.chat_id = c.id AND p.agent_id = ?", agentID).
		Where("c.account_id IN ?", accountIDs)

	if search != "" {
		pattern := "%" + search + "%"
		baseQuery = baseQuery.Where("c.name ILIKE ? OR c.jid ILIKE ?", pattern, pattern)
	}
	if archived != nil {
		baseQuery = baseQuery.Where("c.archived = ?", *archived)
	}
	if accountID != nil {
		baseQuery = baseQuery.Where("c.account_id = ?", *accountID)
	}

	var total int64
	if err := baseQuery.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []UnifiedChatRow
	offset := (page - 1) * pageSize
	if err := baseQuery.
		Order("p.pinned_at DESC, c.last_time DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	for i := range rows {
		rows[i].IsPinned = true
	}

	if err := s.enrichUnifiedRows(rows); err != nil {
		return nil, 0, err
	}

	return rows, total, nil
}

// enrichUnifiedRows 批次填充 AccountName、SourceType、SourceAgentName
func (s *agentOperationsService) enrichUnifiedRows(rows []UnifiedChatRow) error {
	if len(rows) == 0 {
		return nil
	}

	accountIDSet := make(map[uint]bool)
	for _, row := range rows {
		accountIDSet[row.AccountID] = true
	}
	uniqueAccountIDs := make([]uint, 0, len(accountIDSet))
	for id := range accountIDSet {
		uniqueAccountIDs = append(uniqueAccountIDs, id)
	}

	type accountSource struct {
		ID                  uint   `gorm:"column:id"`
		PushName            string `gorm:"column:push_name"`
		ChannelID           *uint  `gorm:"column:channel_id"`
		ReferredByAccountID *uint  `gorm:"column:referred_by_account_id"`
	}
	var sources []accountSource
	s.db.Model(&model.WhatsAppAccount{}).
		Select("id, push_name, channel_id, referred_by_account_id").
		Where("id IN ?", uniqueAccountIDs).
		Find(&sources)

	sourceMap := make(map[uint]accountSource)
	for _, src := range sources {
		sourceMap[src.ID] = src
	}

	referredAccountIDs := make([]uint, 0)
	for _, src := range sources {
		if src.ReferredByAccountID != nil {
			referredAccountIDs = append(referredAccountIDs, src.ID)
		}
	}

	type agentNameRow struct {
		NewAccountID uint   `gorm:"column:new_account_id"`
		Username     string `gorm:"column:username"`
	}
	agentNameMap := make(map[uint]string)
	if len(referredAccountIDs) > 0 {
		var agentNames []agentNameRow
		if err := s.db.Raw(`
			SELECT rr.new_account_id, a.username
			FROM referral_registrations rr
			JOIN agents a ON a.id = rr.source_agent_id AND a.deleted_at IS NULL
			WHERE rr.new_account_id IN ? AND rr.source_agent_id IS NOT NULL
		`, referredAccountIDs).Scan(&agentNames).Error; err != nil {
			return fmt.Errorf("查詢推薦來源 agent 失敗: %w", err)
		}
		for _, an := range agentNames {
			agentNameMap[an.NewAccountID] = an.Username
		}
	}

	for i := range rows {
		src, ok := sourceMap[rows[i].AccountID]
		if !ok {
			rows[i].SourceType = "organic"
			continue
		}
		rows[i].AccountName = src.PushName
		if src.ReferredByAccountID != nil {
			rows[i].SourceType = "referral"
			if name, exists := agentNameMap[rows[i].AccountID]; exists {
				rows[i].SourceAgentName = &name
			}
		} else if src.ChannelID != nil {
			rows[i].SourceType = "channel"
		} else {
			rows[i].SourceType = "organic"
		}
	}

	return nil
}

// GetAccountSourceInfo 取得單一帳號的來源資訊
func (s *agentOperationsService) GetAccountSourceInfo(accountID uint) (sourceType string, sourceAgentName *string) {
	var account struct {
		ChannelID           *uint `gorm:"column:channel_id"`
		ReferredByAccountID *uint `gorm:"column:referred_by_account_id"`
	}
	if err := s.db.Model(&model.WhatsAppAccount{}).
		Select("channel_id, referred_by_account_id").
		Where("id = ?", accountID).
		First(&account).Error; err != nil {
		return "organic", nil
	}

	if account.ReferredByAccountID != nil {
		sourceType = "referral"
		var username string
		err := s.db.Raw(`
			SELECT a.username FROM referral_registrations rr
			JOIN agents a ON a.id = rr.source_agent_id AND a.deleted_at IS NULL
			WHERE rr.new_account_id = ? AND rr.source_agent_id IS NOT NULL
			LIMIT 1
		`, accountID).Scan(&username).Error
		if err == nil && username != "" {
			sourceAgentName = &username
		}
		return
	}

	if account.ChannelID != nil {
		return "channel", nil
	}

	return "organic", nil
}

func (s *agentOperationsService) PinChat(agentID, chatID uint) error {
	if err := s.VerifyChatAccess(agentID, chatID); err != nil {
		return err
	}

	// Check limit
	var count int64
	s.db.Model(&model.AgentPinnedChat{}).Where("agent_id = ?", agentID).Count(&count)
	if count >= model.MaxPinnedChats {
		return fmt.Errorf("釘選數量已達上限 (%d)", model.MaxPinnedChats)
	}

	now := time.Now()
	result := s.db.Where("agent_id = ? AND chat_id = ?", agentID, chatID).
		FirstOrCreate(&model.AgentPinnedChat{
			AgentID:  agentID,
			ChatID:   chatID,
			PinnedAt: now,
		})
	return result.Error
}

func (s *agentOperationsService) UnpinChat(agentID, chatID uint) error {
	if err := s.VerifyChatAccess(agentID, chatID); err != nil {
		return err
	}

	return s.db.Where("agent_id = ? AND chat_id = ?", agentID, chatID).
		Delete(&model.AgentPinnedChat{}).Error
}
