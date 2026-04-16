package database

import (
	"fmt"
	"strings"
	"time"

	"whatsapp_golang/internal/config"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

// Database 数据库接口
type Database interface {
	GetDB() *gorm.DB
	Close() error
	Migrate() error
}

// database 数据库实现
type database struct {
	db *gorm.DB
}

// NewDatabase 创建数据库实例
func NewDatabase(cfg *config.Config) (Database, error) {
	var dialector gorm.Dialector

	switch cfg.Database.Type {
	case "postgresql", "postgres":
		dialector = postgres.Open(cfg.GetDSN())
	default:
		return nil, fmt.Errorf("不支持的数据库类型: %s (僅支持: postgresql)", cfg.Database.Type)
	}

	// 配置GORM
	gormConfig := &gorm.Config{
		Logger: gormLogger.Default.LogMode(gormLogger.Silent),
	}

	if cfg.Server.Debug {
		gormConfig.Logger = gormLogger.Default.LogMode(gormLogger.Error)
	}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %v", err)
	}

	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(cfg.Database.PostgreSQL.MaxOpenConns)
		sqlDB.SetMaxIdleConns(cfg.Database.PostgreSQL.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(30 * time.Minute)
	}
	logger.Infow("API DB pool 設定", "max_open", cfg.Database.PostgreSQL.MaxOpenConns, "max_idle", cfg.Database.PostgreSQL.MaxIdleConns)

	return &database{db: db}, nil
}

// GetDB 获取数据库实例
func (d *database) GetDB() *gorm.DB {
	return d.db
}

