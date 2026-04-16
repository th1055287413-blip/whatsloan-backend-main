-- =====================================================
-- 遷移腳本：user_roles -> admin_roles
-- 版本: 002
-- 說明: 將 user_roles 資料遷移至 admin_roles，並移除 user_roles 表
-- =====================================================

BEGIN;

-- =====================================================
-- 1. 確保 admin_roles 有新欄位
-- =====================================================
ALTER TABLE admin_roles
ADD COLUMN IF NOT EXISTS is_primary BOOLEAN DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS data_scope TEXT,
ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;

-- =====================================================
-- 2. 從 user_roles 遷移資料到 admin_roles
-- =====================================================
INSERT INTO admin_roles (admin_id, role_id, is_primary, data_scope, created_at, updated_at)
SELECT user_id, role_id, is_primary, data_scope, created_at, updated_at
FROM user_roles
WHERE EXISTS (SELECT 1 FROM admin_users WHERE admin_users.id = user_roles.user_id)
ON CONFLICT (admin_id, role_id) DO UPDATE SET
    is_primary = EXCLUDED.is_primary,
    data_scope = EXCLUDED.data_scope,
    updated_at = CURRENT_TIMESTAMP;

-- =====================================================
-- 3. 為 admin_roles 創建 updated_at 觸發器
-- =====================================================
DROP TRIGGER IF EXISTS update_admin_roles_updated_at ON admin_roles;
CREATE TRIGGER update_admin_roles_updated_at
    BEFORE UPDATE ON admin_roles
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- 4. 刪除 user_roles 表的觸發器
-- =====================================================
DROP TRIGGER IF EXISTS update_user_roles_updated_at ON user_roles;

-- =====================================================
-- 5. 刪除 user_roles 表
-- =====================================================
DROP TABLE IF EXISTS user_roles;

-- =====================================================
-- 6. 添加註釋
-- =====================================================
COMMENT ON COLUMN admin_roles.is_primary IS '是否主要角色';
COMMENT ON COLUMN admin_roles.data_scope IS '資料權限範圍(JSON)';

COMMIT;

-- =====================================================
-- 驗證遷移結果
-- =====================================================
SELECT '===== 遷移驗證 =====' AS status;
SELECT 'admin_roles 記錄數: ' || COUNT(*)::TEXT FROM admin_roles;
SELECT 'user_roles 表是否存在: ' || EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'user_roles')::TEXT;
SELECT '===== 遷移完成 =====' AS status;
