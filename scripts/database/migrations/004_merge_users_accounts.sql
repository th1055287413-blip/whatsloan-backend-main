-- =====================================================
-- 遷移腳本：合併用戶管理到帳號管理
-- 版本: 004
-- 說明: 為 whatsapp_accounts 新增管理欄位，
--       建立 account.* 權限並從 user.*/message.* 映射角色權限
-- =====================================================

BEGIN;

-- =====================================================
-- 1. 為 whatsapp_accounts 新增管理欄位
-- =====================================================
ALTER TABLE whatsapp_accounts
ADD COLUMN IF NOT EXISTS message_count BIGINT DEFAULT 0,
ADD COLUMN IF NOT EXISTS admin_status VARCHAR(20) DEFAULT 'active';

CREATE INDEX IF NOT EXISTS idx_whatsapp_accounts_admin_status
    ON whatsapp_accounts(admin_status);

-- =====================================================
-- 2. 新增 account.* 權限
-- =====================================================
INSERT INTO permissions (name, code, resource, action, description, module) VALUES
('查看所有帳號', 'account.view_all', 'account', 'view_all', '查看所有帳號（不受渠道隔離限制）', 'account_management'),
('查看已分配帳號', 'account.view_assigned', 'account', 'view_assigned', '僅查看已分配給自己的帳號', 'account_management'),
('讀取帳號', 'account.read', 'account', 'read', '讀取帳號資訊', 'account_management'),
('寫入帳號', 'account.write', 'account', 'write', '寫入帳號資訊', 'account_management'),
('刪除帳號', 'account.delete', 'account', 'delete', '刪除帳號', 'account_management'),
('管理帳號分配', 'account.manage_assignments', 'account', 'manage_assignments', '管理帳號與操作員的分配關係', 'account_management')
ON CONFLICT (code) DO NOTHING;

-- =====================================================
-- 3. 複製 role_permissions 映射
--    user.* -> account.*（依 action 對應）
-- =====================================================
INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, ap.id
FROM role_permissions rp
JOIN permissions up ON rp.permission_id = up.id
JOIN permissions ap ON ap.resource = 'account' AND ap.action = up.action
WHERE up.resource = 'user'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- =====================================================
-- 4. 複製 role_permissions 映射
--    message.view -> account.read
--    message.send -> account.write
-- =====================================================
INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, ap.id
FROM role_permissions rp
JOIN permissions mp ON rp.permission_id = mp.id
JOIN permissions ap ON ap.resource = 'account'
    AND (
        (mp.code = 'message.view' AND ap.action = 'read')
        OR
        (mp.code = 'message.send' AND ap.action = 'write')
    )
WHERE mp.resource = 'message'
  AND mp.code IN ('message.view', 'message.send')
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;

-- =====================================================
-- 驗證遷移結果
-- =====================================================
SELECT '===== 遷移驗證 =====' AS status;
SELECT 'account.* 權限數量: ' || COUNT(*)::TEXT FROM permissions WHERE resource = 'account';
SELECT '帳號已有 admin_status 欄位: ' || EXISTS(
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'whatsapp_accounts' AND column_name = 'admin_status'
)::TEXT;
SELECT '帳號已有 message_count 欄位: ' || EXISTS(
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'whatsapp_accounts' AND column_name = 'message_count'
)::TEXT;
SELECT 'account.* 角色權限映射數: ' || COUNT(*)::TEXT
FROM role_permissions rp
JOIN permissions p ON rp.permission_id = p.id
WHERE p.resource = 'account';
SELECT '===== 遷移完成 =====' AS status;
