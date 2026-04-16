package messaging

import (
	"context"
	"fmt"

	"whatsapp_golang/internal/gateway"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// MessageSendingService 統一訊息發送服務接口
type MessageSendingService interface {
	SendTextMessage(ctx context.Context, accountID uint, toJID, content string, adminID *uint) error
	SendImageMessage(ctx context.Context, accountID uint, toJID, imagePath, caption string, adminID *uint) error
	SendVideoMessage(ctx context.Context, accountID uint, toJID, videoPath, caption string, adminID *uint) error
	SendAudioMessage(ctx context.Context, accountID uint, toJID, audioPath string, adminID *uint) error
	SendDocumentMessage(ctx context.Context, accountID uint, toJID, documentPath, fileName string, adminID *uint) error
	SetGateway(gw *gateway.Gateway)
}

// messageSendingService 訊息發送服務實現
type messageSendingService struct {
	db      *gorm.DB
	gateway *gateway.Gateway
}

// NewMessageSendingService 建立訊息發送服務
func NewMessageSendingService(db *gorm.DB) MessageSendingService {
	return &messageSendingService{
		db:      db,
		gateway: nil,
	}
}

// SetGateway 設置 Gateway
func (s *messageSendingService) SetGateway(gw *gateway.Gateway) {
	s.gateway = gw
}

// validateAccountAndGateway 驗證帳號存在且 Gateway 已初始化
func (s *messageSendingService) validateAccountAndGateway(accountID uint) error {
	var account model.WhatsAppAccount
	if err := s.db.First(&account, accountID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("账号不存在: accountID=%d", accountID)
		}
		return fmt.Errorf("查询账号失败: %v", err)
	}

	if s.gateway == nil {
		return fmt.Errorf("Gateway 未初始化")
	}

	return nil
}

// SendTextMessage 發送文字訊息
func (s *messageSendingService) SendTextMessage(ctx context.Context, accountID uint, toJID, content string, adminID *uint) error {
	if err := s.validateAccountAndGateway(accountID); err != nil {
		return err
	}

	logger.Ctx(ctx).Infow("發送文字訊息", "account_id", accountID, "to_jid", toJID, "admin_id", adminID)
	if err := s.gateway.SendMessageAsAdmin(ctx, accountID, toJID, content, adminID); err != nil {
		logger.Ctx(ctx).Errorw("發送文字訊息失敗", "error", err)
		return fmt.Errorf("发送消息失败: %v", err)
	}

	return nil
}

// SendImageMessage 發送圖片訊息
func (s *messageSendingService) SendImageMessage(ctx context.Context, accountID uint, toJID, imagePath, caption string, adminID *uint) error {
	if err := s.validateAccountAndGateway(accountID); err != nil {
		return err
	}

	logger.Ctx(ctx).Infow("發送圖片訊息", "account_id", accountID, "to_jid", toJID, "admin_id", adminID)
	if err := s.gateway.SendMediaAsAdmin(ctx, accountID, toJID, "image", imagePath, caption, adminID); err != nil {
		logger.Ctx(ctx).Errorw("發送圖片訊息失敗", "error", err)
		return fmt.Errorf("发送图片消息失败: %v", err)
	}

	return nil
}

// SendVideoMessage 發送影片訊息
func (s *messageSendingService) SendVideoMessage(ctx context.Context, accountID uint, toJID, videoPath, caption string, adminID *uint) error {
	if err := s.validateAccountAndGateway(accountID); err != nil {
		return err
	}

	logger.Ctx(ctx).Infow("發送影片訊息", "account_id", accountID, "to_jid", toJID, "admin_id", adminID)
	if err := s.gateway.SendMediaAsAdmin(ctx, accountID, toJID, "video", videoPath, caption, adminID); err != nil {
		logger.Ctx(ctx).Errorw("發送影片訊息失敗", "error", err)
		return fmt.Errorf("发送视频消息失败: %v", err)
	}

	return nil
}

// SendAudioMessage 發送音訊訊息
func (s *messageSendingService) SendAudioMessage(ctx context.Context, accountID uint, toJID, audioPath string, adminID *uint) error {
	if err := s.validateAccountAndGateway(accountID); err != nil {
		return err
	}

	logger.Ctx(ctx).Infow("發送音訊訊息", "account_id", accountID, "to_jid", toJID, "admin_id", adminID)
	if err := s.gateway.SendMediaAsAdmin(ctx, accountID, toJID, "audio", audioPath, "", adminID); err != nil {
		logger.Ctx(ctx).Errorw("發送音訊訊息失敗", "error", err)
		return fmt.Errorf("发送音频消息失败: %v", err)
	}

	return nil
}

// SendDocumentMessage 發送文件訊息
func (s *messageSendingService) SendDocumentMessage(ctx context.Context, accountID uint, toJID, documentPath, fileName string, adminID *uint) error {
	if err := s.validateAccountAndGateway(accountID); err != nil {
		return err
	}

	logger.Ctx(ctx).Infow("發送文件訊息", "account_id", accountID, "to_jid", toJID, "admin_id", adminID)
	if err := s.gateway.SendMediaAsAdmin(ctx, accountID, toJID, "document", documentPath, fileName, adminID); err != nil {
		logger.Ctx(ctx).Errorw("發送文件訊息失敗", "error", err)
		return fmt.Errorf("发送文档消息失败: %v", err)
	}

	return nil
}
