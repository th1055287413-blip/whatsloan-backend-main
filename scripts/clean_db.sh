#!/bin/bash
# 清除 WhatsApp 資料庫資料（測試用）

# 資料庫連接參數
DB_HOST="192.168.31.30"
DB_PORT="5432"
DB_USER="whatsapp"
DB_NAME="whatsapp"
DB_PASS="2djTSxRTQTsMpbJF"

export PGPASSWORD="$DB_PASS"

echo "=== WhatsApp DB 清理工具 ==="
echo ""
echo "選擇清理模式："
echo "1) 只清除消息和聊天記錄（保留帳號）"
echo "2) 清除所有 WhatsApp 資料（帳號、聊天、消息）"
echo "3) 完全刪除所有資料表（重啟後自動重建）"
echo "4) 取消"
echo ""
read -p "請輸入選項 [1-4]: " choice

case $choice in
    1)
        echo "清除消息和聊天記錄..."
        psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" << 'EOF'
TRUNCATE TABLE whatsapp_messages CASCADE;
TRUNCATE TABLE whatsapp_chats CASCADE;
-- 注意: whatsapp_contacts 已移除，聯絡人由 whatsmeow_contacts 自動管理
EOF
        echo "✅ 消息和聊天記錄已清除"
        ;;
    2)
        echo "清除所有 WhatsApp 資料..."
        psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" << 'EOF'
TRUNCATE TABLE whatsapp_messages CASCADE;
TRUNCATE TABLE whatsapp_chats CASCADE;
TRUNCATE TABLE whatsapp_devices CASCADE;
TRUNCATE TABLE whatsapp_account_tags CASCADE;
TRUNCATE TABLE sensitive_word_alerts CASCADE;
TRUNCATE TABLE whatsapp_accounts CASCADE;
-- 注意: whatsapp_contacts 已移除，聯絡人由 whatsmeow_contacts 自動管理
EOF
        rm -rf db/sessions/*.db 2>/dev/null
        echo "✅ 所有 WhatsApp 資料已清除"
        ;;
    3)
        echo "刪除所有資料表..."
        psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" << 'EOF'
-- 應用資料表
DROP TABLE IF EXISTS whatsapp_messages CASCADE;
DROP TABLE IF EXISTS whatsapp_chats CASCADE;
-- 注意: whatsapp_contacts 已移除，聯絡人由 whatsmeow_contacts 自動管理
DROP TABLE IF EXISTS whatsapp_devices CASCADE;
DROP TABLE IF EXISTS whatsapp_account_tags CASCADE;
DROP TABLE IF EXISTS whatsapp_accounts CASCADE;
DROP TABLE IF EXISTS sensitive_word_alerts CASCADE;
DROP TABLE IF EXISTS sensitive_words CASCADE;
DROP TABLE IF EXISTS translation_cache CASCADE;
DROP TABLE IF EXISTS translation_configs CASCADE;
DROP TABLE IF EXISTS language_configs CASCADE;
DROP TABLE IF EXISTS account_tags CASCADE;
DROP TABLE IF EXISTS system_configs CASCADE;
DROP TABLE IF EXISTS user_data CASCADE;
DROP TABLE IF EXISTS users CASCADE;
DROP TABLE IF EXISTS admin_users CASCADE;
DROP TABLE IF EXISTS roles CASCADE;
DROP TABLE IF EXISTS role_permissions CASCADE;
DROP TABLE IF EXISTS permissions CASCADE;
DROP TABLE IF EXISTS permission_change_logs CASCADE;
DROP TABLE IF EXISTS user_roles CASCADE;
DROP TABLE IF EXISTS permission_deny_logs CASCADE;

-- whatsmeow 資料表
DROP TABLE IF EXISTS whatsmeow_device_identity_keys CASCADE;
DROP TABLE IF EXISTS whatsmeow_pre_keys CASCADE;
DROP TABLE IF EXISTS whatsmeow_sender_keys CASCADE;
DROP TABLE IF EXISTS whatsmeow_sessions CASCADE;
DROP TABLE IF EXISTS whatsmeow_app_state_sync_keys CASCADE;
DROP TABLE IF EXISTS whatsmeow_app_state_version CASCADE;
DROP TABLE IF EXISTS whatsmeow_app_state_mutation_macs CASCADE;
DROP TABLE IF EXISTS whatsmeow_contacts CASCADE;
DROP TABLE IF EXISTS whatsmeow_chat_settings CASCADE;
DROP TABLE IF EXISTS whatsmeow_message_secrets CASCADE;
DROP TABLE IF EXISTS whatsmeow_privacy_tokens CASCADE;
DROP TABLE IF EXISTS whatsmeow_device CASCADE;
DROP TABLE IF EXISTS whatsmeow_version CASCADE;
DROP TABLE IF EXISTS whatsmeow_identity_keys CASCADE;
DROP TABLE IF EXISTS whatsmeow_lid_map CASCADE;
DROP TABLE IF EXISTS whatsmeow_event_buffer CASCADE;
EOF
            # 清除本地檔案
            rm -rf db/sessions/*.db 2>/dev/null
            rm -rf uploads/media/* 2>/dev/null

            echo "✅ 所有資料表已刪除"
            echo ""
            echo "請重啟後端服務以重建資料表："
            echo "  go run main.go"
        ;;
    4)
        echo "已取消"
        exit 0
        ;;
    *)
        echo "無效選項"
        exit 1
        ;;
esac

echo ""
echo "當前資料表數量："
psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c "SELECT COUNT(*) as tables FROM pg_tables WHERE schemaname = 'public';"
