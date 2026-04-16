package contact

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"whatsapp_golang/internal/gateway"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	"whatsapp_golang/internal/service/whatsapp"

	"gorm.io/gorm"
)

// RBACServiceInterface RBAC 服務介面（帳號服務用於數據範圍過濾）
type RBACServiceInterface interface {
	GetAdminUserAssignments(adminID uint) (*model.DataScopeConfig, error)
}

// AccountServiceImpl 帳號服務實現 — 直接操作 whatsapp_accounts，不經 users 表
type AccountServiceImpl struct {
	db                *gorm.DB
	gateway           *gateway.Gateway
	rbacService       RBACServiceInterface
	jidMappingService whatsapp.JIDMappingService
}

// NewAccountService 建立帳號服務
func NewAccountService(db *gorm.DB) whatsapp.AccountService {
	return &AccountServiceImpl{
		db: db,
	}
}

// SetGateway 設置 Gateway（用於斷連等操作）
func (s *AccountServiceImpl) SetGateway(gw *gateway.Gateway) {
	s.gateway = gw
}

// SetRBACService 設置 RBAC 服務
func (s *AccountServiceImpl) SetRBACService(svc RBACServiceInterface) {
	s.rbacService = svc
}

// SetJIDMappingService 設置 JID 映射服務
func (s *AccountServiceImpl) SetJIDMappingService(svc whatsapp.JIDMappingService) {
	s.jidMappingService = svc
}

// GetDB 暴露底層 DB
func (s *AccountServiceImpl) GetDB() *gorm.DB {
	return s.db
}

// ---------- ListAccounts ----------

