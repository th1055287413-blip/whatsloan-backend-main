package messaging

import (
	"context"
	"errors"
	"fmt"
	"time"

	"whatsapp_golang/internal/database"
	"whatsapp_golang/internal/gateway"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// MessageActionService 消息操作服务接口
type MessageActionService interface {
	DeleteMessage(ctx context.Context, messageID uint, deletedBy string) error
	DeleteMessageForMe(ctx context.Context, messageID uint, deletedBy string) error
	RevokeMessage(ctx context.Context, messageID uint, accountID uint, revokedBy string) error
}

// messageActionService 消息操作服务实现
type messageActionService struct {
	db      database.Database
	gateway *gateway.Gateway
}

// NewMessageActionService 创建消息操作服务
func NewMessageActionService(db database.Database, gw *gateway.Gateway) MessageActionService {
	return &messageActionService{
		db:      db,
		gateway: gw,
	}
}

// DeleteMessage 删除消息(管理员操作)
func (s *messageActionService) DeleteMessage(ctx context.Context, messageID uint, deletedBy string) error {
	// 1. 先查询消息(不在事务中,因为需要先调用WhatsApp API)
	var message model.WhatsAppMessage
	if err := s.db.GetDB().First(&message, messageID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("消息不存在")
		}
		logger.Ctx(ctx).Errorw("查詢訊息失敗", "message_id", messageID, "error", err)
		return fmt.Errorf("查询消息失败: %w", err)
	}

	// 2. 检查是否已删除
	if message.DeletedAt != nil {
		return fmt.Errorf("消息已被删除")
	}

	// 3. 先發送 DeleteForMe 刪除原訊息（在所有裝置上）
	if err := s.deleteMessageForMeOnWhatsApp(ctx, &message); err != nil {
		logger.Ctx(ctx).Warnw("WhatsApp DeleteForMe 失敗", "message_id", messageID, "error", err)
	}

	// 3.1 等待 1 秒讓 DeleteForMe 生效
	time.Sleep(1 * time.Second)

	// 3.2 再撤銷訊息（讓對方也看不到）
	if err := s.revokeMessageOnWhatsApp(ctx, &message); err != nil {
		logger.Ctx(ctx).Errorw("WhatsApp 撤回訊息失敗", "message_id", messageID, "error", err)
		// 继续执行数据库删除,即使WhatsApp API调用失败
	}

	// 4. 数据库软删除（保留原始內容供管理員查看）
	return s.db.GetDB().Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Model(&message).Updates(map[string]interface{}{
			"deleted_at": now,
			"deleted_by": deletedBy,
			"is_revoked": true,
			"revoked_at": now,
			// 不清空 content，讓管理員仍可查看原始內容
		}).Error; err != nil {
			logger.Ctx(ctx).Errorw("刪除訊息失敗", "message_id", messageID, "error", err)
			return fmt.Errorf("删除消息失败: %w", err)
		}

		// 5. 更新会话的 LastMessage
		if err := s.updateChatLastMessage(tx, message.ChatID); err != nil {
			logger.Ctx(ctx).Errorw("更新會話 LastMessage 失敗", "chat_id", message.ChatID, "error", err)
			// 不阻塞删除操作,只记录错误
		}

		logger.Ctx(ctx).Infow("訊息已刪除", "message_id", messageID, "deleted_by", deletedBy)
		return nil
	})
}

