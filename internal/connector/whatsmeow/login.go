package whatsmeow

import (
	"context"
	"fmt"
	"time"

	"whatsapp_golang/internal/protocol"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// loginSession tracks a login session
type loginSession struct {
	client    *whatsmeow.Client
	sessionID string
	accountID uint
	cancel    context.CancelFunc
	createdAt time.Time
}

// HandleGetQRCode handles the get QR code command
func (m *Manager) HandleGetQRCode(ctx context.Context, cmd *protocol.Command, payload *protocol.GetQRCodePayload) error {
	// 清理該帳號的舊 device（避免 app state 殘留導致 LTHash 不一致）
	m.cleanupOldDevice(ctx, cmd.AccountID)

	// Create new device for new login
	device := m.container.NewDevice()
	client := whatsmeow.NewClient(device, waLog.Stdout("WhatsApp", "INFO", true))

	// Create cancellable context
	sessionCtx, cancel := context.WithCancel(context.Background())

	// Track login session
	m.loginMu.Lock()
	m.loginSessions[payload.SessionID] = &loginSession{
		client:    client,
		sessionID: payload.SessionID,
		accountID: cmd.AccountID,
		cancel:    cancel,
		createdAt: time.Now(),
	}
	m.loginMu.Unlock()

	// Get QR channel
	qrChan, _ := client.GetQRChannel(sessionCtx)

	// Register event handler for login success
	client.AddEventHandler(func(evt interface{}) {
		if _, ok := evt.(*events.PairSuccess); ok {
			m.log.Infow("QR Code 配對成功", "session_id", payload.SessionID)

			// Note: Don't delete loginSessions here, let HandleBindAccount handle it
			// because bind_account command needs to find the client from loginSessions

			// Save JID mapping
			jid := client.Store.ID
			if jid != nil {
				m.redis.HSet(sessionCtx, "wa:account:jid", fmt.Sprintf("%d", cmd.AccountID), jid.String())

				// Save client + start event worker
				m.mu.Lock()
				m.clients[cmd.AccountID] = client
				m.accountInfo[cmd.AccountID] = &AccountInfo{
					AccountID:   cmd.AccountID,
					JID:         *jid,
					PhoneNumber: jid.User,
					Connected:   true,
					LastSeen:    time.Now(),
				}
				m.startEventWorker(cmd.AccountID)
				m.mu.Unlock()

				// Publish LoginSuccess event
				m.publisher.PublishLoginSuccess(sessionCtx, cmd.AccountID, &protocol.LoginSuccessPayload{
					SessionID:    payload.SessionID,
					JID:          jid.String(),
					PhoneNumber:  jid.User,
					Platform:     client.Store.Platform,
					BusinessName: client.Store.BusinessName,
				})

				// Publish Connected event
				m.publisher.PublishConnected(sessionCtx, cmd.AccountID, &protocol.ConnectedPayload{
					PhoneNumber:  jid.User,
					Platform:     client.Store.Platform,
					BusinessName: client.Store.BusinessName,
				})
			}
		}
	})

	// Connect (will trigger QR generation)
	if err := client.Connect(); err != nil {
		// Cleanup login session
		m.loginMu.Lock()
		delete(m.loginSessions, payload.SessionID)
		m.loginMu.Unlock()
		cancel()

		// Publish LoginFailed event
		m.publisher.PublishLoginFailed(ctx, cmd.AccountID, &protocol.LoginFailedPayload{
			SessionID: payload.SessionID,
			Reason:    fmt.Sprintf("連接失敗: %v", err),
		})

		return fmt.Errorf("連接失敗: %w", err)
	}

	// Start goroutine to handle QR events
	go func() {
		for evt := range qrChan {
			select {
			case <-sessionCtx.Done():
				return
			default:
			}

			if evt.Event == "code" {
				// Publish QR Code event
				if err := m.publisher.PublishQRCode(sessionCtx, cmd.AccountID, &protocol.QRCodePayload{
					SessionID: payload.SessionID,
					QRCode:    evt.Code,
					ExpiresAt: time.Now().Add(60 * time.Second).UnixMilli(),
				}); err != nil {
					m.log.Warnw("發送 QRCode 事件失敗", "error", err)
				}
			} else if evt.Event == "timeout" {
				// QR Code timeout
				m.loginMu.Lock()
				delete(m.loginSessions, payload.SessionID)
				m.loginMu.Unlock()

				m.publisher.PublishLoginFailed(sessionCtx, cmd.AccountID, &protocol.LoginFailedPayload{
					SessionID: payload.SessionID,
					Reason:    "QR Code 已過期",
					Code:      "timeout",
				})
			}
		}
	}()

	return nil
}

// HandleGetPairingCode handles the get pairing code command
func (m *Manager) HandleGetPairingCode(ctx context.Context, cmd *protocol.Command, payload *protocol.GetPairingCodePayload) error {
	m.log.Infow("[PairingCode] 開始處理", "session_id", payload.SessionID, "phone", payload.PhoneNumber)

	// 清理該帳號的舊 device（避免 app state 殘留導致 LTHash 不一致）
	m.cleanupOldDevice(ctx, cmd.AccountID)

	// Create new device for new login
	device := m.container.NewDevice()
	client := whatsmeow.NewClient(device, waLog.Stdout("WhatsApp", "INFO", true))

	// Create cancellable context
	sessionCtx, cancel := context.WithCancel(context.Background())

	// Track login session
	m.loginMu.Lock()
	m.loginSessions[payload.SessionID] = &loginSession{
		client:    client,
		sessionID: payload.SessionID,
		accountID: cmd.AccountID,
		cancel:    cancel,
		createdAt: time.Now(),
	}
	m.loginMu.Unlock()

	// Register event handler
	client.AddEventHandler(func(evt interface{}) {
		if _, ok := evt.(*events.PairSuccess); ok {
			m.log.Infow("配對碼配對成功", "session_id", payload.SessionID)

			// Note: Don't delete loginSessions here, let HandleBindAccount handle it
			// because bind_account command needs to find the client from loginSessions

			// Save JID mapping
			jid := client.Store.ID
			if jid != nil {
				m.redis.HSet(sessionCtx, "wa:account:jid", fmt.Sprintf("%d", cmd.AccountID), jid.String())

				// Save client + start event worker
				m.mu.Lock()
				m.clients[cmd.AccountID] = client
				m.accountInfo[cmd.AccountID] = &AccountInfo{
					AccountID:   cmd.AccountID,
					JID:         *jid,
					PhoneNumber: jid.User,
					Connected:   true,
					LastSeen:    time.Now(),
				}
				m.startEventWorker(cmd.AccountID)
				m.mu.Unlock()

				// Publish LoginSuccess event
				m.publisher.PublishLoginSuccess(sessionCtx, cmd.AccountID, &protocol.LoginSuccessPayload{
					SessionID:    payload.SessionID,
					JID:          jid.String(),
					PhoneNumber:  jid.User,
					Platform:     client.Store.Platform,
					BusinessName: client.Store.BusinessName,
				})

				// Publish Connected event
				m.publisher.PublishConnected(sessionCtx, cmd.AccountID, &protocol.ConnectedPayload{
					PhoneNumber:  jid.User,
					Platform:     client.Store.Platform,
					BusinessName: client.Store.BusinessName,
				})
			}
		}
	})

	// Connect
	m.log.Infow("[PairingCode] 開始連接 WhatsApp", "session_id", payload.SessionID)
	if err := client.Connect(); err != nil {
		// Cleanup login session
		m.loginMu.Lock()
		delete(m.loginSessions, payload.SessionID)
		m.loginMu.Unlock()
		cancel()

		// Publish LoginFailed event
		m.publisher.PublishLoginFailed(ctx, cmd.AccountID, &protocol.LoginFailedPayload{
			SessionID: payload.SessionID,
			Reason:    fmt.Sprintf("連接失敗: %v", err),
		})

		return fmt.Errorf("連接失敗: %w", err)
	}
	m.log.Infow("[PairingCode] 連接成功，開始取得配對碼", "session_id", payload.SessionID)

	// Get pairing code
	code, err := client.PairPhone(ctx, payload.PhoneNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		// Cleanup login session
		m.loginMu.Lock()
		delete(m.loginSessions, payload.SessionID)
		m.loginMu.Unlock()
		cancel()

		// Publish LoginFailed event
		m.publisher.PublishLoginFailed(ctx, cmd.AccountID, &protocol.LoginFailedPayload{
			SessionID: payload.SessionID,
			Reason:    fmt.Sprintf("取得配對碼失敗: %v", err),
		})

		return fmt.Errorf("取得配對碼失敗: %w", err)
	}
	m.log.Infow("[PairingCode] 取得配對碼成功", "session_id", payload.SessionID, "code", code)

	// Publish pairing code event
	if err := m.publisher.PublishPairingCode(ctx, cmd.AccountID, &protocol.PairingCodePayload{
		SessionID:   payload.SessionID,
		PairingCode: code,
		ExpiresAt:   time.Now().Add(60 * time.Second).UnixMilli(),
	}); err != nil {
		m.log.Warnw("發送 PairingCode 事件失敗", "error", err)
	} else {
		m.log.Infow("[PairingCode] 事件發送成功", "session_id", payload.SessionID)
	}

	return nil
}

// HandleCancelLogin handles the cancel login command
func (m *Manager) HandleCancelLogin(ctx context.Context, cmd *protocol.Command, payload *protocol.CancelLoginPayload) error {
	m.loginMu.Lock()
	session, exists := m.loginSessions[payload.SessionID]
	if exists {
		delete(m.loginSessions, payload.SessionID)
	}
	m.loginMu.Unlock()

	if !exists {
		m.log.Warnw("登入會話不存在", "session_id", payload.SessionID)
		return nil
	}

	// Cancel session
	if session.cancel != nil {
		session.cancel()
	}

	// Disconnect client
	if session.client != nil {
		session.client.Disconnect()
	}

	// Publish login cancelled event
	if err := m.publisher.PublishLoginCancelled(ctx, cmd.AccountID, &protocol.LoginCancelledPayload{
		SessionID: payload.SessionID,
	}); err != nil {
		m.log.Warnw("發送 LoginCancelled 事件失敗", "error", err)
	}

	m.log.Infow("登入會話已取消", "session_id", payload.SessionID)
	return nil
}

// HandleBindAccount handles the bind account command (after login success, binds client with accountID=0 to actual account ID)
func (m *Manager) HandleBindAccount(ctx context.Context, cmd *protocol.Command, payload *protocol.BindAccountPayload) error {
	m.log.Infow("綁定帳號", "session_id", payload.SessionID, "new_account_id", payload.NewAccountID)

	// Find client from loginSessions (supports concurrent multi-account login)
	m.loginMu.Lock()
	session, exists := m.loginSessions[payload.SessionID]
	if exists {
		delete(m.loginSessions, payload.SessionID)
	}
	m.loginMu.Unlock()

	if !exists || session.client == nil {
		m.log.Warnw("找不到登入會話", "session_id", payload.SessionID)
		return nil
	}

	client := session.client

	// Get JID
	jid := client.Store.ID
	if jid == nil {
		m.log.Warnw("client 沒有 JID 資訊")
		return nil
	}

	// 停止舊 accountID=0 的 event worker（在鎖外）
	if session.accountID == 0 {
		m.stopEventWorker(0)
	}

	m.mu.Lock()

	// Cleanup old accountID=0 record if exists
	if session.accountID == 0 {
		delete(m.clients, 0)
		delete(m.accountInfo, 0)
		// Cleanup old JID mapping
		m.redis.HDel(ctx, "wa:account:jid", "0")
	}

	// Update JID mapping to new accountID
	m.redis.HSet(ctx, "wa:account:jid", fmt.Sprintf("%d", payload.NewAccountID), jid.String())

	// Save client to new accountID
	m.clients[payload.NewAccountID] = client
	m.accountInfo[payload.NewAccountID] = &AccountInfo{
		AccountID:   payload.NewAccountID,
		JID:         *jid,
		PhoneNumber: jid.User,
		Connected:   client.IsConnected(),
		LastSeen:    time.Now(),
	}

	// 啟動新 accountID 的 event worker + 重新註冊 event handler
	m.startEventWorker(payload.NewAccountID)
	client.RemoveEventHandlers()
	client.AddEventHandler(m.createEventHandler(payload.NewAccountID))

	m.mu.Unlock()

	m.log.Infow("帳號綁定成功", "session_id", payload.SessionID, "account_id", payload.NewAccountID, "jid", jid.String())

	// Publish Connected event with correct accountID to trigger sync on API side
	if client.IsConnected() {
		m.publisher.PublishConnected(ctx, payload.NewAccountID, &protocol.ConnectedPayload{
			PhoneNumber:  jid.User,
			Platform:     client.Store.Platform,
			BusinessName: client.Store.BusinessName,
			DeviceID:     jid.String(),
		})
		m.log.Infow("已發送 Connected 事件", "account_id", payload.NewAccountID, "device_id", jid.String())
	}

	return nil
}
