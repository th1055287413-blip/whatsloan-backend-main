package content

import (
	"strings"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChatTagService 聊天室標籤服務接口
type ChatTagService interface {
	// SyncFromSensitiveWordAlerts 從敏感詞告警同步標籤
	SyncFromSensitiveWordAlerts() error
	// GetTagsByChatIDs 批次查詢聊天室標籤
	GetTagsByChatIDs(accountID uint, chatIDs []string) (map[string][]string, error)
	// GetSummariesByChatIDs 批次查詢 AI 摘要（chatID 為數字 ID）
	GetSummariesByChatIDs(accountID uint, chatIDs []uint) (map[uint]string, error)

	// Admin 管理方法
	// ListTags 查詢標籤列表（分頁）
	ListTags(page, pageSize int, filters map[string]interface{}) ([]model.ChatTag, int64, error)
	// CreateTag 手動新增標籤
	CreateTag(tag *model.ChatTag) error
	// DeleteTag 刪除標籤
	DeleteTag(id uint) error
	// GetTagStats 取得標籤統計
	GetTagStats() ([]map[string]interface{}, error)
}

type chatTagService struct {
	db *gorm.DB
}

// NewChatTagService 創建聊天室標籤服務實例
func NewChatTagService(db *gorm.DB) ChatTagService {
	return &chatTagService{db: db}
}

// SyncFromSensitiveWordAlerts 從敏感詞告警同步標籤
func (s *chatTagService) SyncFromSensitiveWordAlerts() error {
	// 1. 查詢 tag_processed = false 且有 category 的告警
	var alerts []model.SensitiveWordAlert
	if err := s.db.Where("tag_processed = ? AND category != ''", false).Find(&alerts).Error; err != nil {
		return err
	}

	if len(alerts) == 0 {
		logger.Info("沒有需要處理的敏感詞告警")
		return nil
	}

	logger.Infow("找到待處理的敏感詞告警", "count", len(alerts))

	// 2. 按 (chat_id, account_id, category) 分組，準備 upsert
	type tagKey struct {
		ChatID    string
		AccountID uint
		Tag       string
	}
	tagSet := make(map[tagKey]struct{})

	alertIDs := make([]uint, 0, len(alerts))
	for _, alert := range alerts {
		alertIDs = append(alertIDs, alert.ID)

		if alert.AccountID == 0 {
			logger.Warnw("告警缺少 AccountID，跳過", "alert_id", alert.ID, "chat_id", alert.ChatID)
			continue
		}

		// 標準化 chat_id：優先使用 phone_jid 格式
		chatIDForTag := alert.ChatID
		resolved := s.resolvePhoneJID(alert.ChatID)
		if resolved != "" {
			chatIDForTag = resolved
		}

		key := tagKey{
			ChatID:    chatIDForTag,
			AccountID: alert.AccountID,
			Tag:       alert.Category,
		}
		tagSet[key] = struct{}{}
	}

	// 4. Upsert chat_tags
	var tagsToUpsert []model.ChatTag
	for key := range tagSet {
		tagsToUpsert = append(tagsToUpsert, model.ChatTag{
			ChatID:    key.ChatID,
			AccountID: key.AccountID,
			Tag:       key.Tag,
			Category:  "sensitive_word",
			Source:    "sensitive_word",
		})
	}

	if len(tagsToUpsert) > 0 {
		// 使用 ON CONFLICT 實現 upsert
		if err := s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "chat_id"}, {Name: "account_id"}, {Name: "tag"}, {Name: "category"}},
			DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
		}).Create(&tagsToUpsert).Error; err != nil {
			return err
		}
		logger.Infow("成功 upsert 聊天室標籤", "count", len(tagsToUpsert))
	}

	// 5. 標記這些 alerts 為 tag_processed = true
	if err := s.db.Model(&model.SensitiveWordAlert{}).
		Where("id IN ?", alertIDs).
		Update("tag_processed", true).Error; err != nil {
		return err
	}

	logger.Infow("已標記敏感詞告警為已處理", "count", len(alertIDs))
	return nil
}