// DeleteMessageForMe 僅刪除自己裝置上的訊息（不撤銷對方）
func (s *messageActionService) DeleteMessageForMe(ctx context.Context, messageID uint, deletedBy string) error {
	var message model.WhatsAppMessage
	if err := s.db.GetDB().First(&message, messageID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("消息不存在")
		}
		return fmt.Errorf("查询消息失败: %w", err)
	}

	if message.DeletedAt != nil {
		return fmt.Errorf("消息已被刪除")
	}

	if err := s.deleteMessageForMeOnWhatsApp(ctx, &message); err != nil {
		logger.Ctx(ctx).Errorw("WhatsApp DeleteForMe 失敗", "message_id", messageID, "error", err)
		return fmt.Errorf("WhatsApp DeleteForMe 失敗: %w", err)
	}

	return s.db.GetDB().Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Model(&message).Updates(map[string]interface{}{
			"deleted_at": now,
			"deleted_by": deletedBy,
		}).Error; err != nil {
			return fmt.Errorf("删除消息失败: %w", err)
		}

		if err := s.updateChatLastMessage(tx, message.ChatID); err != nil {
			logger.Ctx(ctx).Errorw("更新會話 LastMessage 失敗", "chat_id", message.ChatID, "error", err)
		}

		logger.Ctx(ctx).Infow("訊息已 DeleteForMe", "message_id", messageID, "deleted_by", deletedBy)
		return nil
	})
}

// RevokeMessage 撤销消息(用户操作)
func (s *messageActionService) RevokeMessage(ctx context.Context, messageID uint, accountID uint, revokedBy string) error {
	// 1. 先查询消息(不在事务中,因为需要先调用WhatsApp API)
	var message model.WhatsAppMessage
	if err := s.db.GetDB().First(&message, messageID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("消息不存在")
		}
		logger.Ctx(ctx).Errorw("查詢訊息失敗", "message_id", messageID, "error", err)
		return fmt.Errorf("查询消息失败: %w", err)
	}

	// 2. 如果accountID为0，使用消息自身的accountID（管理员操作）
	if accountID == 0 {
		accountID = message.AccountID
		logger.Ctx(ctx).Infow("管理員撤回訊息", "message_id", messageID, "account_id", accountID)
	} else {
		// 验证是否是消息发送者（普通用户操作）
		if message.AccountID != accountID {
			return fmt.Errorf("只能撤销自己发送的消息")
		}
	}

	// 3. 验证是否是发送的消息
	if !message.IsFromMe {
		return fmt.Errorf("只能撤销自己发送的消息")
	}

	// 4. 检查是否已删除或撤销
	if message.DeletedAt != nil {
		return fmt.Errorf("消息已被删除,无法撤销")
	}
	if message.IsRevoked {
		return fmt.Errorf("消息已被撤销")
	}

	// 5. 检查时间限制(24小时)
	revokeWindow := 24 * time.Hour
	if time.Since(message.Timestamp) > revokeWindow {
		return fmt.Errorf("超过撤销时间限制(24小时)")
	}

	// 6. 调用 WhatsApp API 撤销消息
	if err := s.revokeMessageOnWhatsApp(ctx, &message); err != nil {
		logger.Ctx(ctx).Errorw("WhatsApp 撤回訊息失敗", "message_id", messageID, "error", err)
		return fmt.Errorf("WhatsApp撤销消息失败: %w", err)
	}

	// 注意：撤回后 WhatsApp 會在發送方裝置顯示「您已刪除此訊息」佔位符
	// 這是 WhatsApp 客戶端行為，無法透過 API 刪除

	// 7. 数据库更新
	return s.db.GetDB().Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Model(&message).Updates(map[string]interface{}{
			"is_revoked": true,
			"revoked_at": now,
			"revoked_by": revokedBy,
		}).Error; err != nil {
			logger.Ctx(ctx).Errorw("撤回訊息失敗", "message_id", messageID, "error", err)
			return fmt.Errorf("撤销消息失败: %w", err)
		}

		// 8. 更新会话的 LastMessage
		if err := s.updateChatLastMessage(tx, message.ChatID); err != nil {
			logger.Ctx(ctx).Errorw("更新會話 LastMessage 失敗", "chat_id", message.ChatID, "error", err)
			// 不阻塞撤销操作,只记录错误
		}

		logger.Ctx(ctx).Infow("訊息已撤回", "message_id", messageID, "account_id", accountID)
		return nil
	})
}

