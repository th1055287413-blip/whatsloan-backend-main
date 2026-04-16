-- =====================================================
-- 遷移腳本：users 表多態關聯重構
-- 版本: 003
-- 說明: 將 users 表改為透過 ref_type + ref_id 關聯到各通訊管道
--       移除重複欄位（phone, avatar, is_online, last_seen, channel_id）
-- =====================================================

BEGIN;

-- =====================================================
-- 1. 新增 ref_type 和 ref_id 欄位
-- =====================================================
ALTER TABLE users
ADD COLUMN IF NOT EXISTS ref_type VARCHAR(20),
ADD COLUMN IF NOT EXISTS ref_id INTEGER;

-- =====================================================
-- 2. 遷移現有資料：透過 phone 關聯到 whatsapp_accounts
-- =====================================================
UPDATE users u
SET
    ref_type = 'whatsapp',
    ref_id = wa.id
FROM whatsapp_accounts wa
WHERE u.phone = wa.phone_number
  AND u.ref_type IS NULL;

-- 處理帶有 + 前綴的手機號
UPDATE users u
SET
    ref_type = 'whatsapp',
    ref_id = wa.id
FROM whatsapp_accounts wa
WHERE (u.phone = '+' || wa.phone_number OR '+' || u.phone = wa.phone_number)
  AND u.ref_type IS NULL;

-- =====================================================
-- 3. 刪除無法關聯的孤立記錄（可選，先備份）
-- =====================================================
-- 建立備份表
CREATE TABLE IF NOT EXISTS users_orphaned_backup AS
SELECT * FROM users WHERE ref_type IS NULL OR ref_id IS NULL;

-- 刪除孤立記錄
DELETE FROM users WHERE ref_type IS NULL OR ref_id IS NULL;

-- =====================================================
-- 4. 設定 NOT NULL 約束
-- =====================================================
ALTER TABLE users
ALTER COLUMN ref_type SET NOT NULL,
ALTER COLUMN ref_id SET NOT NULL;

-- =====================================================
-- 5. 移除舊欄位
-- =====================================================
ALTER TABLE users
DROP COLUMN IF EXISTS phone,
DROP COLUMN IF EXISTS avatar,
DROP COLUMN IF EXISTS is_online,
DROP COLUMN IF EXISTS last_seen,
DROP COLUMN IF EXISTS channel_id;

-- =====================================================
-- 6. 建立新索引
-- =====================================================
DROP INDEX IF EXISTS idx_users_phone;
DROP INDEX IF EXISTS idx_users_channel_id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_ref ON users(ref_type, ref_id);

-- =====================================================
-- 7. 更新表註釋
-- =====================================================
COMMENT ON TABLE users IS '應用層用戶表，透過 ref_type + ref_id 關聯到各通訊管道';
COMMENT ON COLUMN users.ref_type IS '來源類型: whatsapp, telegram 等';
COMMENT ON COLUMN users.ref_id IS '來源表的 ID';

COMMIT;

-- =====================================================
-- 驗證遷移結果
-- =====================================================
SELECT '===== 遷移驗證 =====' AS status;
SELECT 'users 記錄數: ' || COUNT(*)::TEXT FROM users;
SELECT 'whatsapp 類型用戶數: ' || COUNT(*)::TEXT FROM users WHERE ref_type = 'whatsapp';
SELECT '孤立記錄備份數: ' || COUNT(*)::TEXT FROM users_orphaned_backup;
SELECT '===== 遷移完成 =====' AS status;
