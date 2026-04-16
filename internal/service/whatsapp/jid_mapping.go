package whatsapp

import (
	"strings"

	"gorm.io/gorm"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
)

// WhatsmeowLIDMap 對應 whatsmeow 的 lid_map 表
type WhatsmeowLIDMap struct {
	LID string `gorm:"column:lid;primaryKey"`
	PN  string `gorm:"column:pn;uniqueIndex"`
}

func (WhatsmeowLIDMap) TableName() string {
	return "whatsmeow_lid_map"
}

// JIDMappingService 管理 LID ↔ PhoneJID 映射接口
type JIDMappingService interface {
	SaveMapping(accountID uint, senderJID, senderAltJID string) error
	GetCanonicalJID(accountID uint, jid string) string
	GetPhoneJID(accountID uint, jid string) string
	GetAlternativeJIDs(accountID uint, jid string) []string
	FindExistingChat(db *gorm.DB, accountID uint, jid string) *model.WhatsAppChat
	GetOrCreateChat(db *gorm.DB, accountID uint, jid, name string, isGroup bool) (*model.WhatsAppChat, error)
	ReconcileDuplicateChats(db *gorm.DB, accountID uint) int
	ResolvePhoneJID(jid string) string
}

// jidMappingService 管理 LID ↔ PhoneJID 映射實現
// 使用 whatsmeow 內建的 whatsmeow_lid_map 表
type jidMappingService struct {
	db *gorm.DB
}

// NewJIDMappingService 建立 JIDMappingService
func NewJIDMappingService(db *gorm.DB) JIDMappingService {
	return &jidMappingService{db: db}
}

// SaveMapping 儲存 LID ↔ PhoneJID 映射（現在不需要，whatsmeow 自動維護）
func (s *jidMappingService) SaveMapping(accountID uint, senderJID, senderAltJID string) error {
	// whatsmeow 會自動維護 whatsmeow_lid_map 表，不需要我們手動儲存
	logger.Debugw("JID 映射由 whatsmeow 自動管理",
		"sender", senderJID, "alt", senderAltJID)
	return nil
}

// GetCanonicalJID 取得規範 JID（優先返回 LID）
// 如果傳入的是 PhoneJID 且有對應的 LID 映射，返回 LID
// 否則返回原始 JID
func (s *jidMappingService) GetCanonicalJID(accountID uint, jid string) string {
	if jid == "" {
		return jid
	}

	// 如果已經是 LID，直接返回
	if IsLID(jid) {
		return jid
	}

	// 如果是 PhoneJID，嘗試從 whatsmeow_lid_map 查找對應的 LID
	if IsPhoneJID(jid) {
		pn := ExtractJIDUser(jid) // 提取電話號碼部分
		var mapping WhatsmeowLIDMap
		if err := s.db.Where("pn = ?", pn).First(&mapping).Error; err == nil && mapping.LID != "" {
			return mapping.LID + "@lid"
		}
	}

	return jid
}

// GetPhoneJID 取得 PhoneJID
// 如果傳入的是 LID 且有對應的 PhoneJID 映射，返回 PhoneJID
// 否則返回原始 JID
func (s *jidMappingService) GetPhoneJID(accountID uint, jid string) string {
	if jid == "" {
		return jid
	}

	// 如果已經是 PhoneJID，直接返回
	if IsPhoneJID(jid) {
		return jid
	}

	// 如果是 LID，嘗試從 whatsmeow_lid_map 查找對應的 PhoneJID
	if IsLID(jid) {
		lid := ExtractJIDUser(jid) // 提取 LID 部分
		var mapping WhatsmeowLIDMap
		if err := s.db.Where("lid = ?", lid).First(&mapping).Error; err == nil && mapping.PN != "" {
			return mapping.PN + "@s.whatsapp.net"
		}
	}

	return jid
}

// GetAlternativeJIDs 取得所有等效 JID（包含原始 JID）
// 用於查詢時合併多個 chat 的訊息
func (s *jidMappingService) GetAlternativeJIDs(accountID uint, jid string) []string {
	result := []string{jid}

	if jid == "" {
		return result
	}

	if IsLID(jid) {
		// 查找對應的 PhoneJID
		lid := ExtractJIDUser(jid)
		var mapping WhatsmeowLIDMap
		if err := s.db.Where("lid = ?", lid).First(&mapping).Error; err == nil && mapping.PN != "" {
			phoneJID := mapping.PN + "@s.whatsapp.net"
			if phoneJID != jid {
				result = append(result, phoneJID)
			}
		}
	} else if IsPhoneJID(jid) {
		// 查找對應的 LID
		pn := ExtractJIDUser(jid)
		var mapping WhatsmeowLIDMap
		if err := s.db.Where("pn = ?", pn).First(&mapping).Error; err == nil && mapping.LID != "" {
			lidJID := mapping.LID + "@lid"
			if lidJID != jid {
				result = append(result, lidJID)
			}
		}
	}

	logger.Debugw("GetAlternativeJIDs", "input", jid, "result", result)
	return result
}

// FindExistingChat 查找已存在的 chat（考慮 LID ↔ PhoneJID 映射）
// 返回找到的 chat 或 nil
func (s *jidMappingService) FindExistingChat(db *gorm.DB, accountID uint, jid string) *model.WhatsAppChat {
	// 取得所有等效 JID
	jids := s.GetAlternativeJIDs(accountID, jid)

	logger.Debugw("FindExistingChat",
		"account_id", accountID, "jid", jid, "alternatives", jids)

	// 查找任一 JID 對應的 chat
	var chat model.WhatsAppChat
	if err := db.Where("account_id = ? AND jid IN ?", accountID, jids).First(&chat).Error; err == nil {
		logger.Debugw("FindExistingChat 找到現有 chat",
			"chat_id", chat.ID, "jid", chat.JID)
		return &chat
	}

	return nil
}

// IsLID 判斷是否為 LID 格式（@lid 後綴）
func IsLID(jid string) bool {
	return strings.HasSuffix(jid, "@lid")
}

// IsPhoneJID 判斷是否為傳統 PhoneJID 格式（@s.whatsapp.net 後綴）
func IsPhoneJID(jid string) bool {
	return strings.HasSuffix(jid, "@s.whatsapp.net")
}

// IsGroupJID 判斷是否為群組 JID 格式（@g.us 後綴）
func IsGroupJID(jid string) bool {
	return strings.HasSuffix(jid, "@g.us")
}

// ExtractJIDUser 從 JID 中提取用戶部分（@ 之前的部分）
func ExtractJIDUser(jid string) string {
	if idx := strings.Index(jid, "@"); idx > 0 {
		return jid[:idx]
	}
	return jid
}
