-- 權限系統優化遷移腳本 v2
-- 執行前請先備份資料庫
-- 此腳本將舊權限名稱遷移至新權限名稱

BEGIN;

-- =====================================================
-- 1. 移除 whatsapp_account 權限（已合併到 message）
-- =====================================================

-- 刪除 whatsapp_account 相關的 role_permissions
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE resource = 'whatsapp_account'
);

-- 刪除 whatsapp_account 權限
DELETE FROM permissions WHERE resource = 'whatsapp_account';

-- =====================================================
-- 2. 遷移 sensitive_word → content_moderation.word_*
-- =====================================================

UPDATE permissions SET
    resource = 'content_moderation',
    action = 'word_view',
    code = 'content_moderation.word_view',
    module = 'content_moderation',
    name = '查看敏感词'
WHERE code = 'sensitive_word.view';

UPDATE permissions SET
    resource = 'content_moderation',
    action = 'word_create',
    code = 'content_moderation.word_create',
    module = 'content_moderation',
    name = '创建敏感词'
WHERE code = 'sensitive_word.create';

UPDATE permissions SET
    resource = 'content_moderation',
    action = 'word_update',
    code = 'content_moderation.word_update',
    module = 'content_moderation',
    name = '修改敏感词'
WHERE code = 'sensitive_word.update';

UPDATE permissions SET
    resource = 'content_moderation',
    action = 'word_delete',
    code = 'content_moderation.word_delete',
    module = 'content_moderation',
    name = '删除敏感词'
WHERE code = 'sensitive_word.delete';

-- =====================================================
-- 3. 遷移 sensitive_word_alert → content_moderation.alert_*
-- =====================================================

UPDATE permissions SET
    resource = 'content_moderation',
    action = 'alert_view',
    code = 'content_moderation.alert_view',
    module = 'content_moderation',
    name = '查看敏感词告警'
WHERE code = 'sensitive_word_alert.view';

UPDATE permissions SET
    resource = 'content_moderation',
    action = 'alert_detail',
    code = 'content_moderation.alert_detail',
    module = 'content_moderation',
    name = '查看告警详情'
WHERE code = 'sensitive_word_alert.detail';

UPDATE permissions SET
    resource = 'content_moderation',
    action = 'alert_stats',
    code = 'content_moderation.alert_stats',
    module = 'content_moderation',
    name = '查看告警统计'
WHERE code = 'sensitive_word_alert.stats';

UPDATE permissions SET
    resource = 'content_moderation',
    action = 'alert_resend',
    code = 'content_moderation.alert_resend',
    module = 'content_moderation',
    name = '重发告警通知'
WHERE code = 'sensitive_word_alert.resend';

-- =====================================================
-- 4. 遷移 config → system.config_*
-- =====================================================

UPDATE permissions SET
    resource = 'system',
    action = 'config_view',
    code = 'system.config_view',
    module = 'system',
    name = '查看系统配置'
WHERE code = 'config.view';

UPDATE permissions SET
    resource = 'system',
    action = 'config_update',
    code = 'system.config_update',
    module = 'system',
    name = '修改系统配置'
WHERE code = 'config.update';

-- =====================================================
-- 5. 遷移 operation_log → system.log_view
-- =====================================================

UPDATE permissions SET
    resource = 'system',
    action = 'log_view',
    code = 'system.log_view',
    module = 'system',
    name = '查看操作日志'
WHERE code = 'operation_log.view';

-- =====================================================
-- 6. 更新 message 權限的 module 名稱（統一為 message）
-- =====================================================

UPDATE permissions SET module = 'message' WHERE resource = 'message';

-- =====================================================
-- 7. 新增 dashboard.view 權限
-- =====================================================

INSERT INTO permissions (name, code, resource, action, description, module, created_at, updated_at)
VALUES ('查看儀表盤', 'dashboard.view', 'dashboard', 'view', '查看儀表盤數據', 'dashboard', NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

-- 為超級管理員分配 dashboard.view 權限
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.name = 'super_admin' AND p.code = 'dashboard.view'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- =====================================================
-- 8. 移除 translation.*, media.*, device.* 權限（不再需要權限檢查）
-- =====================================================

-- 刪除 translation 相關的 role_permissions
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE resource = 'translation'
);

-- 刪除 media 相關的 role_permissions
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE resource = 'media'
);

-- 刪除 device 相關的 role_permissions
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE resource = 'device'
);

-- 刪除 translation 權限
DELETE FROM permissions WHERE resource = 'translation';

-- 刪除 media 權限
DELETE FROM permissions WHERE resource = 'media';

-- 刪除 device 權限
DELETE FROM permissions WHERE resource = 'device';

-- =====================================================
-- 9. 移除 admin_users 的 email 欄位
-- =====================================================

-- 移除 email 唯一約束
ALTER TABLE admin_users DROP CONSTRAINT IF EXISTS uni_admin_users_email;

-- 移除 email 索引
DROP INDEX IF EXISTS idx_admin_users_email;

-- 移除 email 欄位
ALTER TABLE admin_users DROP COLUMN IF EXISTS email;

-- =====================================================
-- 10. 驗證結果
-- =====================================================

-- 確認沒有遺留的舊權限
DO $$
DECLARE
    old_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO old_count FROM permissions
    WHERE resource IN ('whatsapp_account', 'sensitive_word', 'sensitive_word_alert', 'config', 'operation_log', 'translation', 'media', 'device');

    IF old_count > 0 THEN
        RAISE EXCEPTION 'Migration incomplete: % old permissions still exist', old_count;
    END IF;
END $$;

COMMIT;

-- 顯示遷移後的權限統計
SELECT module, COUNT(*) as count
FROM permissions
GROUP BY module
ORDER BY module;