func (s *AccountServiceImpl) ListAccounts(page, pageSize int, filters map[string]interface{}) ([]*model.WhatsAppAccount, int64, error) {
	var total int64
	query := s.db.Model(&model.WhatsAppAccount{}).Where("admin_status != ?", "deleted")

	// --- RBAC: 用戶分配（只查一次） ---
	hasUserAssignment := false
	bypassUserAssignment, _ := filters["bypass_user_assignment"].(bool)
	var dataScope *model.DataScopeConfig
	if !bypassUserAssignment {
		if adminID, ok := filters["admin_id"].(uint); ok && adminID > 0 && s.rbacService != nil {
			var err error
			dataScope, err = s.rbacService.GetAdminUserAssignments(adminID)
			if err != nil {
				logger.Errorw("取得用戶分配範圍失敗", "error", err)
			} else if dataScope != nil && dataScope.Type == "assigned_users" {
				if len(dataScope.AssignedUserIDs) > 0 || (dataScope.UserRangeStart != nil && dataScope.UserRangeEnd != nil) {
					hasUserAssignment = true
				}
			}
		}
	}

	// --- 渠道隔離 ---
	bypassChannelIsolation, _ := filters["bypass_channel_isolation"].(bool)
	if !bypassChannelIsolation && !hasUserAssignment {
		if channelID, ok := filters["channel_id"].(*uint); ok {
			if channelID != nil {
				query = query.Where("channel_id = ?", *channelID)
			} else {
				query = query.Where("1 = 0")
			}
		}
	} else if hasUserAssignment {
		logger.Debugw("用戶有用戶分配權限，跳過渠道隔離")
	}

	// --- 用戶分配隔離 ---
	if !bypassUserAssignment && dataScope != nil && dataScope.Type == "assigned_users" {
		if len(dataScope.AssignedUserIDs) > 0 {
			query = query.Where("id IN ?", dataScope.AssignedUserIDs)
		} else if dataScope.UserRangeStart != nil && dataScope.UserRangeEnd != nil {
			query = query.Where("whatsapp_accounts.id BETWEEN ? AND ?", *dataScope.UserRangeStart, *dataScope.UserRangeEnd)
		} else {
			query = query.Where("1 = 0")
		}
	}

	// --- 過濾條件 ---
	if search, ok := filters["search"].(string); ok && search != "" {
		query = query.Where("phone_number ILIKE ? OR push_name ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if phone, ok := filters["phone"].(string); ok && phone != "" {
		query = query.Where("phone_number ILIKE ?", "%"+phone+"%")
	}
	if status, ok := filters["status"].(string); ok && status != "" {
		query = query.Where("status = ?", status)
	}
	if adminStatus, ok := filters["admin_status"].(string); ok && adminStatus != "" {
		query = query.Where("admin_status = ?", adminStatus)
	}
	if isOnline, ok := filters["is_online"].(*bool); ok && isOnline != nil {
		query = query.Where("is_online = ?", *isOnline)
	}
	if tagID, ok := filters["tag_id"].(*uint); ok && tagID != nil {
		query = query.Where("id IN (SELECT account_id FROM whatsapp_account_tags WHERE tag_id = ?)", *tagID)
	}
	if filterChannelID, ok := filters["filter_channel_id"].(*uint); ok && filterChannelID != nil {
		query = query.Where("channel_id = ?", *filterChannelID)
	}
	if createdFrom, ok := filters["created_from"].(string); ok && createdFrom != "" {
		if t, err := time.Parse("2006-01-02", createdFrom); err == nil {
			query = query.Where("created_at >= ?", t)
		}
	}
	if createdTo, ok := filters["created_to"].(string); ok && createdTo != "" {
		if t, err := time.Parse("2006-01-02", createdTo); err == nil {
			query = query.Where("created_at < ?", t.AddDate(0, 0, 1))
		}
	}

	// --- Count ---
	if err := query.Count(&total).Error; err != nil {
		logger.Errorw("取得帳號總數失敗", "error", err)
		return nil, 0, err
	}

	// --- 排序 ---
	sortBy := "status"
	if sort, ok := filters["sort_by"].(string); ok && sort != "" {
		switch sort {
		case "is_online", "status":
			sortBy = "status"
		case "id", "created_at", "updated_at", "message_count", "phone_number", "push_name", "referral_registered_at":
			sortBy = sort
		default:
			sortBy = "status"
		}
	}

	sortOrder := "desc"
	if order, ok := filters["sort_order"].(string); ok && order != "" {
		sortOrder = order
	}

	// 只在按 message_count 排序時才計算，避免昂貴的 correlated subquery
	if sortBy == "message_count" {
		messageCountExpr := "(SELECT COUNT(*) FROM whatsapp_messages WHERE whatsapp_messages.account_id = whatsapp_accounts.id)"
		query = query.Select("whatsapp_accounts.*, " + messageCountExpr + " AS message_count")
		query = query.Order(messageCountExpr + " " + strings.ToUpper(sortOrder))
	} else if sortBy == "status" {
		query = query.Order("CASE status WHEN 'connected' THEN 3 WHEN 'connecting' THEN 2 WHEN 'disconnected' THEN 1 ELSE 0 END " + strings.ToUpper(sortOrder)).
			Order("created_at DESC")
	} else {
		query = query.Order(sortBy + " " + strings.ToUpper(sortOrder))
	}

	// --- 分頁 ---
	offset := (page - 1) * pageSize
	var accounts []*model.WhatsAppAccount
	if err := query.Offset(offset).Limit(pageSize).Find(&accounts).Error; err != nil {
		logger.Errorw("取得帳號列表失敗", "error", err)
		return nil, 0, err
	}

	// --- Preload Tags ---
	if len(accounts) > 0 {
		ids := make([]uint, len(accounts))
		for i, a := range accounts {
			ids[i] = a.ID
		}
		var tags []struct {
			AccountID uint
			model.AccountTag
		}
		if err := s.db.Table("whatsapp_account_tags").
			Select("whatsapp_account_tags.account_id, account_tags.*").
			Joins("JOIN account_tags ON account_tags.id = whatsapp_account_tags.tag_id").
			Where("whatsapp_account_tags.account_id IN ?", ids).
			Scan(&tags).Error; err != nil {
			logger.Warnw("批量查詢帳號標籤失敗", "error", err)
		} else {
			tagMap := make(map[uint][]model.AccountTag)
			for _, t := range tags {
				tagMap[t.AccountID] = append(tagMap[t.AccountID], t.AccountTag)
			}
			for _, a := range accounts {
				a.Tags = tagMap[a.ID]
			}
		}

		// 填充 ChannelName
		channelIDs := make([]uint, 0)
		for _, a := range accounts {
			if a.ChannelID != nil {
				channelIDs = append(channelIDs, *a.ChannelID)
			}
		}
		if len(channelIDs) > 0 {
			var channels []model.Channel
			if err := s.db.Select("id, channel_name").Where("id IN ?", channelIDs).Find(&channels).Error; err == nil {
				chMap := make(map[uint]string)
				for _, ch := range channels {
					chMap[ch.ID] = ch.ChannelName
				}
				for _, a := range accounts {
					if a.ChannelID != nil {
						a.ChannelName = chMap[*a.ChannelID]
					}
				}
			}
		}

		// 填充 ConnectorName
		connectorIDs := make([]string, 0)
		for _, a := range accounts {
			if a.ConnectorID != "" {
				connectorIDs = append(connectorIDs, a.ConnectorID)
			}
		}
		if len(connectorIDs) > 0 {
			var connectors []struct {
				ID   string
				Name string
			}
			if err := s.db.Model(&model.ConnectorConfig{}).
				Select("id, name").
				Where("id IN ?", connectorIDs).
				Scan(&connectors).Error; err == nil {
				connMap := make(map[string]string)
				for _, c := range connectors {
					connMap[c.ID] = c.Name
				}
				for _, a := range accounts {
					if a.ConnectorID != "" {
						a.ConnectorName = connMap[a.ConnectorID]
					}
				}
			}
		}

		// 填充 WorkgroupName
		var wgas []struct {
			AccountID   uint
			WorkgroupID uint
		}
		if err := s.db.Table("workgroup_accounts").
			Select("account_id, workgroup_id").
			Where("account_id IN ?", ids).
			Scan(&wgas).Error; err == nil && len(wgas) > 0 {
			wgIDs := make([]uint, 0)
			acctWgMap := make(map[uint]uint)
			for _, wa := range wgas {
				acctWgMap[wa.AccountID] = wa.WorkgroupID
				wgIDs = append(wgIDs, wa.WorkgroupID)
			}
			var workgroups []struct {
				ID   uint
				Name string
			}
			if err := s.db.Model(&model.Workgroup{}).
				Select("id, name").
				Where("id IN ?", wgIDs).
				Scan(&workgroups).Error; err == nil {
				wgNameMap := make(map[uint]string)
				for _, wg := range workgroups {
					wgNameMap[wg.ID] = wg.Name
				}
				for _, a := range accounts {
					if wgID, ok := acctWgMap[a.ID]; ok {
						a.WorkgroupName = wgNameMap[wgID]
					}
				}
			}
		}

		// 填充 Source（批次查詢避免 N+1）
		s.batchFillAccountSources(accounts)
	}

	logger.Debugw("成功取得帳號列表", "total", total, "page", page, "page_size", pageSize)
	return accounts, total, nil
}

// ---------- GetAccount ----------

func (s *AccountServiceImpl) GetAccount(id uint) (*model.WhatsAppAccount, error) {
	var account model.WhatsAppAccount
	if err := s.db.Preload("Tags").First(&account, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("帳號不存在")
		}
		logger.Errorw("取得帳號失敗", "id", id, "error", err)
		return nil, err
	}

	if account.ChannelID != nil {
		var channel model.Channel
		if err := s.db.Select("id, channel_name").Where("id = ?", *account.ChannelID).First(&channel).Error; err == nil {
			account.ChannelName = channel.ChannelName
		}
	}

	// 填充 connector_name
	if account.ConnectorID != "" {
		var connectorName string
		if err := s.db.Model(&model.ConnectorConfig{}).
			Select("name").
			Where("id = ?", account.ConnectorID).
			Scan(&connectorName).Error; err == nil && connectorName != "" {
			account.ConnectorName = connectorName
		}
	}

	// 填充 source
	account.Source = s.buildAccountSource(&account)

	// 填充 workgroup_name
	var wga model.WorkgroupAccount
	if err := s.db.Where("account_id = ?", account.ID).First(&wga).Error; err == nil {
		var wgName string
		if err := s.db.Model(&model.Workgroup{}).Select("name").Where("id = ?", wga.WorkgroupID).Scan(&wgName).Error; err == nil {
			account.WorkgroupName = wgName
		}
	}

	return &account, nil
}

