package whatsapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
	"time"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"go.mau.fi/whatsmeow"
	"gorm.io/gorm"
)

// 重試配置從 s.config.Retry 中讀取

// isSQLiteBusyError 检查是否为SQLite忙碌错误
func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "database is locked") ||
		strings.Contains(errStr, "sqlite_busy") ||
		strings.Contains(errStr, "database busy") ||
		strings.Contains(errStr, "busy")
}

// retryDatabaseOperation 帶指數退避的資料庫操作重試機制
func (s *whatsappService) retryDatabaseOperation(operation func() error, operationName string) error {
	var lastErr error

	maxAttempts := 5
	baseDelay := 100 * time.Millisecond
	maxDelay := 5 * time.Second

	for attempt := 0; attempt < maxAttempts; attempt++ {
		err := operation()
		if err == nil {
			if attempt > 0 {
				logger.Infow("資料庫重試成功",
					"operation", operationName, "attempt", attempt+1)
			}
			return nil
		}

		lastErr = err

		if !isSQLiteBusyError(err) {
			if err != gorm.ErrRecordNotFound {
				logger.Errorw("資料庫不可重試錯誤",
					"operation", operationName, "error", err)
			}
			return err
		}

		delay := time.Duration(1<<uint(attempt)) * baseDelay
		if delay > maxDelay {
			delay = maxDelay
		}

		logger.Warnw("資料庫重試",
			"operation", operationName, "attempt", attempt+1,
			"error", err, "retry_after", delay)

		time.Sleep(delay)
	}

	logger.Errorw("資料庫重試最終失敗",
		"operation", operationName, "max_attempts", maxAttempts, "error", lastErr)
	return fmt.Errorf("数据库操作重试失败 (%s): %w", operationName, lastErr)
}

// generateSessionID 生成唯一的會話 ID
func (s *whatsappService) generateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// updateAccountStatus 安全地更新帳號狀態（帶鎖保護和事務包裝）
func (s *whatsappService) updateAccountStatus(accountID uint, updates map[string]interface{}) error {
	accountLock := s.accountLocks.getLock(accountID)
	accountLock.Lock()
	defer accountLock.Unlock()

	tx := s.db.GetDB().Begin()
	if tx.Error != nil {
		logger.WithAccount(accountID).Errorw("開始事務失敗", "error", tx.Error)
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			logger.WithAccount(accountID).Errorw("狀態更新遇到異常，事務已回滾", "panic", r)
		}
	}()

	result := tx.Model(&model.WhatsAppAccount{}).Where("id = ?", accountID).Updates(updates)
	if result.Error != nil {
		tx.Rollback()
		logger.WithAccount(accountID).Errorw("更新帳號狀態失敗，事務已回滾", "error", result.Error)
		return result.Error
	}

	if err := tx.Commit().Error; err != nil {
		logger.WithAccount(accountID).Errorw("提交帳號狀態更新事務失敗", "error", err)
		return err
	}

	return nil
}

// getDeviceInfo 根据运行环境返回合适的设备类型和显示名称
func (s *whatsappService) getDeviceInfo() (whatsmeow.PairClientType, string) {
	osName := runtime.GOOS

	switch osName {
	case "darwin":
		return whatsmeow.PairClientSafari, "Safari (macOS)"
	case "windows":
		return whatsmeow.PairClientEdge, "Edge (Windows)"
	case "linux":
		return whatsmeow.PairClientChrome, "Chrome (Linux)"
	case "android":
		return whatsmeow.PairClientChrome, "Chrome (Android)"
	default:
		return whatsmeow.PairClientChrome, "Chrome (Linux)"
	}
}

// fetchAccountInfo 獲取帳號基本資訊
func (s *whatsappService) fetchAccountInfo(accountID uint) {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	log := logger.WithAccount(accountID)

	if !exists || !client.IsConnected() || !client.IsLoggedIn() {
		log.Warnw("帳號未連接，無法獲取基本資訊")
		return
	}

	log.Infow("開始獲取帳號基本資訊")

	userJID := client.Store.ID
	if userJID != nil {
		log.Infow("用戶 JID 資訊",
			"jid", userJID.String(), "user", userJID.User, "device", userJID.Device)

		deviceID := userJID.String()
		if err := s.db.GetDB().Model(&model.WhatsAppAccount{}).
			Where("id = ?", accountID).
			Update("device_id", deviceID).Error; err != nil {
			log.Errorw("更新 device_id 失敗", "error", err)
		} else {
			log.Infow("device_id 已更新", "device_id", deviceID)
		}
	} else {
		log.Warnw("無法獲取用戶 JID")
	}

	deviceStore := client.Store
	if deviceStore != nil {
		log.Infow("設備存儲資訊",
			"platform", deviceStore.Platform,
			"business_name", deviceStore.BusinessName,
			"push_name", deviceStore.PushName)
	}

	ctx := context.Background()
	contacts, err := client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		log.Errorw("獲取聯絡人失敗", "error", err)
	} else {
		log.Infow("聯絡人數量", "count", len(contacts))
	}

	log.Infow("連接狀態詳情",
		"is_connected", client.IsConnected(),
		"is_logged_in", client.IsLoggedIn(),
		"connect_time", time.Now().Format("2006-01-02 15:04:05"))

	log.Infow("基本資訊獲取完成")
}
