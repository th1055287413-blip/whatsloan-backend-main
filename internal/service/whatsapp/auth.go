package whatsapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"gorm.io/gorm"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
)

// GetPairingCode 获取配对代码
func (s *whatsappService) GetPairingCode(phoneNumber, channelCode string) (string, string, error) {
	// 清理过期会话
	go func() {
		_, err := s.CleanupExpiredSessions()
		if err != nil {
			logger.Errorw("清理過期會話失敗", "error", err)
		}
	}()

	s.mu.RLock()
	// 检查账号是否已存在
	var account model.WhatsAppAccount
	result := s.db.GetDB().Where("phone_number = ?", phoneNumber).First(&account)
	s.mu.RUnlock()

	if result.Error == nil {
		return "", "", fmt.Errorf("该手机号已绑定")
	}

	// 生成会话ID
	sessionID := s.generateSessionID()

	// 创建新的设备存储
	deviceStore := s.container.NewDevice()
	waLogLevel := getWhatsAppLogLevel(s.config.Log.Level)
	clientLog := waLog.Stdout("Client-"+phoneNumber+"-"+sessionID[:8], waLogLevel, true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	// 添加事件处理器（用于自动验证）
	client.AddEventHandler(s.createPairingEventHandler(phoneNumber, sessionID))

	// 先连接客户端
	err := client.Connect()
	if err != nil {
		return "", "", fmt.Errorf("连接WhatsApp失败: %v", err)
	}

	// 等待连接完全建立 - 添加重试机制
	maxRetries := 10
	retryDelay := 500 * time.Millisecond
	connected := false

	for i := 0; i < maxRetries; i++ {
		if client.IsConnected() {
			connected = true
			logger.Infow("WebSocket 連接已建立", "attempt", i+1, "max_retries", maxRetries)
			break
		}
		logger.Debugw("等待 WebSocket 連接建立", "attempt", i+1, "max_retries", maxRetries)
		time.Sleep(retryDelay)
	}

	if !connected {
		client.Disconnect()
		return "", "", fmt.Errorf("WebSocket 连接超时: 在 %d 次尝试后仍未建立连接", maxRetries)
	}

	// 获取设备信息
	clientType, displayName := s.getDeviceInfo()

	// 请求配对代码
	code, err := client.PairPhone(context.Background(), phoneNumber, true, clientType, displayName)
	if err != nil {
		client.Disconnect() // 如果获取配对代码失败，断开连接
		return "", "", fmt.Errorf("获取配对代码失败: %v", err)
	}

	// 保存会话
	s.sessionMu.Lock()
	s.pairingSessions[sessionID] = &pairingSession{
		client:      client,
		phoneNumber: phoneNumber,
		createdAt:   time.Now(),
		channelCode: channelCode, // 保存渠道码，登录成功后用于绑定用户到渠道
	}
	s.sessionMu.Unlock()

	logger.Infow("生成配對代碼", "phone", phoneNumber, "code", code, "session_id", sessionID, "channel_code", channelCode)
	return code, sessionID, nil
}

// VerifyPairingCode 验证配对代码
func (s *whatsappService) VerifyPairingCode(sessionID, phoneNumber, code string) error {
	// 获取会话
	s.sessionMu.RLock()
	session, exists := s.pairingSessions[sessionID]
	s.sessionMu.RUnlock()

	if !exists {
		return fmt.Errorf("未找到配对会话，请重新获取配对代码")
	}

	// 验证手机号是否匹配
	if session.phoneNumber != phoneNumber {
		return fmt.Errorf("手机号不匹配")
	}

	client := session.client
	// 检查客户端是否已连接，如果未连接则连接
	if !client.IsConnected() {
		err := client.Connect()
		if err != nil {
			return fmt.Errorf("连接WhatsApp失败: %v", err)
		}
	}

	// 等待登录完成
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			client.Disconnect()
			// 清理会话
			s.sessionMu.Lock()
			delete(s.pairingSessions, sessionID)
			s.sessionMu.Unlock()
			return fmt.Errorf("验证超时")
		case <-ticker.C:
			if client.IsLoggedIn() {
				// 登录成功，保存账号信息(头像将在后续数据同步时获取)
				s.mu.Lock()
				account := &model.WhatsAppAccount{
					PhoneNumber:   phoneNumber,
					DeviceID:      client.Store.ID.String(),
					Status:        "connected",
					LastConnected: time.Now(),
				}

				// 保存账号信息（使用重试机制）
				createErr := s.retryDatabaseOperation(func() error {
					return s.db.GetDB().Create(account).Error
				}, fmt.Sprintf("配对保存账号 %s", phoneNumber))

				if createErr != nil {
					s.mu.Unlock()
					client.Disconnect()
					// 清理会话
					s.sessionMu.Lock()
					delete(s.pairingSessions, sessionID)
					s.sessionMu.Unlock()
					return fmt.Errorf("保存账号信息失败: %v", createErr)
				}

				// 更新客户端映射
				s.clients[account.ID] = client
				s.mu.Unlock()

				// 清理会话
				s.sessionMu.Lock()
				delete(s.pairingSessions, sessionID)
				s.sessionMu.Unlock()

				// 更新事件处理器
				client.RemoveEventHandlers()
				client.AddEventHandler(s.createEventHandler(account.ID))

				// 新关联账号时自动同步所有会话和消息
				go s.syncNewAccountData(account.ID)

				logger.Infow("帳號驗證成功", "phone", phoneNumber, "account_id", account.ID, "session_id", sessionID)
				return nil
			}
		}
	}
}