// batchFillAccountSources 批次填充帳號來源（替代逐帳號 buildAccountSource 避免 N+1）
func (s *AccountServiceImpl) batchFillAccountSources(accounts []*model.WhatsAppAccount) {
	if len(accounts) == 0 {
		return
	}

	// 分類：referral 帳號 vs channel 帳號
	var referralAccountIDs []uint
	channelIDs := make(map[uint]bool)
	var channelAccounts []*model.WhatsAppAccount
	for _, a := range accounts {
		if a.ReferredByAccountID != nil {
			referralAccountIDs = append(referralAccountIDs, a.ID)
		} else if a.ChannelID != nil {
			channelAccounts = append(channelAccounts, a)
			channelIDs[*a.ChannelID] = true
		}
	}

	// 批次查詢 channel_code
	chCodeMap := make(map[uint]string)
	if len(channelIDs) > 0 {
		ids := make([]uint, 0, len(channelIDs))
		for id := range channelIDs {
			ids = append(ids, id)
		}
		var channels []struct {
			ID          uint
			ChannelCode string
		}
		if err := s.db.Model(&model.Channel{}).Select("id, channel_code").Where("id IN ?", ids).Scan(&channels).Error; err == nil {
			for _, ch := range channels {
				chCodeMap[ch.ID] = ch.ChannelCode
			}
		}
	}
	for _, a := range channelAccounts {
		a.Source = &model.AccountSource{
			SourceType:        "channel",
			ChannelSourceID:   a.ChannelID,
			CaptureMethod:     "pairing_api",
			CapturedAt:        &a.CreatedAt,
			ChannelSourceName: a.ChannelName,
			ChannelSourceKey:  chCodeMap[*a.ChannelID],
		}
	}

	if len(referralAccountIDs) == 0 {
		return
	}

	// 批次查詢 referral_registrations
	var regs []model.ReferralRegistration
	if err := s.db.Where("new_account_id IN ?", referralAccountIDs).Find(&regs).Error; err != nil {
		logger.Warnw("批次查詢裂變記錄失敗", "error", err)
		return
	}

	regMap := make(map[uint]*model.ReferralRegistration)
	sourceAccountIDs := make([]uint, 0)
	agentIDs := make([]uint, 0)
	for i := range regs {
		regMap[regs[i].NewAccountID] = &regs[i]
		sourceAccountIDs = append(sourceAccountIDs, regs[i].SourceAccountID)
		if regs[i].SourceAgentID != nil {
			agentIDs = append(agentIDs, *regs[i].SourceAgentID)
		}
	}

	// 批次查詢來源帳號
	type srcInfo struct {
		ID          uint
		PhoneNumber string
		PushName    string
		FullName    string
	}
	srcMap := make(map[uint]*srcInfo)
	if len(sourceAccountIDs) > 0 {
		var srcs []srcInfo
		if err := s.db.Model(&model.WhatsAppAccount{}).
			Select("id, phone_number, push_name, full_name").
			Where("id IN ?", sourceAccountIDs).
			Scan(&srcs).Error; err == nil {
			for i := range srcs {
				srcMap[srcs[i].ID] = &srcs[i]
			}
		}
	}

	// 批次查詢 agent names
	agentNameMap := make(map[uint]string)
	if len(agentIDs) > 0 {
		var agents []struct {
			ID       uint
			Username string
		}
		if err := s.db.Model(&model.Agent{}).
			Select("id, username").
			Where("id IN ?", agentIDs).
			Scan(&agents).Error; err == nil {
			for _, ag := range agents {
				agentNameMap[ag.ID] = ag.Username
			}
		}
	}

	// 組裝
	for _, a := range accounts {
		if a.ReferredByAccountID == nil {
			continue
		}
		source := &model.AccountSource{
			SourceType:    "referral",
			CapturedAt:    a.ReferralRegisteredAt,
			CaptureMethod: "pairing_api",
		}
		if reg, ok := regMap[a.ID]; ok {
			source.ReferralCode = reg.ReferralCode
			source.SourceAccountID = &reg.SourceAccountID
			if src, ok := srcMap[reg.SourceAccountID]; ok {
				source.SourceAccountPhone = src.PhoneNumber
				if src.PushName != "" {
					source.SourceAccountName = src.PushName
				} else {
					source.SourceAccountName = src.FullName
				}
			}
			if reg.SourceAgentID != nil {
				source.SourceAgentID = reg.SourceAgentID
				source.SourceAgentName = agentNameMap[*reg.SourceAgentID]
			}
		}
		a.Source = source
	}
}

