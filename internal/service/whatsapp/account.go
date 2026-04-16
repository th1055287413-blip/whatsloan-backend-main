package whatsapp

import (
	"context"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"gorm.io/gorm"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
)

// GetAccounts 获取所有账号（排除已刪除）
func (s *whatsappService) GetAccounts() ([]*model.WhatsAppAccount, error) {
	var accounts []*model.WhatsAppAccount
	err := s.db.GetDB().Where("status != ?", "deleted").Find(&accounts).Error
	return accounts, err
}

// GetAccount 获取单个账号
func (s *whatsappService) GetAccount(id uint) (*model.WhatsAppAccount, error) {
	var account model.WhatsAppAccount
	err := s.db.GetDB().First(&account, id).Error
	return &account, err
}

// ConnectAccount 連接帳號
func (s *whatsappService) ConnectAccount(accountID uint) error {
	log := logger.WithAccount(accountID)
	log.Debugw("開始連接帳號")
	s.mu.Lock()
	defer s.mu.Unlock()

	if client, exists := s.clients[accountID]; exists && client.IsConnected() {
		log.Debugw("帳號已連接，跳過")
		return nil
	}

	if oldClient, exists := s.clients[accountID]; exists {
		log.Debugw("存在舊客戶端，先斷開清理")
		oldClient.Disconnect()
		oldClient.RemoveEventHandlers()
		delete(s.clients, accountID)
	}

	var account model.WhatsAppAccount
	err := s.retryDatabaseOperation(func() error {
		return s.db.GetDB().First(&account, accountID).Error
	}, fmt.Sprintf("获取账号信息 %d", accountID))

	if err != nil {
		log.Errorw("帳號不存在", "error", err)
		return fmt.Errorf("账号不存在: %v", err)
	}

	log.Debugw("帳號資料已獲取",
		"phone", account.PhoneNumber, "device_id", account.DeviceID)

	deviceStore := s.container.NewDevice()

	if account.DeviceID != "" {
		log.Debugw("嘗試恢復設備", "device_id", account.DeviceID)
		deviceJID, parseErr := types.ParseJID(account.DeviceID)
		if parseErr == nil {
			existingDevice, err := s.container.GetDevice(context.Background(), deviceJID)
			if err == nil && existingDevice != nil {
				deviceStore = existingDevice
				log.Infow("成功恢復設備", "device_jid", deviceJID.String())
			} else {
				log.Warnw("無法恢復設備，使用新設備", "error", err)
			}
		} else {
			log.Warnw("設備 ID 格式錯誤", "error", parseErr)
		}
	} else {
		log.Debugw("沒有 DeviceID，使用新設備")
	}

	if deviceStore.ID != nil {
		account.DeviceID = deviceStore.ID.String()
		log.Debugw("更新 DeviceID", "device_id", account.DeviceID)
		s.retryDatabaseOperation(func() error {
			return s.db.GetDB().Model(&account).Update("device_id", account.DeviceID).Error
		}, fmt.Sprintf("更新账号 %d 设备ID", accountID))
	}

	log.Debugw("建立 WhatsApp 客戶端")
	waLogLevel := getWhatsAppLogLevel(s.config.Log.Level)
	clientLog := waLog.Stdout("Client-"+account.PhoneNumber, waLogLevel, true)
	client := whatsmeow.NewClient(deviceStore, clientLog)
	client.AddEventHandler(s.createEventHandler(accountID))

	log.Debugw("開始調用 client.Connect()")
	err = client.Connect()
	if err != nil {
		log.Errorw("連接失敗", "error", err)
		return fmt.Errorf("连接失败: %v", err)
	}

	log.Debugw("client.Connect() 成功，加入 clients map")
	s.clients[accountID] = client

	s.retryDatabaseOperation(func() error {
		return s.db.GetDB().Model(&account).Updates(map[string]interface{}{
			"status":         "connected",
			"last_connected": time.Now(),
		}).Error
	}, fmt.Sprintf("更新账号 %d 连接状态", accountID))

	log.Infow("連接流程完成", "phone", account.PhoneNumber)
	return nil
}

// DisconnectAccount 斷開帳號連接
func (s *whatsappService) DisconnectAccount(accountID uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, exists := s.clients[accountID]
	if !exists {
		return fmt.Errorf("账号未连接")
	}

	client.Disconnect()
	delete(s.clients, accountID)

	s.retryDatabaseOperation(func() error {
		return s.db.GetDB().Model(&model.WhatsAppAccount{}).Where("id = ?", accountID).Updates(map[string]interface{}{
			"status": "disconnected",
		}).Error
	}, fmt.Sprintf("更新账号 %d 断开状态", accountID))

	return nil
}