// updateChatLastMessage 更新会话的最后一条消息
func (s *messageActionService) updateChatLastMessage(tx *gorm.DB, chatID uint) error {
	// 查询该会话的最新一条有效消息(未删除且未撤销)
	var lastMsg model.WhatsAppMessage
	err := tx.Where("chat_id = ? AND deleted_at IS NULL AND is_revoked = ?", chatID, false).
		Order("timestamp DESC").
		First(&lastMsg).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 没有有效消息了,清空 LastMessage
			return tx.Model(&model.WhatsAppChat{}).Where("id = ?", chatID).
				Updates(map[string]interface{}{
					"last_message": "",
					"last_time":    time.Time{},
				}).Error
		}
		return fmt.Errorf("查询最新消息失败: %w", err)
	}

	// 更新为最新有效消息
	lastMessageText := lastMsg.Content
	if lastMsg.Type != "text" {
		// 非文本消息显示类型
		typeLabels := map[string]string{
			"image":    "[图片]",
			"video":    "[视频]",
			"audio":    "[音频]",
			"document": "[文档]",
			"location": "[位置]",
			"contact":  "[联系人]",
		}
		if label, ok := typeLabels[lastMsg.Type]; ok {
			lastMessageText = label
		} else {
			lastMessageText = "[消息]"
		}
	}

	return tx.Model(&model.WhatsAppChat{}).Where("id = ?", chatID).
		Updates(map[string]interface{}{
			"last_message": lastMessageText,
			"last_time":    lastMsg.Timestamp,
		}).Error
}

// revokeMessageOnWhatsApp 在 WhatsApp 上撤销消息
func (s *messageActionService) revokeMessageOnWhatsApp(ctx context.Context, message *model.WhatsAppMessage) error {
	// 檢查 Gateway 是否可用
	if s.gateway == nil {
		return fmt.Errorf("Gateway 未初始化")
	}

	// 確定聊天 JID（優先使用 ToJID，如果為空則用 FromJID）
	chatJID := message.ToJID
	if chatJID == "" {
		chatJID = message.FromJID
	}
	if chatJID == "" {
		return fmt.Errorf("无法确定聊天JID")
	}

	// 透過 Gateway 發送撤銷命令到 Connector
	if err := s.gateway.RevokeMessage(ctx, message.AccountID, chatJID, message.MessageID); err != nil {
		return fmt.Errorf("发送撤销命令失败: %w", err)
	}

	logger.Ctx(ctx).Debugw("WhatsApp 撤回命令已發送", "message_id", message.MessageID, "chat_jid", chatJID)
	return nil
}

// deleteMessageForMeOnWhatsApp 在 WhatsApp 上執行 DeleteForMe（清除撤回後的佔位訊息）
func (s *messageActionService) deleteMessageForMeOnWhatsApp(ctx context.Context, message *model.WhatsAppMessage) error {
	if s.gateway == nil {
		return fmt.Errorf("Gateway 未初始化")
	}

	chatJID := message.ToJID
	if chatJID == "" {
		chatJID = message.FromJID
	}
	if chatJID == "" {
		return fmt.Errorf("无法确定聊天JID")
	}

	senderJID := ""
	if !message.IsFromMe {
		senderJID = message.FromJID
	}

	logger.Ctx(ctx).Debugw("DeleteForMe 準備調用",
		"account_id", message.AccountID, "chat_jid", chatJID, "message_id", message.MessageID,
		"sender_jid", senderJID, "is_from_me", message.IsFromMe, "timestamp", message.Timestamp.Unix(),
		"from_jid", message.FromJID, "to_jid", message.ToJID)

	if err := s.gateway.DeleteMessageForMe(ctx, message.AccountID, chatJID, message.MessageID, senderJID, message.IsFromMe, message.Timestamp.Unix()); err != nil {
		return fmt.Errorf("发送DeleteForMe命令失败: %w", err)
	}

	logger.Ctx(ctx).Debugw("WhatsApp DeleteForMe 命令已發送", "message_id", message.MessageID, "chat_jid", chatJID)
	return nil
}