// buildAccountSource 組裝帳號來源歸因資料
func (s *AccountServiceImpl) buildAccountSource(account *model.WhatsAppAccount) *model.AccountSource {
	// 裂變來源
	if account.ReferredByAccountID != nil {
		source := &model.AccountSource{
			SourceType:    "referral",
			CapturedAt:    account.ReferralRegisteredAt,
			CaptureMethod: "pairing_api",
		}

		var reg model.ReferralRegistration
		if err := s.db.Where("new_account_id = ?", account.ID).First(&reg).Error; err == nil {
			source.ReferralCode = reg.ReferralCode
			source.SourceAccountID = &reg.SourceAccountID

			var srcAccount struct {
				PhoneNumber string
				PushName    string
				FullName    string
			}
			if err := s.db.Model(&model.WhatsAppAccount{}).
				Select("phone_number, push_name, full_name").
				Where("id = ?", reg.SourceAccountID).
				Scan(&srcAccount).Error; err == nil {
				source.SourceAccountPhone = srcAccount.PhoneNumber
				if srcAccount.PushName != "" {
					source.SourceAccountName = srcAccount.PushName
				} else {
					source.SourceAccountName = srcAccount.FullName
				}
			}

			// 填充裂變來源組員
			if reg.SourceAgentID != nil {
				source.SourceAgentID = reg.SourceAgentID
				var agentName string
				if err := s.db.Model(&model.Agent{}).Select("username").Where("id = ?", *reg.SourceAgentID).Scan(&agentName).Error; err == nil {
					source.SourceAgentName = agentName
				}
			}
		}

		return source
	}

	// 渠道來源
	if account.ChannelID != nil {
		source := &model.AccountSource{
			SourceType:      "channel",
			ChannelSourceID: account.ChannelID,
			CaptureMethod:   "pairing_api",
			CapturedAt:      &account.CreatedAt,
		}

		var channel model.Channel
		if err := s.db.Select("id, channel_code, channel_name").
			Where("id = ?", *account.ChannelID).
			First(&channel).Error; err == nil {
			source.ChannelSourceKey = channel.ChannelCode
			source.ChannelSourceName = channel.ChannelName
		}

		return source
	}

	// 無來源
	return nil
}

// ---------- GetAccountChats ----------

