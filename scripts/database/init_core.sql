-- =====================================================
-- WhatsApp 管理系統 - PostgreSQL 核心初始化腳本
-- 版本: 4.0
-- 說明: 僅包含權限相關表、觸發器函數和初始數據
--       業務表由 GORM AutoMigrate 自動創建
-- =====================================================

BEGIN;

-- =====================================================
-- 啟用擴展
-- =====================================================
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- =====================================================
-- 觸發器函數 - 自動更新 updated_at
-- =====================================================
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE 'plpgsql';

-- =====================================================
-- 0. 推廣域名表
-- =====================================================
CREATE TABLE IF NOT EXISTS promotion_domains (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    domain VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'enabled',
    remark TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_promotion_domain ON promotion_domains(domain) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_promotion_domains_status ON promotion_domains(status);
CREATE INDEX IF NOT EXISTS idx_promotion_domains_deleted_at ON promotion_domains(deleted_at);

COMMENT ON TABLE promotion_domains IS '推廣域名表';

-- =====================================================
-- 1. 渠道表
-- =====================================================
CREATE TABLE IF NOT EXISTS channels (
    id BIGSERIAL PRIMARY KEY,
    channel_code VARCHAR(6) NOT NULL,
    channel_name VARCHAR(100) NOT NULL,
    promotion_domain_id INTEGER REFERENCES promotion_domains(id) ON DELETE SET NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'enabled',
    remark TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_channel_code ON channels(channel_code) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_channel_name ON channels(channel_name);
CREATE INDEX IF NOT EXISTS idx_channel_status ON channels(status);
CREATE INDEX IF NOT EXISTS idx_channels_deleted_at ON channels(deleted_at);

COMMENT ON TABLE channels IS '渠道表';

-- =====================================================
-- 2. 管理員用戶表
-- =====================================================
CREATE TABLE IF NOT EXISTS admin_users (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(50) NOT NULL,
    CONSTRAINT uni_admin_users_username UNIQUE (username),
    password VARCHAR(255) NOT NULL,
    salt VARCHAR(32) NOT NULL,
    status VARCHAR(20) DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'locked')),
    last_login_at TIMESTAMP NULL,
    login_attempts INT DEFAULT 0,
    locked_until TIMESTAMP NULL,
    avatar VARCHAR(500) DEFAULT NULL,
    real_name VARCHAR(100) DEFAULT NULL,
    phone VARCHAR(20) DEFAULT NULL,
    department VARCHAR(100) DEFAULT NULL,
    position VARCHAR(100) DEFAULT NULL,
    channel_id BIGINT REFERENCES channels(id) ON DELETE SET NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_admin_users_username ON admin_users(username);
CREATE INDEX IF NOT EXISTS idx_admin_users_status ON admin_users(status);
CREATE INDEX IF NOT EXISTS idx_admin_users_channel_id ON admin_users(channel_id);

COMMENT ON TABLE admin_users IS '管理員表';

-- =====================================================
-- 3. 角色表
-- =====================================================
CREATE TABLE IF NOT EXISTS roles (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(50) NOT NULL,
    display_name VARCHAR(100),
    CONSTRAINT uni_roles_name UNIQUE (name),
    description TEXT,
    is_system BOOLEAN DEFAULT FALSE,
    sort_order INT DEFAULT 0,
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_roles_name ON roles(name);
CREATE INDEX IF NOT EXISTS idx_roles_status ON roles(status);

COMMENT ON TABLE roles IS '角色表';

-- =====================================================
-- 4. 權限表
-- =====================================================
CREATE TABLE IF NOT EXISTS permissions (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    display_name VARCHAR(100),
    code VARCHAR(100) NOT NULL,
    CONSTRAINT uni_permissions_name UNIQUE (name),
    CONSTRAINT uni_permissions_code UNIQUE (code),
    resource VARCHAR(100) NOT NULL,
    action VARCHAR(50) NOT NULL,
    description TEXT,
    module VARCHAR(50),
    category VARCHAR(50),
    is_system BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_permissions_code ON permissions(code);
CREATE INDEX IF NOT EXISTS idx_permissions_resource ON permissions(resource);
CREATE INDEX IF NOT EXISTS idx_permissions_module ON permissions(module);

COMMENT ON TABLE permissions IS '權限表';

-- =====================================================
-- 5. 管理員角色關聯表
-- =====================================================
CREATE TABLE IF NOT EXISTS admin_roles (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    granted_by BIGINT REFERENCES admin_users(id) ON DELETE SET NULL,
    granted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    is_primary BOOLEAN DEFAULT FALSE,
    data_scope TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT unique_admin_role UNIQUE (admin_id, role_id)
);

CREATE INDEX IF NOT EXISTS idx_admin_roles_admin ON admin_roles(admin_id);
CREATE INDEX IF NOT EXISTS idx_admin_roles_role ON admin_roles(role_id);

COMMENT ON TABLE admin_roles IS '管理員角色關聯表';

-- =====================================================
-- 6. 角色權限關聯表
-- =====================================================
CREATE TABLE IF NOT EXISTS role_permissions (
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id BIGINT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (role_id, permission_id)
);

CREATE INDEX IF NOT EXISTS idx_role_permissions_role ON role_permissions(role_id);
CREATE INDEX IF NOT EXISTS idx_role_permissions_permission ON role_permissions(permission_id);

COMMENT ON TABLE role_permissions IS '角色權限關聯表';

-- =====================================================
-- 7. 登錄會話表
-- =====================================================
CREATE TABLE IF NOT EXISTS admin_sessions (
    id VARCHAR(128) PRIMARY KEY,
    admin_id BIGINT NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    ip_address VARCHAR(45),
    user_agent TEXT,
    device_info JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    last_activity TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    is_active BOOLEAN DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_admin_sessions_admin ON admin_sessions(admin_id);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_expires ON admin_sessions(expires_at);

COMMENT ON TABLE admin_sessions IS '管理員會話表';

-- =====================================================
-- 8. 登錄日誌表
-- =====================================================
CREATE TABLE IF NOT EXISTS login_logs (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT REFERENCES admin_users(id) ON DELETE SET NULL,
    username VARCHAR(50),
    ip_address VARCHAR(45),
    user_agent TEXT,
    status VARCHAR(20) NOT NULL CHECK (status IN ('success', 'failed', 'locked', 'blocked')),
    failure_reason VARCHAR(100),
    session_id VARCHAR(128),
    login_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    location VARCHAR(200)
);

CREATE INDEX IF NOT EXISTS idx_login_logs_admin ON login_logs(admin_id);
CREATE INDEX IF NOT EXISTS idx_login_logs_time ON login_logs(login_time);

COMMENT ON TABLE login_logs IS '登錄日誌表';

-- =====================================================
-- 9. JWT Token 黑名單表
-- =====================================================
CREATE TABLE IF NOT EXISTS token_blacklist (
    id BIGSERIAL PRIMARY KEY,
    token_id VARCHAR(128) NOT NULL,
    CONSTRAINT uni_token_blacklist_token_id UNIQUE (token_id),
    admin_id BIGINT NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    token_type VARCHAR(20) NOT NULL CHECK (token_type IN ('access', 'refresh')),
    revoked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    reason VARCHAR(100)
);

CREATE INDEX IF NOT EXISTS idx_token_blacklist_admin ON token_blacklist(admin_id);
CREATE INDEX IF NOT EXISTS idx_token_blacklist_expires ON token_blacklist(expires_at);

COMMENT ON TABLE token_blacklist IS 'JWT Token 黑名單表';

-- =====================================================
-- 10. 權限變更日誌表
-- =====================================================
CREATE TABLE IF NOT EXISTS permission_change_logs (
    id BIGSERIAL PRIMARY KEY,
    change_type VARCHAR(50) NOT NULL,
    operator_id BIGINT NOT NULL,
    target_id BIGINT NOT NULL,
    before_value TEXT,
    after_value TEXT,
    reason TEXT,
    ip_address VARCHAR(45),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_perm_change_logs_operator ON permission_change_logs(operator_id);
CREATE INDEX IF NOT EXISTS idx_perm_change_logs_created ON permission_change_logs(created_at);

COMMENT ON TABLE permission_change_logs IS '權限變更日誌表';

-- =====================================================
-- 11. 權限拒絕日誌表
-- =====================================================
CREATE TABLE IF NOT EXISTS permission_deny_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    resource VARCHAR(100) NOT NULL,
    action VARCHAR(50) NOT NULL,
    reason TEXT,
    ip_address VARCHAR(45),
    user_agent TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_perm_deny_logs_user ON permission_deny_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_perm_deny_logs_created ON permission_deny_logs(created_at);

COMMENT ON TABLE permission_deny_logs IS '權限拒絕日誌表';

-- =====================================================
-- 創建權限相關表的觸發器
-- =====================================================
DROP TRIGGER IF EXISTS update_admin_users_updated_at ON admin_users;
CREATE TRIGGER update_admin_users_updated_at BEFORE UPDATE ON admin_users FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_roles_updated_at ON roles;
CREATE TRIGGER update_roles_updated_at BEFORE UPDATE ON roles FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_permissions_updated_at ON permissions;
CREATE TRIGGER update_permissions_updated_at BEFORE UPDATE ON permissions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_admin_roles_updated_at ON admin_roles;
CREATE TRIGGER update_admin_roles_updated_at BEFORE UPDATE ON admin_roles FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- 初始化數據 - 角色
-- =====================================================
INSERT INTO roles (name, display_name, description, is_system, sort_order, status) VALUES
('super_admin', '超級管理員', '系統最高權限角色，可以管理所有功能', TRUE, 1, 'active'),
('operator', '運營人員', '負責日常運營和用戶管理', TRUE, 2, 'active')
ON CONFLICT (name) DO NOTHING;

-- =====================================================
-- 初始化數據 - 權限
-- =====================================================
INSERT INTO permissions (name, code, resource, action, description, module) VALUES
-- 儀表盤
('查看儀表盤', 'dashboard.view', 'dashboard', 'view', '查看儀表盤數據', 'dashboard'),

-- 後管用戶管理
('查看後管用戶', 'admin_user.view', 'admin_user', 'view', '查看後管用戶列表', 'admin_user_management'),
('創建後管用戶', 'admin_user.create', 'admin_user', 'create', '創建新的後管用戶', 'admin_user_management'),
('修改後管用戶', 'admin_user.update', 'admin_user', 'update', '修改後管用戶信息', 'admin_user_management'),
('刪除後管用戶', 'admin_user.delete', 'admin_user', 'delete', '刪除後管用戶', 'admin_user_management'),
('更新用戶狀態', 'admin_user.update_status', 'admin_user', 'update_status', '啟用/禁用用戶帳號', 'admin_user_management'),
('重置用戶密碼', 'admin_user.reset_password', 'admin_user', 'reset_password', '重置後管用戶密碼', 'admin_user_management'),

-- 角色管理
('查看角色', 'role.view', 'role', 'view', '查看角色列表', 'permission_management'),
('創建角色', 'role.create', 'role', 'create', '創建新角色', 'permission_management'),
('修改角色', 'role.update', 'role', 'update', '修改角色信息', 'permission_management'),
('刪除角色', 'role.delete', 'role', 'delete', '刪除角色', 'permission_management'),
('分配權限', 'role.assign_permission', 'role', 'assign_permission', '為角色分配權限', 'permission_management'),
('分配角色', 'user_role.assign', 'user_role', 'assign', '為用戶分配角色', 'permission_management'),

-- 帳號管理
('查看所有帳號', 'account.view_all', 'account', 'view_all', '查看所有帳號（不受渠道隔離限制）', 'account_management'),
('查看已分配帳號', 'account.view_assigned', 'account', 'view_assigned', '僅查看已分配給自己的帳號', 'account_management'),
('讀取帳號', 'account.read', 'account', 'read', '讀取帳號資訊', 'account_management'),
('寫入帳號', 'account.write', 'account', 'write', '寫入帳號資訊', 'account_management'),
('刪除帳號', 'account.delete', 'account', 'delete', '刪除帳號', 'account_management'),
('管理帳號分配', 'account.manage_assignments', 'account', 'manage_assignments', '管理帳號與操作員的分配關係', 'account_management'),

-- 消息管理
('查看消息', 'message.view', 'message', 'view', '查看消息列表和WhatsApp帳號', 'message'),
('發送消息', 'message.send', 'message', 'send', '發送消息、連接帳號、同步數據', 'message'),
('刪除消息', 'message.delete', 'message', 'delete', '刪除消息', 'message'),
('撤銷消息', 'message.revoke', 'message', 'revoke', '撤銷已發送的消息', 'message'),

-- 標籤管理
('查看標籤', 'tag.view', 'tag', 'view', '查看標籤列表', 'tag_management'),
('創建標籤', 'tag.create', 'tag', 'create', '創建新標籤', 'tag_management'),
('修改標籤', 'tag.update', 'tag', 'update', '修改標籤', 'tag_management'),
('刪除標籤', 'tag.delete', 'tag', 'delete', '刪除標籤', 'tag_management'),

-- 渠道管理
('查看渠道', 'channel.view', 'channel', 'view', '查看渠道列表', 'channel_management'),
('創建渠道', 'channel.create', 'channel', 'create', '創建新渠道', 'channel_management'),
('修改渠道', 'channel.update', 'channel', 'update', '修改渠道信息', 'channel_management'),
('刪除渠道', 'channel.delete', 'channel', 'delete', '刪除渠道', 'channel_management'),
('配置渠道隔離', 'channel.config', 'channel', 'config', '配置渠道隔離開關', 'channel_management'),

-- 內容審核
('查看敏感詞', 'content_moderation.word_view', 'content_moderation', 'word_view', '查看敏感詞列表', 'content_moderation'),
('創建敏感詞', 'content_moderation.word_create', 'content_moderation', 'word_create', '創建敏感詞', 'content_moderation'),
('修改敏感詞', 'content_moderation.word_update', 'content_moderation', 'word_update', '修改敏感詞', 'content_moderation'),
('刪除敏感詞', 'content_moderation.word_delete', 'content_moderation', 'word_delete', '刪除敏感詞', 'content_moderation'),
('查看敏感詞告警', 'content_moderation.alert_view', 'content_moderation', 'alert_view', '查看敏感詞告警列表', 'content_moderation'),
('查看告警詳情', 'content_moderation.alert_detail', 'content_moderation', 'alert_detail', '查看告警詳情', 'content_moderation'),
('查看告警統計', 'content_moderation.alert_stats', 'content_moderation', 'alert_stats', '查看告警統計', 'content_moderation'),
('重發告警通知', 'content_moderation.alert_resend', 'content_moderation', 'alert_resend', '重發告警通知', 'content_moderation'),

-- 批量發送
('查看批量發送', 'batch_send.view', 'batch_send', 'view', '查看批量發送任務', 'batch_send'),
('創建批量發送', 'batch_send.create', 'batch_send', 'create', '創建批量發送任務', 'batch_send'),
('執行批量發送', 'batch_send.execute', 'batch_send', 'execute', '執行批量發送任務', 'batch_send'),
('暫停批量發送', 'batch_send.pause', 'batch_send', 'pause', '暫停批量發送任務', 'batch_send'),
('恢復批量發送', 'batch_send.resume', 'batch_send', 'resume', '恢復批量發送任務', 'batch_send'),
('刪除批量發送', 'batch_send.delete', 'batch_send', 'delete', '刪除批量發送任務', 'batch_send'),

-- 推廣域名管理
('查看推廣域名', 'promotion_domain.view', 'promotion_domain', 'view', '查看推廣域名列表', 'promotion_domain'),
('創建推廣域名', 'promotion_domain.create', 'promotion_domain', 'create', '創建推廣域名', 'promotion_domain'),
('修改推廣域名', 'promotion_domain.update', 'promotion_domain', 'update', '修改推廣域名', 'promotion_domain'),
('刪除推廣域名', 'promotion_domain.delete', 'promotion_domain', 'delete', '刪除推廣域名', 'promotion_domain'),

-- 系統
('查看系統配置', 'system.config_view', 'system', 'config_view', '查看系統配置', 'system'),
('修改系統配置', 'system.config_update', 'system', 'config_update', '修改系統配置', 'system'),
('查看操作日誌', 'system.log_view', 'system', 'log_view', '查看操作日誌', 'system'),
('系統管理員', 'system.admin', 'system', 'admin', '系統管理員權限', 'system')
ON CONFLICT (code) DO NOTHING;

-- =====================================================
-- 為超級管理員分配所有權限
-- =====================================================
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'super_admin'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- =====================================================
-- 為運營人員分配基礎權限
-- =====================================================
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'operator'
  AND p.code IN (
    'account.read',
    'message.view', 'message.send',
    'tag.view', 'tag.create', 'tag.update'
  )
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- =====================================================
-- 創建默認超級管理員帳戶
-- 用戶名: admin, 密碼: Admin123456 (BCrypt hash)
-- =====================================================
INSERT INTO admin_users (username, password, salt, real_name, status) VALUES
('admin', '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewdBPj32.rLIyG1C', 'default_salt', '系統管理員', 'active')
ON CONFLICT (username) DO NOTHING;

-- =====================================================
-- 為默認管理員分配超級管理員角色
-- =====================================================
INSERT INTO admin_roles (admin_id, role_id, granted_by)
SELECT au.id, r.id, au.id
FROM admin_users au
CROSS JOIN roles r
WHERE au.username = 'admin'
  AND r.name = 'super_admin'
ON CONFLICT (admin_id, role_id) DO NOTHING;

-- =====================================================
-- 初始化系統配置
-- =====================================================
INSERT INTO system_configs (config_key, config_value, description) VALUES
('telegram.bot_token', '', 'Telegram Bot API Token'),
('telegram.chat_id', '', 'Telegram 群組 Chat ID'),
('telegram.enabled', 'false', 'Telegram 通知總開關'),
('sensitive_word.enabled', 'true', '敏感詞監控開關'),
('sensitive_word.notify_telegram', 'true', '是否發送 Telegram 通知'),
('channel_isolation_enabled', 'true', '渠道隔離開關：true=啟用，false=禁用')
ON CONFLICT (config_key) DO NOTHING;

COMMIT;

-- =====================================================
-- 驗證初始化結果
-- =====================================================
SELECT '===== 初始化驗證 =====' AS status;
SELECT '角色數量: ' || COUNT(*)::TEXT FROM roles WHERE deleted_at IS NULL;
SELECT '權限數量: ' || COUNT(*)::TEXT FROM permissions WHERE deleted_at IS NULL;
SELECT '超級管理員權限數: ' || COUNT(*)::TEXT FROM role_permissions rp JOIN roles r ON rp.role_id = r.id WHERE r.name = 'super_admin';
SELECT '===== 初始化完成 =====' AS status;