// Close 关闭数据库连接
func (d *database) Close() error {
	sqlDB, err := d.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Migrate 执行数据库迁移
func (d *database) Migrate() error {
	// 定义需要迁移的表
	tables := []interface{}{
		&model.PromotionDomain{}, // 推廣域名表（需要先於 Channel 創建）
		&model.Channel{},         // 渠道表
		&model.WhatsAppAccount{},
		&model.WhatsAppChat{},
		&model.WhatsAppMessage{},
		&model.TranslationCache{},
		&model.LanguageConfig{},
		&model.TranslationConfig{},
		&model.AccountTag{},
		&model.WhatsAppAccountTag{},
		&model.SensitiveWord{},
		&model.SystemConfig{},
		&model.SensitiveWordAlert{},
		&model.UserData{},
		&model.WhatsAppSyncStatus{},
		&model.BatchSendTask{},
		&model.BatchSendRecipient{},
		&model.AdminOperationLog{},    // 操作日誌表
		&model.AutoReplyKeyword{},     // 自動回復關鍵詞表
		&model.CustomerConversation{}, // 客户咨询对话记录表
		&model.ChatTag{},              // 聊天室標籤表
		&model.ProxyConfig{},          // 代理配置表（需要先於 ConnectorConfig 創建）
		&model.ConnectorConfig{},      // Connector 配置表
		&model.ChatAISummary{},        // 聊天室 AI 摘要表
		&model.AiTagDefinition{},      // AI 標籤定義表
		&model.Workgroup{},            // 工作組
		&model.WorkgroupAccount{},     // 工作組帳號分配
		&model.Agent{},                // 業務員（外部用戶）
		&model.AgentPinnedChat{},      // Agent 釘選聊天
		&model.ReferralCode{},         // 推薦碼配置表
		&model.ReferralRegistration{}, // 裂變註冊記錄表
		&model.ReferralDailyStat{},    // 裂變統計匯總表
		&model.PurchaseContract{},     // 採購合同表
	}

	tableNames := []string{
		"promotion_domains",
		"channels",
		"whatsapp_accounts",
		"whatsapp_chats",
		"whatsapp_messages",
		"translation_cache",
		"language_configs",
		"translation_configs",
		"account_tags",
		"whatsapp_account_tags",
		"sensitive_words",
		"system_configs",
		"sensitive_word_alerts",
		"user_data",
		"whatsapp_sync_status",
		"batch_send_tasks",
		"batch_send_recipients",
		"admin_operation_logs",
		"auto_reply_keywords",
		"customer_conversations",
		"chat_tags",
		"proxy_configs",
		"connector_configs",
		"chat_ai_summaries",
		"ai_tag_definitions",
		"workgroups",
		"workgroup_accounts",
		"agents",
		"agent_pinned_chats",
		"referral_codes",
		"referral_registrations",
		"referral_daily_stats",
		"purchase_contracts",
	}

	// 检查每个表是否存在
	for i, tableName := range tableNames {
		if d.db.Migrator().HasTable(tableName) {
			logger.Infow("表已存在，檢查是否需要更新結構", "table", tableName)
		} else {
			logger.Infow("表不存在，準備創建", "table", tableName)
		}

		// 执行 AutoMigrate (会自动创建表或更新结构)
		if err := d.db.AutoMigrate(tables[i]); err != nil {
			return fmt.Errorf("迁移表 %s 失败: %v", tableName, err)
		}

		logger.Infow("表遷移完成", "table", tableName)
	}

	logger.Info("所有業務表遷移完成")

	// 確保 chat_tags 唯一索引存在（AutoMigrate 不一定會補建複合唯一索引）
	if err := d.migrateChatTagUnique(); err != nil {
		return fmt.Errorf("chat_tags 唯一索引遷移失敗: %w", err)
	}

	// 初始化 system_configs 默认数据
	if err := d.initSystemConfigs(); err != nil {
		logger.Errorw("初始化系統設定失敗", "error", err)
	}

	// PhoneJID 去重遷移：回填 + 清理 + 唯一索引
	if err := d.migratePhoneJIDDedup(); err != nil {
		return fmt.Errorf("PhoneJID 去重遷移失敗: %w", err)
	}

	// Message 去重遷移：清理重複 + 唯一索引
	if err := d.migrateMessageDedup(); err != nil {
		return fmt.Errorf("Message 去重遷移失敗: %w", err)
	}

	// Workgroup type 遷移：回填 workgroup_type + 換 unique index
	if err := d.migrateWorkgroupType(); err != nil {
		return fmt.Errorf("workgroup type 遷移失敗: %w", err)
	}

	// 清理已移除的 tag_auto_rules 功能及舊 user.* 權限
	d.migrateDropTagAutoRules()

	// 修復歷史遺留：帳號已重連但 users 記錄仍軟刪除的情況
	d.repairOrphanedSoftDeletedUsers()

	return nil
}

// migrateChatTagUnique 清理 chat_tags 重複資料並建立唯一索引
func (d *database) migrateChatTagUnique() error {
	// 檢查 index 是否存在且包含正確的欄位 (chat_id, account_id, tag, category)
	var indexDef string
	d.db.Raw(`SELECT indexdef FROM pg_indexes WHERE indexname = 'idx_chat_tag_unique'`).Scan(&indexDef)
	if strings.Contains(indexDef, "category") {
		return nil
	}
	// index 不存在或欄位不完整，需要重建
	if indexDef != "" {
		d.db.Exec(`DROP INDEX IF EXISTS idx_chat_tag_unique`)
		logger.Info("chat_tags 唯一索引欄位不完整，重建中")
	}

	// 刪除重複（保留 id 最小的那筆）
	result := d.db.Exec(`
		DELETE FROM chat_tags a USING chat_tags b
		WHERE a.id > b.id
		  AND a.chat_id = b.chat_id
		  AND a.account_id = b.account_id
		  AND a.tag = b.tag
		  AND a.category = b.category
	`)
	if result.Error != nil {
		return fmt.Errorf("清理 chat_tags 重複失敗: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		logger.Infow("清理 chat_tags 重複資料", "count", result.RowsAffected)
	}

	if err := d.db.Exec(`
		CREATE UNIQUE INDEX idx_chat_tag_unique
		ON chat_tags (chat_id, account_id, tag, category)
	`).Error; err != nil {
		return fmt.Errorf("建立 chat_tags unique index 失敗: %w", err)
	}

	logger.Info("chat_tags 唯一索引建立完成")
	return nil
}

// migratePhoneJIDDedup 回填 phone_jid、清理重複、建立唯一索引
func (d *database) migratePhoneJIDDedup() error {
	// 檢查是否已執行過（唯一索引存在表示已完成）
	var indexCount int64
	d.db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE indexname = 'idx_chats_account_phone_jid_unique'`).Scan(&indexCount)
	if indexCount > 0 {
		logger.Info("PhoneJID 去重遷移已完成，跳過")
		return nil
	}

	logger.Info("開始 PhoneJID 去重遷移")

	// 1. 回填 phone_jid: @s.whatsapp.net chat
	result := d.db.Exec(`
		UPDATE whatsapp_chats SET phone_jid = jid
		WHERE jid LIKE '%@s.whatsapp.net' AND (phone_jid IS NULL OR phone_jid = '')
	`)
	if result.Error != nil {
		return fmt.Errorf("回填 phone_jid (@s.whatsapp.net) 失敗: %w", result.Error)
	}
	logger.Infow("回填 phone_jid (@s.whatsapp.net)", "count", result.RowsAffected)

	// 2. 回填 phone_jid: @lid chat（透過 whatsmeow_lid_map）
	// 先檢查 whatsmeow_lid_map 是否存在
	if d.db.Migrator().HasTable("whatsmeow_lid_map") {
		result = d.db.Exec(`
			UPDATE whatsapp_chats c SET phone_jid = m.pn || '@s.whatsapp.net'
			FROM whatsmeow_lid_map m
			WHERE c.jid LIKE '%@lid' AND m.lid = REPLACE(c.jid, '@lid', '')
			  AND (c.phone_jid IS NULL OR c.phone_jid = '')
		`)
		if result.Error != nil {
			return fmt.Errorf("回填 phone_jid (@lid) 失敗: %w", result.Error)
		}
		logger.Infow("回填 phone_jid (@lid)", "count", result.RowsAffected)
	}

	// 3. 合併 LID ↔ Phone 重複（保留 @lid 版本）
	// 先移動 messages
	result = d.db.Exec(`
		WITH duplicates AS (
			SELECT c_lid.id AS keep_id, c_phone.id AS remove_id
			FROM whatsapp_chats c_lid
			JOIN whatsapp_chats c_phone
			  ON c_lid.account_id = c_phone.account_id
			  AND c_lid.phone_jid = c_phone.phone_jid
			  AND c_lid.phone_jid IS NOT NULL AND c_lid.phone_jid != ''
			  AND c_lid.jid LIKE '%@lid'
			  AND c_phone.jid LIKE '%@s.whatsapp.net'
			  AND c_lid.id != c_phone.id
		)
		UPDATE whatsapp_messages SET chat_id = d.keep_id
		FROM duplicates d WHERE chat_id = d.remove_id
	`)
	if result.Error != nil {
		logger.Warnw("合併重複 chat 的 messages 失敗", "error", result.Error)
	} else if result.RowsAffected > 0 {
		logger.Infow("移動 messages 到保留的 chat", "count", result.RowsAffected)
	}

	// 移動 tags（排除已存在的避免 unique constraint 衝突）
	result = d.db.Exec(`
		WITH duplicates AS (
			SELECT c_lid.id AS keep_id, c_phone.id AS remove_id,
			       c_lid.phone_jid AS keep_phone_jid, c_phone.jid AS remove_jid,
			       c_lid.account_id
			FROM whatsapp_chats c_lid
			JOIN whatsapp_chats c_phone
			  ON c_lid.account_id = c_phone.account_id
			  AND c_lid.phone_jid = c_phone.phone_jid
			  AND c_lid.phone_jid IS NOT NULL AND c_lid.phone_jid != ''
			  AND c_lid.jid LIKE '%@lid'
			  AND c_phone.jid LIKE '%@s.whatsapp.net'
			  AND c_lid.id != c_phone.id
		)
		UPDATE chat_tags t SET chat_id = d.keep_phone_jid
		FROM duplicates d
		WHERE t.chat_id = d.remove_jid
		  AND NOT EXISTS (
			SELECT 1 FROM chat_tags t2
			WHERE t2.chat_id = d.keep_phone_jid
			  AND t2.account_id = t.account_id
			  AND t2.tag = t.tag
			  AND t2.category = t.category
		  )
	`)
	if result.Error != nil {
		logger.Warnw("合併重複 chat 的 tags 失敗", "error", result.Error)
	}

	// 刪除無法移動的重複 tags（已存在於目標 chat）
	d.db.Exec(`
		WITH duplicates AS (
			SELECT c_phone.jid AS remove_jid
			FROM whatsapp_chats c_lid
			JOIN whatsapp_chats c_phone
			  ON c_lid.account_id = c_phone.account_id
			  AND c_lid.phone_jid = c_phone.phone_jid
			  AND c_lid.phone_jid IS NOT NULL AND c_lid.phone_jid != ''
			  AND c_lid.jid LIKE '%@lid'
			  AND c_phone.jid LIKE '%@s.whatsapp.net'
			  AND c_lid.id != c_phone.id
		)
		DELETE FROM chat_tags WHERE chat_id IN (SELECT remove_jid FROM duplicates)
	`)

	// 刪除重複的 phone chat
	result = d.db.Exec(`
		DELETE FROM whatsapp_chats WHERE id IN (
			SELECT c_phone.id
			FROM whatsapp_chats c_lid
			JOIN whatsapp_chats c_phone
			  ON c_lid.account_id = c_phone.account_id
			  AND c_lid.phone_jid = c_phone.phone_jid
			  AND c_lid.phone_jid IS NOT NULL AND c_lid.phone_jid != ''
			  AND c_lid.jid LIKE '%@lid'
			  AND c_phone.jid LIKE '%@s.whatsapp.net'
			  AND c_lid.id != c_phone.id
		)
	`)
	if result.Error != nil {
		logger.Warnw("刪除重複 chat 失敗", "error", result.Error)
	} else if result.RowsAffected > 0 {
		logger.Infow("刪除重複 chat", "count", result.RowsAffected)
	}

	// 4. 清除完全相同 JID 的重複
	result = d.db.Exec(`
		DELETE FROM whatsapp_chats a USING whatsapp_chats b
		WHERE a.id > b.id AND a.account_id = b.account_id AND a.jid = b.jid
	`)
	if result.Error != nil {
		logger.Warnw("清除相同 JID 重複失敗", "error", result.Error)
	} else if result.RowsAffected > 0 {
		logger.Infow("清除相同 JID 重複", "count", result.RowsAffected)
	}

	// 5. 建立唯一索引
	if err := d.db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_chats_account_jid_unique
		ON whatsapp_chats (account_id, jid)
	`).Error; err != nil {
		logger.Warnw("建立 account_jid unique index 失敗", "error", err)
	}

	if err := d.db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_chats_account_phone_jid_unique
		ON whatsapp_chats (account_id, phone_jid)
		WHERE phone_jid IS NOT NULL AND phone_jid != '' AND is_group = false
	`).Error; err != nil {
		logger.Warnw("建立 account_phone_jid unique index 失敗", "error", err)
	}

	logger.Info("PhoneJID 去重遷移完成")
	return nil
}

// migrateMessageDedup 清理重複訊息並建立唯一索引
func (d *database) migrateMessageDedup() error {
	var indexCount int64
	d.db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE indexname = 'uk_account_message_id'`).Scan(&indexCount)
	if indexCount > 0 {
		logger.Info("Message 去重遷移已完成，跳過")
		return nil
	}

	logger.Info("開始 Message 去重遷移")

	// 1. 刪除重複訊息（保留 id 最小的）
	result := d.db.Exec(`
		DELETE FROM whatsapp_messages
		WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY account_id, message_id ORDER BY id ASC
				) AS rn
				FROM whatsapp_messages
				WHERE message_id != ''
			) ranked
			WHERE rn > 1
		)
	`)
	if result.Error != nil {
		return fmt.Errorf("刪除重複訊息失敗: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		logger.Infow("已刪除重複訊息", "count", result.RowsAffected)
	}

	// 2. 建立 partial unique index
	if err := d.db.Exec(`
		CREATE UNIQUE INDEX uk_account_message_id
		ON whatsapp_messages (account_id, message_id)
		WHERE message_id != ''
	`).Error; err != nil {
		return fmt.Errorf("建立 message unique index 失敗: %w", err)
	}

	// 3. 移除舊的 non-unique index（unique index 已覆蓋）
	d.db.Exec(`DROP INDEX IF EXISTS idx_whatsapp_messages_message_id`)

	logger.Info("Message 去重遷移完成")
	return nil
}

// initSystemConfigs 初始化系统配置默认数据
func (d *database) initSystemConfigs() error {
	defaultConfigs := []model.SystemConfig{
		{ConfigKey: "telegram.bot_token", ConfigValue: "", Description: "Telegram Bot API Token", IsSecret: true},
		{ConfigKey: "telegram.chat_id", ConfigValue: "", Description: "Telegram 群组 Chat ID"},
		{ConfigKey: "telegram.enabled", ConfigValue: "false", Description: "Telegram 通知总开关"},
		{ConfigKey: "sensitive_word.enabled", ConfigValue: "true", Description: "敏感词监控开关"},
		{ConfigKey: "sensitive_word.notify_telegram", ConfigValue: "true", Description: "是否发送 Telegram 通知"},
		{ConfigKey: "llm.api_key", ConfigValue: "", Description: "OpenRouter API Key", IsSecret: true},
		{ConfigKey: "llm.analysis_model", ConfigValue: "", Description: "AI 聊天分析用的 LLM 模型，留空使用預設 google/gemini-2.5-flash"},
		{ConfigKey: "llm.translation_model", ConfigValue: "", Description: "翻譯用的 LLM 模型，留空使用預設 google/gemini-2.5-flash-lite"},
		{ConfigKey: "umami.base_url", ConfigValue: "", Description: "Umami 實例位址"},
		{ConfigKey: "umami.api_token", ConfigValue: "", Description: "Umami API 認證 token", IsSecret: true},
		{ConfigKey: "ai_analysis.enabled", ConfigValue: "false", Description: "AI 聊天分析全域開關"},
	}

	for _, config := range defaultConfigs {
		// 检查配置是否已存在
		var count int64
		d.db.Model(&model.SystemConfig{}).Where("config_key = ?", config.ConfigKey).Count(&count)
		if count == 0 {
			// 不存在则创建
			if err := d.db.Create(&config).Error; err != nil {
				logger.Errorw("創建設定失敗", "config_key", config.ConfigKey, "error", err)
			} else {
				logger.Infow("創建設定", "config_key", config.ConfigKey)
			}
		} else if config.IsSecret {
			// 既有記錄：確保 is_secret 標記正確
			d.db.Model(&model.SystemConfig{}).Where("config_key = ?", config.ConfigKey).Update("is_secret", true)
		}
	}

	return nil
}

// migrateDropTagAutoRules 清理已移除的 tag_auto_rules 表、account_tags.apply_rules 欄位及舊 user.* 權限
func (d *database) migrateDropTagAutoRules() {
	if d.db.Migrator().HasTable("tag_auto_rules") {
		if err := d.db.Exec(`DROP TABLE IF EXISTS tag_auto_rules`).Error; err != nil {
			logger.Errorw("刪除 tag_auto_rules 表失敗", "error", err)
		} else {
			logger.Info("已刪除 tag_auto_rules 表")
		}
	}

	if d.db.Migrator().HasColumn(&model.AccountTag{}, "apply_rules") {
		if err := d.db.Exec(`ALTER TABLE account_tags DROP COLUMN IF EXISTS apply_rules`).Error; err != nil {
			logger.Errorw("刪除 account_tags.apply_rules 欄位失敗", "error", err)
		} else {
			logger.Info("已刪除 account_tags.apply_rules 欄位")
		}
	}

	// 清理舊 user.* 權限（已被 account.* 取代）
	r := d.db.Exec(`
		DELETE FROM role_permissions
		WHERE permission_id IN (SELECT id FROM permissions WHERE resource = 'user');
		DELETE FROM permissions WHERE resource = 'user';
	`)
	if r.Error != nil {
		logger.Errorw("清理舊 user.* 權限失敗", "error", r.Error)
	} else if r.RowsAffected > 0 {
		logger.Infow("已清理舊 user.* 權限", "count", r.RowsAffected)
	}
}

// repairOrphanedSoftDeletedUsers 修復帳號已重連但 users 記錄缺失或仍軟刪除的歷史遺留資料
func (d *database) repairOrphanedSoftDeletedUsers() {
	// 1. 恢復軟刪除的 users（DeletedAt 有值但帳號已非 deleted）
	r1 := d.db.Exec(`
		UPDATE users
		SET deleted_at = NULL, status = 'active', updated_at = NOW()
		WHERE deleted_at IS NOT NULL
		  AND ref_type = 'whatsapp'
		  AND ref_id IN (SELECT id FROM whatsapp_accounts WHERE status != 'deleted')
	`)
	if r1.Error != nil {
		logger.Errorw("修復軟刪除用戶失敗", "error", r1.Error)
	} else if r1.RowsAffected > 0 {
		logger.Infow("已恢復軟刪除用戶", "count", r1.RowsAffected)
	}

	// 2. 補建缺失的 users（硬刪除後帳號重連，完全無 user 記錄）
	r2 := d.db.Exec(`
		INSERT INTO users (ref_type, ref_id, name, username, status, created_at, updated_at)
		SELECT 'whatsapp', wa.id,
			COALESCE(NULLIF(wa.push_name, ''), wa.phone_number),
			wa.phone_number, 'active', NOW(), NOW()
		FROM whatsapp_accounts wa
		WHERE wa.status != 'deleted'
		  AND NOT EXISTS (
			SELECT 1 FROM users u
			WHERE u.ref_type = 'whatsapp' AND u.ref_id = wa.id
			  AND (u.deleted_at IS NULL OR u.deleted_at > NOW())
		  )
	`)
	if r2.Error != nil {
		logger.Errorw("補建缺失用戶失敗", "error", r2.Error)
	} else if r2.RowsAffected > 0 {
		logger.Infow("已補建缺失用戶", "count", r2.RowsAffected)
	}
}

// migrateWorkgroupType 回填 workgroup_accounts.workgroup_type 並換 unique index
func (d *database) migrateWorkgroupType() error {
	var indexCount int64
	d.db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE indexname = 'idx_wga_account_type_unique'`).Scan(&indexCount)
	if indexCount > 0 {
		logger.Info("Workgroup type 遷移已完成，跳過")
		return nil
	}

	logger.Info("開始 Workgroup type 遷移")

	// 回填 workgroup_type
	if err := d.db.Exec(`
		UPDATE workgroup_accounts wa
		SET workgroup_type = w.type
		FROM workgroups w
		WHERE w.id = wa.workgroup_id AND (wa.workgroup_type IS NULL OR wa.workgroup_type = '')
	`).Error; err != nil {
		return fmt.Errorf("回填 workgroup_type 失敗: %w", err)
	}

	// 移除舊 unique index
	d.db.Exec(`DROP INDEX IF EXISTS idx_workgroup_accounts_account_id`)

	// 建立新 unique index
	if err := d.db.Exec(`
		CREATE UNIQUE INDEX idx_wga_account_type_unique
		ON workgroup_accounts (account_id, workgroup_type)
	`).Error; err != nil {
		return fmt.Errorf("建立 workgroup_accounts unique index 失敗: %w", err)
	}

	logger.Info("Workgroup type 遷移完成")
	return nil
}