// deduplicatedChatIDs 回傳去重後的 chat ID 列表
func (s *AccountServiceImpl) deduplicatedChatIDs(accountID uint) ([]uint, error) {
	var chatIDs []uint
	if err := s.db.Model(&model.WhatsAppMessage{}).
		Select("DISTINCT chat_id").
		Where("account_id = ?", accountID).
		Pluck("chat_id", &chatIDs).Error; err != nil {
		return nil, err
	}
	if len(chatIDs) == 0 {
		return nil, nil
	}

	var deduped []uint
	if err := s.db.Raw(`
		SELECT DISTINCT ON (c.account_id, COALESCE(m.pn || '@s.whatsapp.net', c.jid)) c.id
		FROM whatsapp_chats c
		LEFT JOIN whatsmeow_lid_map m ON c.jid LIKE '%@lid' AND m.lid = REPLACE(c.jid, '@lid', '')
		WHERE c.account_id = ? AND c.id IN (?) AND (c.jid LIKE '%@s.whatsapp.net' OR c.jid LIKE '%@g.us' OR c.jid LIKE '%@lid')
		ORDER BY c.account_id, COALESCE(m.pn || '@s.whatsapp.net', c.jid), c.last_time DESC
	`, accountID, chatIDs).Scan(&deduped).Error; err != nil {
		return nil, err
	}
	return deduped, nil
}

func (s *AccountServiceImpl) GetAccountChats(id uint, page, pageSize int, search string, archived *bool) ([]*model.WhatsAppChat, int64, error) {
	deduplicatedIDs, err := s.deduplicatedChatIDs(id)
	if err != nil {
		return nil, 0, err
	}
	if len(deduplicatedIDs) == 0 {
		return []*model.WhatsAppChat{}, 0, nil
	}

	baseQuery := s.db.Model(&model.WhatsAppChat{}).Where("id IN ?", deduplicatedIDs)

	if search != "" {
		searchPattern := "%" + search + "%"
		baseQuery = baseQuery.Where("name ILIKE ? OR jid ILIKE ?", searchPattern, searchPattern)
	}

	if archived != nil {
		baseQuery = baseQuery.Where("archived = ?", *archived)
	}

	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		logger.Errorw("計算聊天總數失敗", "error", err)
		return nil, 0, err
	}

	var chats []*model.WhatsAppChat
	offset := (page - 1) * pageSize
	if err := baseQuery.
		Order("last_time DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&chats).Error; err != nil {
		logger.Errorw("取得聊天列表失敗", "error", err)
		return nil, 0, err
	}

	return chats, total, nil
}

// ---------- GetAccountChatCounts ----------

func (s *AccountServiceImpl) GetAccountChatCounts(id uint) (*model.ChatCounts, error) {
	deduped, err := s.deduplicatedChatIDs(id)
	if err != nil {
		return nil, err
	}
	if len(deduped) == 0 {
		return &model.ChatCounts{}, nil
	}

	var counts model.ChatCounts
	s.db.Model(&model.WhatsAppChat{}).Where("id IN ?", deduped).
		Count(&counts.Total)
	s.db.Model(&model.WhatsAppChat{}).Where("id IN ? AND archived = true", deduped).
		Count(&counts.Archived)
	counts.Unarchived = counts.Total - counts.Archived
	return &counts, nil
}

// ---------- CreateAccount ----------

func (s *AccountServiceImpl) CreateAccount(account *model.WhatsAppAccount) (*model.WhatsAppAccount, error) {
	if account.AdminStatus == "" {
		account.AdminStatus = "active"
	}
	if err := s.db.Create(account).Error; err != nil {
		logger.Errorw("建立帳號失敗", "error", err)
		return nil, err
	}
	logger.Infow("成功建立帳號", "id", account.ID, "phone", account.PhoneNumber)
	return account, nil
}

// ---------- UpdateAccount ----------

func (s *AccountServiceImpl) UpdateAccount(id uint, updates map[string]interface{}) (*model.WhatsAppAccount, error) {
	var account model.WhatsAppAccount
	if err := s.db.First(&account, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("帳號不存在")
		}
		return nil, err
	}

	updates["updated_at"] = time.Now()
	if err := s.db.Model(&model.WhatsAppAccount{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		logger.Errorw("更新帳號失敗", "id", id, "error", err)
		return nil, err
	}

	// 重新載入含 Tags
	if err := s.db.Preload("Tags").First(&account, id).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

// ---------- DeleteAccount ----------

func (s *AccountServiceImpl) DeleteAccount(id uint) error {
	var account model.WhatsAppAccount
	if err := s.db.First(&account, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("帳號不存在")
		}
		return err
	}

	// 標記 admin_status = "deleted"，清除裂變與渠道資訊
	if err := s.db.Model(&model.WhatsAppAccount{}).Where("id = ?", id).Updates(map[string]interface{}{
		"admin_status":           "deleted",
		"referred_by_account_id": nil,
		"referral_registered_at": nil,
		"channel_id":             nil,
		"channel_source":         nil,
	}).Error; err != nil {
		logger.Errorw("標記帳號為 deleted 失敗", "id", id, "error", err)
		return err
	}

	// 刪除裂變記錄，讓帳號重新配對時可重新綁定
	if err := s.db.Where("new_account_id = ?", id).Delete(&model.ReferralRegistration{}).Error; err != nil {
		logger.Warnw("刪除裂變記錄失敗（不影響刪除）", "id", id, "error", err)
	}

	logger.Infow("已標記帳號 admin_status=deleted", "id", id)

	// 斷連裝置（非阻塞，失敗僅 log）
	if s.gateway != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.gateway.DisconnectAccount(ctx, id); err != nil {
			logger.Warnw("斷連裝置失敗（不影響刪除）", "id", id, "error", err)
		} else {
			logger.Infow("已斷連裝置", "id", id)
		}
	}

	return nil
}

