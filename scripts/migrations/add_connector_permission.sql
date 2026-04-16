-- 新增 Connector 狀態查看權限
-- 執行此腳本以在現有資料庫中新增 connector.view 權限

BEGIN;

-- 新增權限
INSERT INTO permissions (name, code, resource, action, description, module, created_at, updated_at)
VALUES ('查看 Connector 状态', 'connector.view', 'connector', 'view', '查看 Connector 状态和统计信息', 'system', NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

-- 為超級管理員分配權限
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.name = 'super_admin' AND p.code = 'connector.view'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
