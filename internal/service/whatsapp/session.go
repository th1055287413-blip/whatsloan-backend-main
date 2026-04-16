package whatsapp

import (
	"fmt"
	"time"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
)

// CleanupExpiredSessions 清理过期会话
func (s *whatsappService) CleanupExpiredSessions() (int, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	now := time.Now()
	expireTime := 10 * time.Minute         // 未完成会话10分钟后过期
	completedExpireTime := 5 * time.Minute // 已完成会话5分钟后过期
	cleanedCount := 0

	// 清理过期的二维码会话
	for sessionID, session := range s.qrSessions {
		shouldClean := false
		reason := ""

		if session.accountID != nil {
			// 已完成的会话，5分钟后清理
			if now.Sub(session.createdAt) > completedExpireTime {
				shouldClean = true
				reason = "已完成會話超過5分鐘"
			}
		} else {
			// 未完成的会话，10分钟后清理
			if now.Sub(session.createdAt) > expireTime {
				shouldClean = true
				reason = "未完成會話超過10分鐘"
			}
		}

		if shouldClean {
			if session.client != nil {
				session.client.Disconnect()
			}
			delete(s.qrSessions, sessionID)
			logger.Infow("清理二維碼會話", "session_id", sessionID, "reason", reason)
			cleanedCount++
		}
	}

	// 清理过期的配对会话
	for sessionID, session := range s.pairingSessions {
		shouldClean := false
		reason := ""

		if session.accountID != nil {
			// 已完成的会话，5分钟后清理
			if now.Sub(session.createdAt) > completedExpireTime {
				shouldClean = true
				reason = "已完成會話超過5分鐘"
			}
		} else {
			// 未完成的会话，10分钟后清理
			if now.Sub(session.createdAt) > expireTime {
				shouldClean = true
				reason = "未完成會話超過10分鐘"
			}
		}

		if shouldClean {
			if session.client != nil {
				session.client.Disconnect()
			}
			delete(s.pairingSessions, sessionID)
			logger.Infow("清理配對會話", "session_id", sessionID, "reason", reason)
			cleanedCount++
		}
	}

	return cleanedCount, nil
}