// ---------- BatchOperation ----------

func (s *AccountServiceImpl) BatchOperation(accountIDs []uint, operation string, data map[string]interface{}) (int, []map[string]interface{}, error) {
	var affectedCount int
	var errs []map[string]interface{}

	for _, accountID := range accountIDs {
		switch operation {
		case "delete":
			if err := s.DeleteAccount(accountID); err != nil {
				errs = append(errs, map[string]interface{}{
					"account_id": accountID,
					"error":      err.Error(),
				})
			} else {
				affectedCount++
			}

		case "update":
			if _, err := s.UpdateAccount(accountID, data); err != nil {
				errs = append(errs, map[string]interface{}{
					"account_id": accountID,
					"error":      err.Error(),
				})
			} else {
				affectedCount++
			}

		default:
			errs = append(errs, map[string]interface{}{
				"account_id": accountID,
				"error":      "不支持的操作: " + operation,
			})
		}
	}

	logger.Infow("批量操作完成", "operation", operation, "affected_count", affectedCount, "error_count", len(errs))
	return affectedCount, errs, nil
}

// ---------- GetAccountStats ----------

func (s *AccountServiceImpl) GetAccountStats(filters map[string]interface{}) (*whatsapp.AccountStats, error) {
	stats := &whatsapp.AccountStats{}

	baseQuery := s.db.Model(&model.WhatsAppAccount{}).Where("admin_status != ?", "deleted")

	// 渠道隔離
	bypassChannelIsolation, _ := filters["bypass_channel_isolation"].(bool)
	if !bypassChannelIsolation {
		if channelID, ok := filters["channel_id"].(*uint); ok {
			if channelID != nil {
				baseQuery = baseQuery.Where("channel_id = ?", *channelID)
			} else {
				baseQuery = baseQuery.Where("1 = 0")
			}
		}
	}

	// Total
	if err := baseQuery.Session(&gorm.Session{}).Count(&stats.Total).Error; err != nil {
		logger.Errorw("取得帳號總數失敗", "error", err)
		return nil, err
	}

	// Active (admin_status = 'active')
	if err := baseQuery.Session(&gorm.Session{}).Where("admin_status = ?", "active").Count(&stats.Active).Error; err != nil {
		logger.Errorw("取得 active 帳號數失敗", "error", err)
		return nil, err
	}

	// Online
	if err := baseQuery.Session(&gorm.Session{}).Where("is_online = ?", true).Count(&stats.Online).Error; err != nil {
		logger.Errorw("取得在線帳號數失敗", "error", err)
		return nil, err
	}

	// Connected
	if err := baseQuery.Session(&gorm.Session{}).Where("status = ?", "connected").Count(&stats.Connected).Error; err != nil {
		logger.Errorw("取得 connected 帳號數失敗", "error", err)
		return nil, err
	}

	// Disconnected
	if err := baseQuery.Session(&gorm.Session{}).Where("status = ?", "disconnected").Count(&stats.Disconnected).Error; err != nil {
		logger.Errorw("取得 disconnected 帳號數失敗", "error", err)
		return nil, err
	}

	// LoggedOut
	if err := baseQuery.Session(&gorm.Session{}).Where("status = ?", "logged_out").Count(&stats.LoggedOut).Error; err != nil {
		logger.Errorw("取得 logged_out 帳號數失敗", "error", err)
		return nil, err
	}

	return stats, nil
}

// ---------- GetAccountIDRange ----------

func (s *AccountServiceImpl) GetAccountIDRange(filters map[string]interface{}) (map[string]interface{}, error) {
	var minID, maxID *uint
	var total int64

	baseQuery := s.db.Model(&model.WhatsAppAccount{}).Where("admin_status != ?", "deleted")

	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	if total == 0 {
		return map[string]interface{}{
			"min_id": 0,
			"max_id": 0,
			"total":  0,
		}, nil
	}

	s.db.Model(&model.WhatsAppAccount{}).Where("admin_status != ?", "deleted").Select("MIN(id)").Scan(&minID)
	s.db.Model(&model.WhatsAppAccount{}).Where("admin_status != ?", "deleted").Select("MAX(id)").Scan(&maxID)

	result := map[string]interface{}{
		"total": total,
	}
	if minID != nil {
		result["min_id"] = *minID
	} else {
		result["min_id"] = 0
	}
	if maxID != nil {
		result["max_id"] = *maxID
	} else {
		result["max_id"] = 0
	}

	return result, nil
}