// GetQRCode 获取二维码
func (s *whatsappService) GetQRCode(channelCode string) (string, string, error) {
	logger.Infow("開始 QR 碼登入流程")

	// 清理过期会话
	go func() {
		_, err := s.CleanupExpiredSessions()
		if err != nil {
			logger.Errorw("清理過期會話失敗", "error", err)
		}
	}()

	// 生成会话ID
	sessionID := s.generateSessionID()
	logger.Infow("生成新的會話 ID", "session_id", sessionID)

	// 创建新的设备存储 - 确保每次都是全新未认证的设备
	logger.Debugw("正在建立新的設備存儲", "session_id", sessionID)
	deviceStore := s.container.NewDevice()

	// 验证设备是否为全新状态
	if deviceStore.ID != nil {
		logger.Warnw("檢測到已認證設備，強制建立新設備存儲", "session_id", sessionID)
		// 重新创建一个新的设备存储
		deviceStore = s.container.NewDevice()
		// 如果还是有ID，则清空它
		if deviceStore.ID != nil {
			deviceStore.ID = nil
			logger.Infow("已清空設備 ID 以確保全新狀態", "session_id", sessionID)
		}
	} else {
		logger.Debugw("設備存儲狀態正常（未認證）", "session_id", sessionID)
	}

	logger.Debugw("正在建立 WhatsApp 客戶端", "session_id", sessionID)
	waLogLevel := getWhatsAppLogLevel(s.config.Log.Level)
	clientLog := waLog.Stdout("QR-Client-"+sessionID[:8], waLogLevel, true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	// 添加事件处理器
	logger.Debugw("正在新增事件處理器", "session_id", sessionID)
	client.AddEventHandler(s.createQREventHandler(sessionID))

	// 获取二维码通道
	logger.Debugw("正在取得 QR 通道", "session_id", sessionID)
	qrChan, err := client.GetQRChannel(context.Background())
	if err != nil {
		logger.Errorw("取得二維碼通道失敗", "session_id", sessionID, "error", err)
		return "", "", fmt.Errorf("获取二维码通道失败: %v", err)
	}

	// 连接客户端
	logger.Debugw("正在連接 WhatsApp 伺服器", "session_id", sessionID)
	err = client.Connect()
	if err != nil {
		logger.Errorw("連接 WhatsApp 失敗", "session_id", sessionID, "error", err)
		return "", "", fmt.Errorf("连接WhatsApp失败: %v", err)
	}
	logger.Infow("WhatsApp 連接成功，等待 QR 碼", "session_id", sessionID)

	// 保存会话
	s.sessionMu.Lock()
	s.qrSessions[sessionID] = &qrSession{
		client:      client,
		qrChan:      qrChan,
		createdAt:   time.Now(),
		channelCode: strings.TrimSpace(channelCode),
	}
	s.sessionMu.Unlock()

	// 等待第一个二维码
	logger.Debugw("等待二維碼生成", "session_id", sessionID)
	select {
	case evt := <-qrChan:
		logger.Infow("收到 QR 事件", "event", evt.Event, "session_id", sessionID)
		switch evt.Event {
		case "code":
			logger.Infow("二維碼生成成功", "code", evt.Code, "session_id", sessionID)
			return evt.Code, sessionID, nil
		case "timeout":
			logger.Warnw("二維碼取得逾時", "session_id", sessionID)
			client.Disconnect()
			// 清理会话
			s.sessionMu.Lock()
			delete(s.qrSessions, sessionID)
			s.sessionMu.Unlock()
			return "", "", fmt.Errorf("二维码获取超时")
		default:
			logger.Warnw("未知事件", "event", evt.Event, "session_id", sessionID)
			return "", "", fmt.Errorf("未知事件: %s", evt.Event)
		}
	case <-time.After(30 * time.Second):
		logger.Warnw("等待二維碼逾時", "timeout_sec", 30, "session_id", sessionID)
		client.Disconnect()
		// 清理会话
		s.sessionMu.Lock()
		delete(s.qrSessions, sessionID)
		s.sessionMu.Unlock()
		return "", "", fmt.Errorf("等待二维码超时")
	}
}

// VerifyQRCode 验证二维码登录状态
func (s *whatsappService) VerifyQRCode(sessionID string) error {
	// 如果没有提供sessionID，检查所有活跃的二维码会话
	if sessionID == "" {
		s.sessionMu.RLock()
		defer s.sessionMu.RUnlock()

		if len(s.qrSessions) == 0 {
			return fmt.Errorf("未找到二维码会话，请重新获取二维码")
		}

		// 检查所有会话，找到最新的一个
		var latestSession *qrSession
		var latestSessionID string
		var latestTime time.Time

		for id, session := range s.qrSessions {
			if session.createdAt.After(latestTime) {
				latestTime = session.createdAt
				latestSession = session
				latestSessionID = id
			}
		}

		if latestSession == nil {
			return fmt.Errorf("未找到有效的二维码会话")
		}

		sessionID = latestSessionID

		// 监听二维码事件（移除过早的IsLoggedIn检查，只通过事件处理登录）
		if latestSession.qrChan != nil {
			select {
			case evt := <-latestSession.qrChan:
				logger.Infow("QR 驗證收到事件", "event", evt.Event, "session_id", sessionID)
				switch evt.Event {
				case "success":
					logger.Infow("二維碼登入成功", "session_id", sessionID)
					return s.saveQRAccount(latestSession.client, sessionID)
				case "timeout":
					logger.Warnw("二維碼已過期", "session_id", sessionID)
					s.cleanupQRSession(sessionID)
					return fmt.Errorf("二维码已过期")
				case "code":
					logger.Infow("二維碼已更新", "session_id", sessionID)
					// 新的二维码，返回需要更新
					return fmt.Errorf("二维码已更新")
				default:
					logger.Warnw("未知事件", "event", evt.Event, "session_id", sessionID)
					return fmt.Errorf("未知事件: %s", evt.Event)
				}
			default:
				logger.Debugw("沒有新事件，繼續等待掃描")
				// 没有新事件，返回等待状态
				return fmt.Errorf("等待扫描")
			}
		}

		return fmt.Errorf("二维码会话无效")
	}

	// 原有的逻辑：使用提供的sessionID
	logger.Debugw("查找指定的會話", "session_id", sessionID)
	s.sessionMu.RLock()
	session, exists := s.qrSessions[sessionID]
	s.sessionMu.RUnlock()

	if !exists {
		logger.Warnw("未找到會話", "session_id", sessionID)
		return fmt.Errorf("未找到二维码会话，请重新获取二维码")
	}
	logger.Debugw("找到會話，開始監聽事件", "session_id", sessionID)

	client := session.client
	qrChan := session.qrChan

	// 监听二维码事件（移除过早的IsLoggedIn检查，只通过事件处理登录）
	if qrChan != nil {
		select {
		case evt := <-qrChan:
			logger.Infow("QR 驗證收到事件", "event", evt.Event, "session_id", sessionID)
			switch evt.Event {
			case "success":
				logger.Infow("二維碼登入成功", "session_id", sessionID)
				return s.saveQRAccount(client, sessionID)
			case "timeout":
				logger.Warnw("二維碼已過期", "session_id", sessionID)
				s.cleanupQRSession(sessionID)
				return fmt.Errorf("二维码已过期")
			case "code":
				logger.Infow("二維碼已更新", "session_id", sessionID)
				// 新的二维码，返回需要更新
				return fmt.Errorf("二维码已更新")
			default:
				logger.Warnw("未知事件", "event", evt.Event, "session_id", sessionID)
				return fmt.Errorf("未知事件: %s", evt.Event)
			}
		default:
			logger.Debugw("沒有新事件，繼續等待掃描")
			// 没有新事件，返回等待状态
			return fmt.Errorf("等待扫描")
		}
	}

	return fmt.Errorf("二维码会话无效")
}

// createPairingEventHandler 创建配对过程的事件处理器
func (s *whatsappService) createPairingEventHandler(phoneNumber, sessionID string) func(interface{}) {
	return func(evt interface{}) {
		switch evt.(type) {
		case *events.Connected:
			logger.Infow("配對客戶端已連接", "phone", phoneNumber, "session_id", sessionID)

			// 基于状态检查,如果客户端已登录则立即保存账号
			s.sessionMu.RLock()
			session, exists := s.pairingSessions[sessionID]
			s.sessionMu.RUnlock()

			if !exists {
				logger.Warnw("配對會話不存在或已過期", "phone", phoneNumber, "session_id", sessionID)
				return
			}

			if session.client == nil {
				logger.Warnw("配對會話客戶端為空", "phone", phoneNumber, "session_id", sessionID)
				return
			}

			if !session.client.IsLoggedIn() {
				logger.Debugw("配對客戶端尚未登入", "phone", phoneNumber, "session_id", sessionID)
				return
			}

			logger.Infow("配對客戶端已就緒，開始儲存帳號", "phone", phoneNumber)
			go func() {
				s.autoSaveAccount(phoneNumber, sessionID)

				// 保存账号后获取头像
				s.mu.RLock()
				var account model.WhatsAppAccount
				err := s.db.GetDB().Where("phone_number = ?", phoneNumber).First(&account).Error
				s.mu.RUnlock()

				if err != nil {
					if err == gorm.ErrRecordNotFound {
						logger.Warnw("配對登入: 未找到對應帳號，可能儲存失敗", "phone", phoneNumber)
					} else {
						logger.Errorw("配對登入: 查詢帳號失敗", "phone", phoneNumber, "error", err)
					}
					return
				}

				if account.ID == 0 {
					logger.Warnw("配對登入: 帳號 ID 無效", "phone", phoneNumber)
					return
				}

				logger.Infow("配對登入: 找到帳號，開始取得頭像", "account_id", account.ID)
				s.updateAccountAvatar(account.ID, session.client)
			}()
		case *events.Disconnected:
			logger.Infow("配對客戶端已斷開連接", "session_id", sessionID)
		}
	}
}

// createQREventHandler 创建二维码登录的事件处理器
func (s *whatsappService) createQREventHandler(sessionID string) func(interface{}) {
	return func(evt interface{}) {
		switch v := evt.(type) {
		case *events.PairSuccess:
			// 配对成功事件 - 这是确认登录成功的关键事件
			logger.Infow("QR 配對成功", "pair_id", v.ID, "session_id", sessionID)
			s.sessionMu.RLock()
			if session, exists := s.qrSessions[sessionID]; exists && session.client != nil {
				s.sessionMu.RUnlock()
				logger.Infow("開始儲存二維碼登入帳號", "session_id", sessionID)
				go func() {
					if err := s.saveQRAccount(session.client, sessionID); err != nil {
						logger.Errorw("儲存二維碼帳號失敗", "session_id", sessionID, "error", err)
					} else {
						logger.Infow("帳號儲存成功", "session_id", sessionID)
					}
				}()
			} else {
				s.sessionMu.RUnlock()
			}
		case *events.Connected:
			logger.Infow("二維碼客戶端已連接", "session_id", sessionID)

			// 当客户端连接且已登录时,立即获取头像
			s.sessionMu.RLock()
			session, exists := s.qrSessions[sessionID]
			s.sessionMu.RUnlock()

			if !exists {
				logger.Warnw("會話不存在或已過期", "session_id", sessionID)
				return
			}

			if session.client == nil {
				logger.Warnw("會話客戶端為空", "session_id", sessionID)
				return
			}

			if !session.client.IsLoggedIn() {
				logger.Debugw("客戶端尚未登入", "session_id", sessionID)
				return
			}

			logger.Infow("客戶端已就緒，準備取得頭像", "session_id", sessionID)

			// 查找对应的账号ID
			if session.client.Store.ID == nil {
				logger.Warnw("無法取得手機號: Store.ID 為空", "session_id", sessionID)
				return
			}

			phoneNumber := session.client.Store.ID.User

			// 从数据库查找账号ID
			s.mu.RLock()
			var account model.WhatsAppAccount
			err := s.db.GetDB().Where("phone_number = ?", phoneNumber).First(&account).Error
			s.mu.RUnlock()

			if err != nil {
				if err == gorm.ErrRecordNotFound {
					logger.Warnw("未找到對應帳號，可能儲存尚未完成", "phone", phoneNumber)
				} else {
					logger.Errorw("查詢帳號失敗", "phone", phoneNumber, "error", err)
				}
				return
			}

			if account.ID == 0 {
				logger.Warnw("帳號 ID 無效", "phone", phoneNumber)
				return
			}

			logger.Infow("找到帳號，開始取得頭像", "account_id", account.ID)
			go s.updateAccountAvatar(account.ID, session.client)
		case *events.Disconnected:
			logger.Warnw("二維碼客戶端已斷開連接", "session_id", sessionID)
		case *events.LoggedOut:
			logger.Warnw("二維碼客戶端已登出", "session_id", sessionID)

			// 获取手机号(在锁内部)
			var phoneNumber string
			s.sessionMu.RLock()
			if session, exists := s.qrSessions[sessionID]; exists && session.client != nil && session.client.Store.ID != nil {
				phoneNumber = session.client.Store.ID.User
			}
			s.sessionMu.RUnlock()

			// 更新数据库(锁外部)
			if phoneNumber != "" {
				if err := s.updateAccountStatusOnLogout(phoneNumber); err != nil {
					logger.Errorw("更新退出狀態失敗", "phone", phoneNumber, "error", err)
				}
			}

			s.cleanupQRSession(sessionID)
		case *events.QR:
			logger.Debugw("收到新的二維碼事件", "event", v, "session_id", sessionID)
		}
	}
}

// autoSaveAccount 自动保存账号
func (s *whatsappService) autoSaveAccount(phoneNumber, sessionID string) {
	s.sessionMu.Lock()
	session, exists := s.pairingSessions[sessionID]
	if !exists {
		s.sessionMu.Unlock()
		logger.Errorw("未找到配對會話，無法儲存帳號", "session_id", sessionID)
		return
	}
	client := session.client
	channelCode := session.channelCode // 获取渠道码
	s.sessionMu.Unlock()

	// 检查是否已登录
	if !client.IsLoggedIn() {
		logger.Errorw("客戶端未登入，無法儲存帳號", "session_id", sessionID)
		return
	}

	// 获取客户端的设备ID
	deviceID := client.Store.ID.String()

	// 🔒 渠道隔离：根据渠道码查找渠道ID
	var channelID *uint
	if channelCode != "" {
		var channel model.Channel
		err := s.db.GetDB().
			Where("channel_code = ? AND status = ? AND deleted_at IS NULL", channelCode, "enabled").
			First(&channel).Error
		if err != nil {
			logger.Warnw("渠道碼無效或未啟用，用戶將不綁定渠道", "channel_code", channelCode, "phone", phoneNumber, "error", err)
		} else {
			channelID = &channel.ID
			logger.Infow("用戶將綁定到渠道", "phone", phoneNumber, "channel_name", channel.ChannelName, "channel_id", channel.ID)
		}
	}

	// 创建账号记录
	account := &model.WhatsAppAccount{
		PhoneNumber:   phoneNumber,
		DeviceID:      deviceID,
		Status:        "connected",
		LastConnected: time.Now(),
		Platform:      client.Store.Platform,
		BusinessName:  client.Store.BusinessName,
		ChannelID:     channelID,
	}
	// 渠道码来源于链接，便于后续统计
	if channelCode != "" {
		account.ChannelSource = "link"
	}

	// 保存到数据库（使用重试机制）
	err := s.retryDatabaseOperation(func() error {
		return s.db.GetDB().Create(account).Error
	}, fmt.Sprintf("保存账号 %s", phoneNumber))

	if err != nil {
		logger.Errorw("儲存帳號失敗", "session_id", sessionID, "error", err)
		return
	}

	s.mu.Lock()
	// 更新客户端映射
	s.clients[account.ID] = client
	s.mu.Unlock()

	// 更新事件处理器
	client.RemoveEventHandlers() // 移除旧的事件处理器
	client.AddEventHandler(s.createEventHandler(account.ID))

	// 标记会话已完成，保存账号ID供前端轮询使用
	logger.Infow("標記配對會話完成並儲存帳號 ID", "session_id", sessionID, "account_id", account.ID)
	s.sessionMu.Lock()
	if session, exists := s.pairingSessions[sessionID]; exists {
		session.accountID = &account.ID
	}
	s.sessionMu.Unlock()

	logger.Infow("帳號自動驗證成功", "phone", phoneNumber, "account_id", account.ID, "session_id", sessionID)

	// 异步更新用户资料(头像和用户名)
	go func(accID uint) {
		time.Sleep(2 * time.Second) // 等待连接稳定
		if err := s.UpdateAccountProfile(accID); err != nil {
			logger.Errorw("更新帳號用戶資料失敗", "account_id", accID, "error", err)
		} else {
			logger.Infow("成功更新帳號用戶資料", "account_id", accID)
		}
	}(account.ID)

	// 新帳號關聯後觸發數據同步
	go s.syncNewAccountData(account.ID)
}

// saveQRAccount 保存二维码登录账号
func (s *whatsappService) saveQRAccount(client *whatsmeow.Client, sessionID string) error {
	logger.Infow("開始儲存 QR 碼登入帳號", "session_id", sessionID)

	var channelID *uint
	var channelCode string
	// 读取会话中的渠道码
	s.sessionMu.RLock()
	if session, exists := s.qrSessions[sessionID]; exists {
		channelCode = session.channelCode
	}
	s.sessionMu.RUnlock()

	// 根据渠道码查找渠道
	if channelCode != "" {
		var channel model.Channel
		if err := s.db.GetDB().Where("channel_code = ? AND status = ? AND deleted_at IS NULL", channelCode, "enabled").First(&channel).Error; err == nil {
			channelID = &channel.ID
			logger.Infow("渠道碼綁定渠道", "channel_code", channelCode, "channel_id", channel.ID)
		} else {
			logger.Warnw("渠道碼無效或未啟用，跳過綁定", "channel_code", channelCode)
		}
	}

	// 获取设备信息 (不需要锁)
	logger.Debugw("正在取得設備資訊", "session_id", sessionID)
	deviceID := client.Store.ID.String()
	phoneNumber := ""
	if client.Store.ID != nil {
		phoneNumber = client.Store.ID.User
	}
	logger.Infow("設備資訊", "device_id", deviceID, "phone", phoneNumber)

	// 注意: 头像将在后续的 Connected 事件中获取,此时连接尚未完全稳定

	// 检查账号是否已存在 (使用 phone_number 查找，因为 device_id 在重新扫码时会变化)
	logger.Debugw("正在檢查帳號是否已存在", "phone", phoneNumber, "device_id", deviceID)
	s.mu.RLock()
	var existingAccount model.WhatsAppAccount
	result := s.db.GetDB().Where("phone_number = ?", phoneNumber).First(&existingAccount)
	s.mu.RUnlock()

	if result.Error == nil {
		// 账号已存在，更新状态 (此时已无全局锁)
		logger.Infow("找到現有帳號，開始更新狀態", "account_id", existingAccount.ID)
		updateData := map[string]interface{}{
			"status":         "connected",
			"last_connected": time.Now(),
			"last_seen":      time.Now(),
			"device_id":      deviceID, // 更新 device_id，因为重新扫码后会变化
			"platform":       client.Store.Platform,
			"business_name":  client.Store.BusinessName,
		}
		if channelID != nil && existingAccount.ChannelID == nil {
			updateData["channel_id"] = *channelID
			updateData["channel_source"] = "link"
		}
		err := s.updateAccountStatus(existingAccount.ID, updateData)
		if err != nil {
			logger.Errorw("更新現有帳號狀態失敗", "account_id", existingAccount.ID, "error", err)
			return fmt.Errorf("更新账号状态失败: %v", err)
		}
		logger.Infow("現有帳號狀態更新成功", "account_id", existingAccount.ID)

		// 更新客户端映射
		logger.Debugw("正在更新客戶端映射和事件處理器", "account_id", existingAccount.ID)
		s.mu.Lock()
		s.clients[existingAccount.ID] = client
		s.mu.Unlock()

		// 更新事件处理器
		client.RemoveEventHandlers()
		client.AddEventHandler(s.createEventHandler(existingAccount.ID))

		// 标记会话已完成，保存账号ID供前端轮询使用
		logger.Debugw("標記 QR 會話完成並儲存帳號 ID", "session_id", sessionID, "account_id", existingAccount.ID)
		s.sessionMu.Lock()
		if session, exists := s.qrSessions[sessionID]; exists {
			session.accountID = &existingAccount.ID
		}
		s.sessionMu.Unlock()

		logger.Infow("二維碼登入成功，更新現有帳號", "account_id", existingAccount.ID, "session_id", sessionID)

		// 异步更新用户资料(头像和用户名)
		go func(accID uint) {
			time.Sleep(2 * time.Second) // 等待连接稳定
			if err := s.UpdateAccountProfile(accID); err != nil {
				logger.Errorw("更新帳號用戶資料失敗", "account_id", accID, "error", err)
			} else {
				logger.Infow("成功更新帳號用戶資料", "account_id", accID)
			}
		}(existingAccount.ID)

		// 注意: 联系人头像更新已经在 saveChatsFromStore 函数中自动触发，无需在此重复调用

		// 重新連接時也觸發數據同步（重置同步狀態並開始同步）
		s.syncStatusService.ResetAllSteps(existingAccount.ID)
		go s.syncNewAccountData(existingAccount.ID)

		return nil
	}

	// 创建新账号
	logger.Infow("未找到現有帳號，開始建立新帳號", "phone", phoneNumber)
	account := &model.WhatsAppAccount{
		PhoneNumber:   phoneNumber,
		DeviceID:      deviceID,
		Status:        "connected",
		LastConnected: time.Now(),
		Platform:      client.Store.Platform,
		BusinessName:  client.Store.BusinessName,
		ChannelID:     channelID,
	}
	if channelCode != "" {
		account.ChannelSource = "link"
	}

	logger.Debugw("正在儲存新帳號到資料庫", "phone", phoneNumber)
	if err := s.db.GetDB().Create(account).Error; err != nil {
		logger.Errorw("儲存帳號資訊失敗", "error", err)
		return fmt.Errorf("保存账号信息失败: %v", err)
	}
	logger.Infow("新帳號儲存成功", "account_id", account.ID)

	// 更新客户端映射
	logger.Debugw("正在設定客戶端映射和事件處理器", "account_id", account.ID)
	s.mu.Lock()
	s.clients[account.ID] = client
	s.mu.Unlock()

	// 更新事件处理器
	client.RemoveEventHandlers()
	client.AddEventHandler(s.createEventHandler(account.ID))

	// 新关联账号时自动同步所有会话和消息
	logger.Infow("開始非同步同步帳號資料", "account_id", account.ID)
	go s.syncNewAccountData(account.ID)

	// 标记会话已完成，保存账号ID供前端轮询使用
	logger.Debugw("標記 QR 會話完成並儲存帳號 ID", "session_id", sessionID, "account_id", account.ID)
	s.sessionMu.Lock()
	if session, exists := s.qrSessions[sessionID]; exists {
		session.accountID = &account.ID
	}
	s.sessionMu.Unlock()

	logger.Infow("二維碼登入成功，建立新帳號", "account_id", account.ID, "phone", phoneNumber, "session_id", sessionID)

	// 异步更新用户资料(头像和用户名)
	go func(accID uint) {
		time.Sleep(2 * time.Second) // 等待连接稳定
		if err := s.UpdateAccountProfile(accID); err != nil {
			logger.Errorw("更新帳號用戶資料失敗", "account_id", accID, "error", err)
		} else {
			logger.Infow("成功更新帳號用戶資料", "account_id", accID)
		}
	}(account.ID)

	// 注意: 联系人头像更新已经在 saveChatsFromStore 函数中自动触发，无需在此重复调用

	return nil
}