// GetSessionStatus 获取会话状态
func (s *whatsappService) GetSessionStatus(sessionID string) (*SessionStatus, error) {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()

	// 检查二维码会话
	if session, exists := s.qrSessions[sessionID]; exists {
		// 如果会话已完成（accountID 不为空），返回账号的登录状态
		if session.accountID != nil {
			logger.Debugw("QR 會話已完成，查詢帳號狀態",
				"session_id", sessionID, "account_id", *session.accountID)
			var account model.WhatsAppAccount
			if dbErr := s.db.GetDB().First(&account, *session.accountID).Error; dbErr == nil {
				s.mu.RLock()
				client, exists := s.clients[account.ID]
				isLoggedIn := exists && client != nil && client.IsLoggedIn()
				s.mu.RUnlock()

				logger.Debugw("帳號登入狀態",
					"account_id", account.ID, "is_logged_in", isLoggedIn)
				platform := account.Platform
				if platform == "" {
					platform = "browser"
				}
				status := &SessionStatus{
					Connected:    isLoggedIn,
					JID:          account.DeviceID,
					PushName:     account.PhoneNumber,
					Platform:     platform,
					IsBusiness:   account.IsBusiness(),
					BusinessName: account.BusinessName,
					LastSeen:     account.LastConnected,
					Avatar:       account.Avatar,
					AvatarID:     account.AvatarID,
				}
				return status, nil
			} else {
				logger.Warnw("查詢帳號失敗",
					"account_id", *session.accountID, "error", dbErr)
			}
		}

		// 会话尚未完成，检查是否已登录
		// Connected字段应该表示是否已登录认证，而不是WebSocket连接状态
		isLoggedIn := session.client != nil && session.client.IsLoggedIn()

		platform := "browser"
		if session.client != nil && session.client.Store.Platform != "" {
			platform = session.client.Store.Platform
		}
		status := &SessionStatus{
			Connected: isLoggedIn,
			Platform:  platform,
			LastSeen:  session.createdAt,
		}
		if session.client != nil && session.client.Store.ID != nil {
			status.JID = session.client.Store.ID.String()
			status.PushName = session.client.Store.PushName
			status.BusinessName = session.client.Store.BusinessName
			status.IsBusiness = platform == "smba" || platform == "smbi"

			// 如果已登录,从数据库加载头像信息
			if isLoggedIn {
				deviceID := session.client.Store.ID.String()
				var account model.WhatsAppAccount
				if err := s.db.GetDB().Where("device_id = ?", deviceID).First(&account).Error; err == nil {
					status.Avatar = account.Avatar
					status.AvatarID = account.AvatarID
				}
			}
		}
		return status, nil
	}

	// 检查配对会话
	if session, exists := s.pairingSessions[sessionID]; exists {
		// 如果会话已完成（accountID 不为空），返回账号的登录状态
		if session.accountID != nil {
			logger.Debugw("配對會話已完成，查詢帳號狀態",
				"session_id", sessionID, "account_id", *session.accountID)
			var account model.WhatsAppAccount
			if dbErr := s.db.GetDB().First(&account, *session.accountID).Error; dbErr == nil {
				s.mu.RLock()
				client, exists := s.clients[account.ID]
				isLoggedIn := exists && client != nil && client.IsLoggedIn()
				s.mu.RUnlock()

				logger.Debugw("帳號登入狀態",
					"account_id", account.ID, "is_logged_in", isLoggedIn)
				platform := account.Platform
				if platform == "" {
					platform = "mobile"
				}
				status := &SessionStatus{
					Connected:    isLoggedIn,
					JID:          account.DeviceID,
					PushName:     account.PhoneNumber,
					Platform:     platform,
					IsBusiness:   account.IsBusiness(),
					BusinessName: account.BusinessName,
					LastSeen:     account.LastConnected,
					Avatar:       account.Avatar,
					AvatarID:     account.AvatarID,
				}
				return status, nil
			} else {
				logger.Warnw("查詢帳號失敗",
					"account_id", *session.accountID, "error", dbErr)
			}
		}

		// 会话尚未完成，检查是否已登录
		// Connected字段应该表示是否已登录认证，而不是WebSocket连接状态
		isLoggedIn := session.client != nil && session.client.IsLoggedIn()

		platform := "mobile"
		if session.client != nil && session.client.Store.Platform != "" {
			platform = session.client.Store.Platform
		}
		status := &SessionStatus{
			Connected: isLoggedIn,
			Platform:  platform,
			LastSeen:  session.createdAt,
		}
		if session.client != nil && session.client.Store.ID != nil {
			status.JID = session.client.Store.ID.String()
			status.PushName = session.client.Store.PushName
			status.BusinessName = session.client.Store.BusinessName
			status.IsBusiness = platform == "smba" || platform == "smbi"

			// 如果已登录,从数据库加载头像信息
			if isLoggedIn {
				deviceID := session.client.Store.ID.String()
				var account model.WhatsAppAccount
				if err := s.db.GetDB().Where("device_id = ?", deviceID).First(&account).Error; err == nil {
					status.Avatar = account.Avatar
					status.AvatarID = account.AvatarID
				}
			}
		}
		return status, nil
	}

	return nil, fmt.Errorf("会话不存在")
}

// DisconnectSession 断开会话连接
func (s *whatsappService) DisconnectSession(sessionID string) error {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	// 检查并断开二维码会话
	if session, exists := s.qrSessions[sessionID]; exists {
		if session.client != nil {
			session.client.Disconnect()
		}
		delete(s.qrSessions, sessionID)
		logger.Infow("斷開二維碼會話", "session_id", sessionID)
		return nil
	}

	// 检查并断开配对会话
	if session, exists := s.pairingSessions[sessionID]; exists {
		if session.client != nil {
			session.client.Disconnect()
		}
		delete(s.pairingSessions, sessionID)
		logger.Infow("斷開配對會話", "session_id", sessionID)
		return nil
	}

	return fmt.Errorf("会话不存在")
}

// RestoreSession 恢复会话连接
func (s *whatsappService) RestoreSession(sessionID string) error {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()

	// 检查二维码会话
	if session, exists := s.qrSessions[sessionID]; exists {
		if session.client != nil && !session.client.IsConnected() {
			return session.client.Connect()
		}
		return fmt.Errorf("会话已连接或无效")
	}

	// 检查配对会话
	if session, exists := s.pairingSessions[sessionID]; exists {
		if session.client != nil && !session.client.IsConnected() {
			return session.client.Connect()
		}
		return fmt.Errorf("会话已连接或无效")
	}

	return fmt.Errorf("会话不存在")
}

// cleanupQRSession 清理指定的二维码会话
func (s *whatsappService) cleanupQRSession(sessionID string) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	if session, exists := s.qrSessions[sessionID]; exists {
		if session.client != nil {
			session.client.Disconnect()
		}
		delete(s.qrSessions, sessionID)
		logger.Infow("清理二維碼會話", "session_id", sessionID)
	}
}