// GetTagsByChatIDs 批次查詢聊天室標籤，回傳 label 供前端顯示
// 同時查詢原始 JID 和 phone_jid 格式的 tags，合併結果
func (s *chatTagService) GetTagsByChatIDs(accountID uint, chatIDs []string) (map[string][]string, error) {
	if len(chatIDs) == 0 {
		return make(map[string][]string), nil
	}

	// 擴展查詢範圍：加入 phone_jid 格式
	allIDs := make([]string, 0, len(chatIDs)*2)
	phoneJIDMap := make(map[string]string) // phone_jid → 原始 chatID
	for _, chatID := range chatIDs {
		allIDs = append(allIDs, chatID)
		phoneJID := s.resolvePhoneJID(chatID)
		if phoneJID != "" && phoneJID != chatID {
			allIDs = append(allIDs, phoneJID)
			phoneJIDMap[phoneJID] = chatID
		}
	}

	var tags []model.ChatTag
	if err := s.db.Where("account_id = ? AND chat_id IN ?", accountID, allIDs).Find(&tags).Error; err != nil {
		return nil, err
	}

	// 建立 AI 標籤 key → label 的對照表
	var defs []model.AiTagDefinition
	s.db.Select("key", "label").Find(&defs)
	labelMap := make(map[string]string, len(defs))
	for _, d := range defs {
		labelMap[d.Key] = d.Label
	}

	result := make(map[string][]string)
	for _, tag := range tags {
		display := tag.Tag
		if label, ok := labelMap[tag.Tag]; ok {
			display = label
		}
		// 將 phone_jid 的 tag 歸到原始 chatID
		targetChatID := tag.ChatID
		if origID, ok := phoneJIDMap[tag.ChatID]; ok {
			targetChatID = origID
		}
		result[targetChatID] = append(result[targetChatID], display)
	}

	return result, nil
}

// GetSummariesByChatIDs 批次查詢 AI 摘要
func (s *chatTagService) GetSummariesByChatIDs(accountID uint, chatIDs []uint) (map[uint]string, error) {
	if len(chatIDs) == 0 {
		return make(map[uint]string), nil
	}

	var summaries []model.ChatAISummary
	if err := s.db.Where("account_id = ? AND chat_id IN ?", accountID, chatIDs).Find(&summaries).Error; err != nil {
		return nil, err
	}

	result := make(map[uint]string, len(summaries))
	for _, sm := range summaries {
		result[sm.ChatID] = sm.Summary
	}
	return result, nil
}

// ListTags 查詢標籤列表（分頁）
func (s *chatTagService) ListTags(page, pageSize int, filters map[string]interface{}) ([]model.ChatTag, int64, error) {
	var tags []model.ChatTag
	var total int64

	query := s.db.Model(&model.ChatTag{})

	// 篩選條件
	if accountID, ok := filters["account_id"].(uint); ok && accountID > 0 {
		query = query.Where("account_id = ?", accountID)
	}
	if tag, ok := filters["tag"].(string); ok && tag != "" {
		query = query.Where("tag ILIKE ?", "%"+tag+"%")
	}
	if source, ok := filters["source"].(string); ok && source != "" {
		query = query.Where("source = ?", source)
	}
	if chatID, ok := filters["chat_id"].(string); ok && chatID != "" {
		query = query.Where("chat_id ILIKE ?", "%"+chatID+"%")
	}

	// 計算總數
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分頁查詢
	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&tags).Error; err != nil {
		return nil, 0, err
	}

	return tags, total, nil
}

// CreateTag 手動新增標籤
func (s *chatTagService) CreateTag(tag *model.ChatTag) error {
	// 使用 FirstOrCreate 更為穩健，避免 ON CONFLICT 索引不匹配問題
	result := s.db.Where(model.ChatTag{
		ChatID:    tag.ChatID,
		AccountID: tag.AccountID,
		Tag:       tag.Tag,
		Category:  tag.Category,
	}).FirstOrCreate(tag)
	return result.Error
}

// DeleteTag 刪除標籤
func (s *chatTagService) DeleteTag(id uint) error {
	result := s.db.Delete(&model.ChatTag{}, id)
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

// GetTagStats 取得標籤統計
func (s *chatTagService) GetTagStats() ([]map[string]interface{}, error) {
	var results []struct {
		Tag    string
		Source string
		Count  int64
	}

	if err := s.db.Model(&model.ChatTag{}).
		Select("tag, source, COUNT(*) as count").
		Group("tag, source").
		Order("count DESC").
		Find(&results).Error; err != nil {
		return nil, err
	}

	stats := make([]map[string]interface{}, len(results))
	for i, r := range results {
		stats[i] = map[string]interface{}{
			"tag":    r.Tag,
			"source": r.Source,
			"count":  r.Count,
		}
	}

	return stats, nil
}

// resolvePhoneJID 將 JID 解析為 phone@s.whatsapp.net 格式
// @s.whatsapp.net → 原樣返回
// @lid → 查 whatsmeow_lid_map → phone@s.whatsapp.net
// 其他 → ""
func (s *chatTagService) resolvePhoneJID(jid string) string {
	if strings.HasSuffix(jid, "@g.us") {
		return ""
	}
	if strings.HasSuffix(jid, "@s.whatsapp.net") {
		return jid
	}
	if strings.HasSuffix(jid, "@lid") {
		lid := jid[:len(jid)-len("@lid")]
		var result struct{ PN string }
		if err := s.db.Table("whatsmeow_lid_map").Select("pn").Where("lid = ?", lid).First(&result).Error; err == nil && result.PN != "" {
			return result.PN + "@s.whatsapp.net"
		}
	}
	return ""
}