// GetAccountStatus 獲取帳號狀態
func (s *whatsappService) GetAccountStatus(accountID uint) (string, error) {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	log := logger.WithAccount(accountID)
	log.Debugw("檢查帳號狀態", "exists", exists)
	if exists {
		log.Debugw("客戶端狀態",
			"is_logged_in", client.IsLoggedIn(), "is_connected", client.IsConnected())
	}

	if !exists {
		return "disconnected", nil
	}

	if client.IsLoggedIn() && client.IsConnected() {
		return "connected", nil
	}

	return "connecting", nil
}

// connectExistingAccounts 連接現有帳號並同步資料
func (s *whatsappService) connectExistingAccounts() {
	var accounts []*model.WhatsAppAccount
	if err := s.db.GetDB().Find(&accounts).Error; err != nil {
		logger.Errorw("獲取帳號列表失敗", "error", err)
		return
	}

	logger.Infow("專案啟動時發現已關聯帳號", "count", len(accounts))

	var activeAccounts []*model.WhatsAppAccount
	for _, acc := range accounts {
		if acc.Status == "logged_out" || acc.Status == "deleted" {
			logger.Infow("帳號狀態不適合連接，跳過", "phone", acc.PhoneNumber, "status", acc.Status)
			continue
		}
		activeAccounts = append(activeAccounts, acc)
	}

	if len(activeAccounts) == 0 {
		logger.Infow("沒有需要連接的帳號")
		return
	}

	if s.syncQueue != nil {
		logger.Infow("使用 Redis Stream 限速隊列連接帳號", "count", len(activeAccounts))
		for _, acc := range activeAccounts {
			if err := s.syncQueue.EnqueueAccountConnect(s.syncQueueCtx, acc.ID); err != nil {
				logger.Errorw("入隊帳號連接任務失敗",
					"phone", acc.PhoneNumber, "error", err)
			}
		}
		return
	}

	logger.Warnw("Redis Stream 隊列未啟用，直接連接帳號（可能導致封號風險）",
		"count", len(activeAccounts))
	for _, acc := range activeAccounts {
		go func(account *model.WhatsAppAccount) {
			if err := s.ConnectAccount(account.ID); err != nil {
				logger.Errorw("連接帳號失敗",
					"phone", account.PhoneNumber, "error", err)
			}
		}(acc)
		time.Sleep(5 * time.Second)
	}
}

// updateAccountStatusOnLogout 更新帳號退出狀態
func (s *whatsappService) updateAccountStatusOnLogout(phoneNumber string) error {
	var account model.WhatsAppAccount
	db := s.db.GetDB()

	if err := db.Where("phone_number = ?", phoneNumber).First(&account).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			logger.Warnw("未找到退出的帳號", "phone", phoneNumber)
			return nil
		}
		return fmt.Errorf("查找账号失败: %v", err)
	}

	now := time.Now()
	updates := map[string]interface{}{
		"status":   "logged_out",
		"last_seen": now,
	}

	if err := db.Model(&account).Updates(updates).Error; err != nil {
		return fmt.Errorf("更新状态失败: %v", err)
	}

	logger.Infow("帳號已更新為退出狀態",
		"phone", phoneNumber, "account_id", account.ID)
	return nil
}

// cleanupAccountWhatsAppData 清理帳號的所有 WhatsApp 相關資料
func (s *whatsappService) cleanupAccountWhatsAppData(accountID uint) {
	log := logger.WithAccount(accountID)
	log.Infow("開始清理 WhatsApp 資料")

	db := s.db.GetDB()

	if err := db.Where("account_id = ?", accountID).Delete(&model.WhatsAppMessage{}).Error; err != nil {
		log.Errorw("清理訊息資料失敗", "error", err)
	} else {
		log.Infow("成功清理訊息資料")
	}

	if err := db.Where("account_id = ?", accountID).Delete(&model.WhatsAppChat{}).Error; err != nil {
		log.Errorw("清理聊天資料失敗", "error", err)
	} else {
		log.Infow("成功清理聊天資料")
	}

	log.Infow("WhatsApp 資料清理完成，帳號記錄已保留但狀態為 logged_out")
}