// ---------- GetConversationHistory ----------

func (s *AccountServiceImpl) GetConversationHistory(accountID uint, contactPhone string, page, limit int, targetLanguage string) ([]*model.MessageWithSender, int64, error) {
	var account model.WhatsAppAccount
	if err := s.db.First(&account, accountID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, 0, fmt.Errorf("帳號不存在")
		}
		logger.Errorw("取得帳號失敗", "error", err)
		return nil, 0, err
	}

	if account.PhoneNumber == "" {
		return nil, 0, fmt.Errorf("帳號手機號為空")
	}

	// 取得所有等效 JID（包含原始 JID 和映射的 JID）
	jids := []string{contactPhone}
	if s.jidMappingService != nil {
		jids = s.jidMappingService.GetAlternativeJIDs(accountID, contactPhone)
	}

	// 查找任一等效 JID 對應的 chat
	var targetChats []model.WhatsAppChat
	if err := s.db.Where("account_id = ? AND jid IN ?", accountID, jids).Find(&targetChats).Error; err != nil {
		logger.Errorw("查詢聊天記錄失敗", "error", err)
		return nil, 0, err
	}

	if len(targetChats) == 0 {
		return []*model.MessageWithSender{}, 0, nil
	}

	chatIDs := make([]uint, len(targetChats))
	for i, c := range targetChats {
		chatIDs[i] = c.ID
	}

	// 通過 chat_id 查詢訊息
	query := s.db.Model(&model.WhatsAppMessage{}).Where("account_id = ? AND chat_id IN ?", accountID, chatIDs)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Errorw("取得會話訊息總數失敗", "error", err)
		return nil, 0, err
	}

	var messages []*model.WhatsAppMessage
	offset := (page - 1) * limit
	if err := query.Order("timestamp DESC").Offset(offset).Limit(limit).Find(&messages).Error; err != nil {
		logger.Errorw("取得會話訊息失敗", "error", err)
		return nil, 0, err
	}

	// 批量查詢發送者資訊
	senderJIDs := make([]string, 0)
	jidSet := make(map[string]bool)
	for _, msg := range messages {
		if !jidSet[msg.FromJID] {
			senderJIDs = append(senderJIDs, msg.FromJID)
			jidSet[msg.FromJID] = true
		}
	}

	var chats []*model.WhatsAppChat
	if len(senderJIDs) > 0 {
		s.db.Where("account_id = ? AND jid IN ?", accountID, senderJIDs).Find(&chats)
	}

	chatMap := make(map[string]*model.WhatsAppChat)
	for _, chat := range chats {
		chatMap[chat.JID] = chat
	}

	messagesWithSender := make([]*model.MessageWithSender, len(messages))
	for i, msg := range messages {
		sender := &model.MessageSender{JID: msg.FromJID}

		if msg.IsFromMe {
			sender.Name = "我"
			sender.Phone = account.PhoneNumber
		} else if chat, exists := chatMap[msg.FromJID]; exists {
			sender.Name = chat.Name
			if strings.Contains(msg.FromJID, "@s.whatsapp.net") {
				sender.Phone = strings.Replace(msg.FromJID, "@s.whatsapp.net", "", 1)
			} else {
				sender.Phone = msg.FromJID
			}
		} else {
			if strings.Contains(msg.FromJID, "@s.whatsapp.net") {
				sender.Phone = strings.Replace(msg.FromJID, "@s.whatsapp.net", "", 1)
				sender.Name = sender.Phone
			} else {
				sender.Name = msg.FromJID
				sender.Phone = msg.FromJID
			}
		}

		messagesWithSender[i] = &model.MessageWithSender{
			WhatsAppMessage: *msg,
			Sender:          sender,
		}
	}

	if targetLanguage != "" {
		s.batchFillTranslations(messagesWithSender, targetLanguage)
	}

	return messagesWithSender, total, nil
}

// batchFillTranslations 批量查詢翻譯快取並填充到訊息中
func (s *AccountServiceImpl) batchFillTranslations(messages []*model.MessageWithSender, targetLang string) {
	if len(messages) == 0 || targetLang == "" {
		return
	}

	type contentHash struct {
		hash    string
		indexes []int
	}

	hashMap := make(map[string]*contentHash)
	hashes := make([]string, 0)

	for i, msg := range messages {
		if msg.Type == "text" && msg.Content != "" {
			hash := md5.Sum([]byte(msg.Content))
			hashStr := hex.EncodeToString(hash[:])

			if ch, exists := hashMap[hashStr]; exists {
				ch.indexes = append(ch.indexes, i)
			} else {
				hashMap[hashStr] = &contentHash{
					hash:    hashStr,
					indexes: []int{i},
				}
				hashes = append(hashes, hashStr)
			}
		}
	}

	if len(hashes) == 0 {
		return
	}

	var caches []model.TranslationCache
	if err := s.db.Where("content_hash IN ? AND target_lang = ?", hashes, targetLang).
		Find(&caches).Error; err != nil {
		logger.Errorw("批量查詢翻譯快取失敗", "error", err)
		return
	}

	translationMap := make(map[string]string)
	for _, cache := range caches {
		translationMap[cache.ContentHash] = cache.TranslatedText
	}

	for hash, ch := range hashMap {
		if translatedText, exists := translationMap[hash]; exists {
			for _, idx := range ch.indexes {
				messages[idx].TranslatedText = translatedText
			}
		}
	}
}

