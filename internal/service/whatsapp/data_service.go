package whatsapp

import (
	"whatsapp_golang/internal/database"
	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// AccountQueryParams 帳號查詢參數
type AccountQueryParams struct {
	Page     int    // 頁碼（從 1 開始）
	PageSize int    // 每頁數量
	Phone    string // 手機號碼篩選（支持部分匹配）
	Status   string // 狀態篩選：connected, disconnected, logged_out
}

// AccountQueryResult 帳號查詢結果（分頁）
type AccountQueryResult struct {
	Items    []*model.WhatsAppAccount `json:"items"`
	Total    int64                    `json:"total"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"page_size"`
}

// DataService 純 DB 查詢服務介面
// 不涉及 WhatsApp 連線操作，僅用於資料查詢和簡單的 DB 操作
type DataService interface {
	// 帳號查詢
	GetAccounts(params *AccountQueryParams) (*AccountQueryResult, error)
	GetAccount(id uint) (*model.WhatsAppAccount, error)

	// 聊天查詢
	GetChats(accountID uint) ([]*model.WhatsAppChat, error)
	GetChatByID(chatID uint) (*model.WhatsAppChat, error)
	GetContacts(accountID uint, page, pageSize int) ([]*model.WhatsAppContact, int64, error)

	// 聊天操作（純 DB 操作，不呼叫 WhatsApp API）
	ArchiveChatByID(chatID uint) (*model.WhatsAppChat, error)
	UnarchiveChatByID(chatID uint) (*model.WhatsAppChat, error)

	// 帳號更新
	UpdateAccountAIAnalysis(id uint, enabled bool) error
	UpdateAccountSettings(id uint, updates map[string]interface{}) error

	// 同步狀態
	GetSyncStatusService() SyncStatusService
}

// dataService DataService 實現
type dataService struct {
	db                *gorm.DB
	syncStatusService SyncStatusService
}

// NewDataService 建立 DataService
func NewDataService(db database.Database) DataService {
	gormDB := db.GetDB()
	return &dataService{
		db:                gormDB,
		syncStatusService: NewSyncStatusService(gormDB),
	}
}

// NewDataServiceFromGorm 從 gorm.DB 建立 DataService
func NewDataServiceFromGorm(db *gorm.DB) DataService {
	return &dataService{
		db:                db,
		syncStatusService: NewSyncStatusService(db),
	}
}

// GetAccounts 獲取帳號列表（支持分頁和篩選）
func (s *dataService) GetAccounts(params *AccountQueryParams) (*AccountQueryResult, error) {
	// 如果沒有傳參數，使用默認值（向後兼容）
	if params == nil {
		params = &AccountQueryParams{
			Page:     1,
			PageSize: 100, // 默認分頁大小
		}
	}

	// 設置默認值
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 {
		params.PageSize = 50
	}
	if params.PageSize > 500 {
		params.PageSize = 500 // 限制最大值
	}

	query := s.db.Model(&model.WhatsAppAccount{}).Where("status != ?", "deleted")

	// 手機號篩選
	if params.Phone != "" {
		query = query.Where("phone_number LIKE ?", "%"+params.Phone+"%")
	}

	// 狀態篩選
	if params.Status != "" {
		query = query.Where("status = ?", params.Status)
	}

	// 計算總數
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	// 分頁查詢
	var accounts []*model.WhatsAppAccount
	offset := (params.Page - 1) * params.PageSize
	if err := query.
		Order("id DESC").
		Offset(offset).
		Limit(params.PageSize).
		Find(&accounts).Error; err != nil {
		return nil, err
	}

	return &AccountQueryResult{
		Items:    accounts,
		Total:    total,
		Page:     params.Page,
		PageSize: params.PageSize,
	}, nil
}

// GetAccount 獲取單個帳號
func (s *dataService) GetAccount(id uint) (*model.WhatsAppAccount, error) {
	var account model.WhatsAppAccount
	err := s.db.First(&account, id).Error
	return &account, err
}

// GetChats 獲取聊天列表
func (s *dataService) GetChats(accountID uint) ([]*model.WhatsAppChat, error) {
	var chats []*model.WhatsAppChat
	err := s.db.Where("account_id = ?", accountID).Order("last_time DESC").Find(&chats).Error
	return chats, err
}

// GetChatByID 根據 ID 獲取單個聊天
func (s *dataService) GetChatByID(chatID uint) (*model.WhatsAppChat, error) {
	var chat model.WhatsAppChat
	err := s.db.First(&chat, chatID).Error
	return &chat, err
}

// GetContacts 獲取帳號的聯絡人列表（從 whatsmeow_contacts 表讀取）
func (s *dataService) GetContacts(accountID uint, page, pageSize int) ([]*model.WhatsAppContact, int64, error) {
	// 1. 取得帳號的 DeviceID
	var account model.WhatsAppAccount
	if err := s.db.Select("device_id").Where("id = ?", accountID).First(&account).Error; err != nil {
		return nil, 0, err
	}

	if account.DeviceID == "" {
		return []*model.WhatsAppContact{}, 0, nil
	}

	// 2. 使用 DISTINCT ON 去重：LID 和 PhoneJID 指向同一人時只回傳一筆
	type wmContact struct {
		TheirJID string `gorm:"column:their_jid"`
		PushName string `gorm:"column:push_name"`
		FullName string `gorm:"column:full_name"`
		Phone    string `gorm:"column:phone"`
	}

	var total int64
	countSQL := `
		SELECT COUNT(*) FROM (
			SELECT DISTINCT ON (COALESCE(m.pn || '@s.whatsapp.net', c.their_jid)) c.their_jid
			FROM whatsmeow_contacts c
			LEFT JOIN whatsmeow_lid_map m ON c.their_jid LIKE '%@lid' AND m.lid = REPLACE(c.their_jid, '@lid', '')
			WHERE c.our_jid = ?
		) sub
	`
	if err := s.db.Raw(countSQL, account.DeviceID).Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	var wmContacts []wmContact
	offset := (page - 1) * pageSize
	querySQL := `
		SELECT DISTINCT ON (COALESCE(m.pn || '@s.whatsapp.net', c.their_jid))
			c.their_jid, c.push_name, c.full_name,
			COALESCE(m.pn, CASE WHEN c.their_jid LIKE '%@s.whatsapp.net' THEN REPLACE(c.their_jid, '@s.whatsapp.net', '') ELSE '' END) AS phone
		FROM whatsmeow_contacts c
		LEFT JOIN whatsmeow_lid_map m ON c.their_jid LIKE '%@lid' AND m.lid = REPLACE(c.their_jid, '@lid', '')
		WHERE c.our_jid = ?
		ORDER BY COALESCE(m.pn || '@s.whatsapp.net', c.their_jid), c.push_name ASC
		OFFSET ? LIMIT ?
	`
	if err := s.db.Raw(querySQL, account.DeviceID, offset, pageSize).Scan(&wmContacts).Error; err != nil {
		return nil, 0, err
	}

	// 3. 轉換為 WhatsAppContact DTO
	contacts := make([]*model.WhatsAppContact, 0, len(wmContacts))
	for _, wc := range wmContacts {
		name := wc.PushName
		if name == "" {
			name = wc.FullName
		}

		contacts = append(contacts, &model.WhatsAppContact{
			AccountID: accountID,
			JID:       wc.TheirJID,
			Phone:     wc.Phone,
			PushName:  name,
			FullName:  wc.FullName,
		})
	}

	return contacts, total, nil
}

// ArchiveChatByID 通過會話 ID 歸檔會話（純 DB 操作）
func (s *dataService) ArchiveChatByID(chatID uint) (*model.WhatsAppChat, error) {
	var chat model.WhatsAppChat
	if err := s.db.First(&chat, chatID).Error; err != nil {
		return nil, err
	}

	// 更新歸檔狀態
	chat.Archived = true
	if err := s.db.Save(&chat).Error; err != nil {
		return nil, err
	}

	return &chat, nil
}

// UnarchiveChatByID 通過會話 ID 取消歸檔會話（純 DB 操作）
func (s *dataService) UnarchiveChatByID(chatID uint) (*model.WhatsAppChat, error) {
	var chat model.WhatsAppChat
	if err := s.db.First(&chat, chatID).Error; err != nil {
		return nil, err
	}

	// 更新歸檔狀態
	chat.Archived = false
	chat.ArchivedAt = nil
	if err := s.db.Save(&chat).Error; err != nil {
		return nil, err
	}

	return &chat, nil
}

// UpdateAccountAIAnalysis 更新帳號的 AI 分析開關
func (s *dataService) UpdateAccountAIAnalysis(id uint, enabled bool) error {
	return s.db.Model(&model.WhatsAppAccount{}).Where("id = ?", id).Update("ai_analysis_enabled", enabled).Error
}

// UpdateAccountSettings 批量更新帳號設定欄位
func (s *dataService) UpdateAccountSettings(id uint, updates map[string]interface{}) error {
	return s.db.Model(&model.WhatsAppAccount{}).Where("id = ?", id).Updates(updates).Error
}

// GetSyncStatusService 獲取同步狀態服務
func (s *dataService) GetSyncStatusService() SyncStatusService {
	return s.syncStatusService
}