// ---------- ClearUnreadCount ----------

func (s *AccountServiceImpl) ClearUnreadCount(accountID uint, contactPhone string) error {
	jids := []string{contactPhone}
	if s.jidMappingService != nil {
		jids = s.jidMappingService.GetAlternativeJIDs(accountID, contactPhone)
	}

	return s.db.Model(&model.WhatsAppChat{}).
		Where("account_id = ? AND jid IN ?", accountID, jids).
		Update("unread_count", 0).Error
}

// ---------- GetDisconnectStats ----------

func (s *AccountServiceImpl) GetDisconnectStats(filters map[string]interface{}) (*whatsapp.DisconnectStats, error) {
	baseQuery := s.db.Model(&model.WhatsAppAccount{}).Where("admin_status != ?", "deleted")

	bypassChannelIsolation, _ := filters["bypass_channel_isolation"].(bool)
	if !bypassChannelIsolation {
		if channelID, ok := filters["channel_id"].(*uint); ok {
			if channelID != nil {
				baseQuery = baseQuery.Where("channel_id = ?", *channelID)
			} else {
				baseQuery = baseQuery.Where("1 = 0")
			}
		}
	}

	if period, ok := filters["period"].(string); ok && period != "all" {
		var days int
		switch period {
		case "7d":
			days = 7
		case "30d":
			days = 30
		case "90d":
			days = 90
		}
		if days > 0 {
			baseQuery = baseQuery.Where("created_at >= ?", time.Now().AddDate(0, 0, -days))
		}
	}

	var totalDisconnected, neverDisconnected int64
	baseQuery.Session(&gorm.Session{}).Where("logged_out_at IS NOT NULL").Count(&totalDisconnected)
	baseQuery.Session(&gorm.Session{}).Where("logged_out_at IS NULL").Count(&neverDisconnected)

	// PostgreSQL: EXTRACT(EPOCH FROM (logged_out_at - created_at))
	buckets := []struct {
		label   string
		daysMin *float64
		daysMax *float64
		cond    string
	}{
		{label: "< 1天", daysMax: ptr(1.0), cond: "EXTRACT(EPOCH FROM (logged_out_at - created_at)) < 86400"},
		{label: "1-3天", daysMin: ptr(1.0), daysMax: ptr(3.0), cond: "EXTRACT(EPOCH FROM (logged_out_at - created_at)) >= 86400 AND EXTRACT(EPOCH FROM (logged_out_at - created_at)) < 259200"},
		{label: "3-7天", daysMin: ptr(3.0), daysMax: ptr(7.0), cond: "EXTRACT(EPOCH FROM (logged_out_at - created_at)) >= 259200 AND EXTRACT(EPOCH FROM (logged_out_at - created_at)) < 604800"},
		{label: "7-14天", daysMin: ptr(7.0), daysMax: ptr(14.0), cond: "EXTRACT(EPOCH FROM (logged_out_at - created_at)) >= 604800 AND EXTRACT(EPOCH FROM (logged_out_at - created_at)) < 1209600"},
		{label: "14-30天", daysMin: ptr(14.0), daysMax: ptr(30.0), cond: "EXTRACT(EPOCH FROM (logged_out_at - created_at)) >= 1209600 AND EXTRACT(EPOCH FROM (logged_out_at - created_at)) < 2592000"},
		{label: "> 30天", daysMin: ptr(30.0), cond: "EXTRACT(EPOCH FROM (logged_out_at - created_at)) >= 2592000"},
	}

	distribution := make([]whatsapp.DisconnectStatsBucket, 0, len(buckets))
	for _, b := range buckets {
		var count int64
		baseQuery.Session(&gorm.Session{}).Where("logged_out_at IS NOT NULL").Where(b.cond).Count(&count)
		pct := 0.0
		if totalDisconnected > 0 {
			pct = float64(count) / float64(totalDisconnected) * 100
		}
		distribution = append(distribution, whatsapp.DisconnectStatsBucket{
			Label:      b.label,
			DaysMin:    b.daysMin,
			DaysMax:    b.daysMax,
			Count:      count,
			Percentage: pct,
		})
	}

	return &whatsapp.DisconnectStats{
		Distribution:      distribution,
		TotalDisconnected: totalDisconnected,
		NeverDisconnected: neverDisconnected,
	}, nil
}

func ptr(v float64) *float64 { return &v }
