package whatsapp

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"whatsapp_golang/internal/gateway"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/service/whatsapp"
)

// SessionHandler WhatsApp 會話處理器
type SessionHandler struct {
	dataService            whatsapp.DataService
	gateway                *gateway.Gateway
	referralService        *whatsapp.ReferralService
	referralSessionService whatsapp.ReferralSessionService
}

// NewSessionHandler 創建會話處理器
func NewSessionHandler(dataService whatsapp.DataService, gw *gateway.Gateway) *SessionHandler {
	return &SessionHandler{
		dataService: dataService,
		gateway:     gw,
	}
}

// SetReferralServices 設置推薦碼服務
func (h *SessionHandler) SetReferralServices(referralService *whatsapp.ReferralService, referralSessionService whatsapp.ReferralSessionService) {
	h.referralService = referralService
	h.referralSessionService = referralSessionService
}

// GetPairingCodeRequest 獲取配對代碼請求
type GetPairingCodeRequest struct {
	PhoneNumber  string `json:"phone_number" binding:"required"`
	ChannelCode  string `json:"channel_code"`
	ReferralCode string `json:"referral_code"` // 推荐码
	SourceKey    string `json:"source_key"`
	AgentID      *uint  `json:"agent_id"`
}

// GetPairingCodeResponse 獲取配對代碼響應
type GetPairingCodeResponse struct {
	PairingCode string `json:"pairing_code"`
	SessionID   string `json:"session_id"`
	Timeout     int    `json:"timeout"`
}

// VerifyPairingCodeRequest 驗證配對代碼請求
type VerifyPairingCodeRequest struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
	Code        string `json:"code" binding:"required"`
	SessionID   string `json:"session_id" binding:"required"`
}

// QRCodeResponse 獲取二維碼響應
type QRCodeResponse struct {
	SessionID string `json:"session_id"`
	QRCode    string `json:"qr_code"`
	Timeout   int    `json:"timeout"`
	CreatedAt string `json:"created_at"`
}

// LoginStatusResponse 登入狀態響應
type LoginStatusResponse struct {
	Connected   bool   `json:"connected"`
	State       string `json:"state,omitempty"`   // pending, connected, failed, cancelled
	QRCode      string `json:"qr_code,omitempty"` // Base64 QR Code
	PairingCode string `json:"pairing_code,omitempty"`
	JID         string `json:"jid,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	PushName    string `json:"push_name,omitempty"`
	Platform    string `json:"platform,omitempty"`
	LastSeen    string `json:"last_seen"`
	FailReason  string `json:"fail_reason,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	AvatarID    string `json:"avatar_id,omitempty"`
}

// VerifyQRCodeResponse 驗證二維碼響應
type VerifyQRCodeResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// GetPairingCode 獲取配對代碼
// 在 Connector 架構中，這會創建一個臨時帳號並發送配對碼請求
// 配對碼會通過事件回調返回
func (h *SessionHandler) GetPairingCode(c *gin.Context) {
	var req GetPairingCodeRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	// 優先使用 body，fallback 到 query parameter
	channelCode := strings.TrimSpace(req.ChannelCode)
	if channelCode == "" {
		channelCode = strings.TrimSpace(c.Query("channel_code"))
		if channelCode == "" {
			channelCode = strings.TrimSpace(c.Query("ad"))
		}
	}

	// 生成 session ID
	sessionID := uuid.New().String()

	ctx := context.Background()

	// 如果提供了推荐码，验证并保存到 Redis
	if req.ReferralCode != "" && h.referralService != nil && h.referralSessionService != nil {
		// 验证推荐码
		validation, err := h.referralService.ValidateReferralCode(ctx, req.ReferralCode)
		if err != nil {
			logger.Ctx(c.Request.Context()).Warnw("验证推荐码失败", "referral_code", req.ReferralCode, "error", err)
		} else if validation != nil && validation.Valid {
			// 保存推荐码信息到 Redis（包含 source_key）
			sessionInfo := &whatsapp.ReferralSessionInfo{
				ReferralCode:      req.ReferralCode,
				SourceAccountID:   validation.SourceAccountID,
				PromotionDomainID: validation.PromotionDomainID,
				SourceKey:         req.SourceKey,
				SourceAgentID:     req.AgentID,
			}
			if err := h.referralSessionService.StoreReferralSession(ctx, sessionID, sessionInfo); err != nil {
				logger.Ctx(c.Request.Context()).Warnw("保存推荐码会话失败", "session_id", sessionID, "error", err)
			} else {
				logger.Ctx(c.Request.Context()).Infow("推荐码会话已保存", "session_id", sessionID, "referral_code", req.ReferralCode, "source_key", req.SourceKey)
			}
		}
	} else if req.SourceKey != "" && h.referralSessionService != nil {
		// 只有来源代码，没有推荐码
		sessionInfo := &whatsapp.ReferralSessionInfo{
			SourceKey:     req.SourceKey,
			SourceAgentID: req.AgentID,
		}
		if err := h.referralSessionService.StoreReferralSession(ctx, sessionID, sessionInfo); err != nil {
			logger.Ctx(c.Request.Context()).Warnw("保存来源代码会话失败", "session_id", sessionID, "source_key", req.SourceKey, "error", err)
		} else {
			logger.Ctx(c.Request.Context()).Infow("来源代码会话已保存", "session_id", sessionID, "source_key", req.SourceKey)
		}
	}

	// 建立登入會話追蹤（帶渠道碼）
	if err := h.gateway.CreateLoginSession(ctx, sessionID, 0, channelCode); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("建立登入會話失敗", "session_id", sessionID, "error", err)
	}
	// 使用 accountID=0 表示新登入流程
	if err := h.gateway.RequestPairingCode(ctx, 0, sessionID, req.PhoneNumber); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("請求配對代碼失敗", "session_id", sessionID, "error", err)

		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "rate-overlimit") || strings.Contains(errStr, "429") || strings.Contains(errStr, "too many requests") {
			common.Error(c, common.CodeRateLimited, "WhatsApp API 速率限制，請稍後再試（建議等待 1-2 分鐘）")
			return
		}

		common.Error(c, common.CodeWhatsAppError, err.Error())
		return
	}

	// 等待配對碼生成（最多等待 15 秒）
	session, err := h.gateway.WaitForPairingCode(ctx, sessionID, 15*time.Second)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("等待配對碼失敗", "session_id", sessionID, "error", err)
		common.Error(c, common.CodeWhatsAppError, err.Error())
		return
	}

	// 檢查是否登入失敗
	if session.State == gateway.LoginStateFailed {
		common.Error(c, common.CodeWhatsAppError, session.FailReason)
		return
	}

	common.Success(c, GetPairingCodeResponse{
		PairingCode: session.PairingCode,
		SessionID:   sessionID,
		Timeout:     300,
	})
}

// VerifyPairingCode 驗證配對代碼
// 在 Connector 架構中，配對碼驗證是自動的，前端只需等待登入成功事件
func (h *SessionHandler) VerifyPairingCode(c *gin.Context) {
	var req VerifyPairingCodeRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	// 在 Connector 架構中，配對碼驗證是由用戶在手機上完成的
	// 這個 API 主要用於檢查登入狀態
	logger.Ctx(c.Request.Context()).Infow("配對碼驗證請求", "session_id", req.SessionID, "phone", req.PhoneNumber)

	// TODO: 可以檢查 session 狀態
	common.SuccessWithMessage(c, "請在手機上確認配對", nil)
}

// GetQRCode 獲取二維碼
// 在 Connector 架構中，這會創建一個臨時帳號並發送 QR Code 請求
// QR Code 會通過事件回調返回
func (h *SessionHandler) GetQRCode(c *gin.Context) {
	var body struct {
		ChannelCode string `json:"channel_code"`
	}
	_ = c.ShouldBindJSON(&body)
	channelCode := strings.TrimSpace(body.ChannelCode)
	if channelCode == "" {
		channelCode = strings.TrimSpace(c.Query("channel_code"))
		if channelCode == "" {
			channelCode = strings.TrimSpace(c.Query("ad"))
		}
	}

	// 生成 session ID
	sessionID := uuid.New().String()

	ctx := context.Background()
	// 建立登入會話追蹤（帶渠道碼）
	if err := h.gateway.CreateLoginSession(ctx, sessionID, 0, channelCode); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("建立登入會話失敗", "session_id", sessionID, "error", err)
	}
	// 使用 accountID=0 表示新登入流程
	if err := h.gateway.RequestQRCode(ctx, 0, sessionID); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("請求二維碼失敗", "session_id", sessionID, "error", err)
		common.Error(c, common.CodeWhatsAppError, err.Error())
		return
	}

	// 等待 QR Code 生成（最多等待 10 秒）
	session, err := h.gateway.WaitForQRCode(ctx, sessionID, 10*time.Second)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("等待 QR Code 失敗", "session_id", sessionID, "error", err)
		common.Error(c, common.CodeWhatsAppError, err.Error())
		return
	}

	// 檢查是否登入失敗
	if session.State == gateway.LoginStateFailed {
		common.Error(c, common.CodeWhatsAppError, session.FailReason)
		return
	}

	common.SuccessWithMessage(c, "QR Code 已生成", QRCodeResponse{
		SessionID: sessionID,
		QRCode:    session.QRCode,
		Timeout:   300,
		CreatedAt: time.Now().Format(time.RFC3339),
	})
}

// CheckLoginStatus 檢查登入狀態
func (h *SessionHandler) CheckLoginStatus(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		common.Error(c, common.CodeInvalidParams, "缺少 session_id 參數")
		return
	}

	ctx := context.Background()
	session, err := h.gateway.GetLoginSession(ctx, sessionID)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("查詢登入會話失敗", "session_id", sessionID, "error", err)
		common.Error(c, common.CodeInternalError, "查詢會話狀態失敗")
		return
	}

	if session == nil {
		common.Error(c, common.CodeSessionNotFound, "會話不存在或已過期")
		return
	}

	common.Success(c, LoginStatusResponse{
		Connected:   session.State == gateway.LoginStateConnected,
		QRCode:      session.QRCode,
		PairingCode: session.PairingCode,
		JID:         session.JID,
		PhoneNumber: session.PhoneNumber,
		State:       string(session.State),
		FailReason:  session.FailReason,
		LastSeen:    session.UpdatedAt.Format(time.RFC3339),
	})
}

// DisconnectSession 斷開會話連接
func (h *SessionHandler) DisconnectSession(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
	}

	if !common.BindAndValidate(c, &req) {
		return
	}

	ctx := context.Background()
	if err := h.gateway.CancelLogin(ctx, 0, req.SessionID); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("斷開會話連接失敗", "session_id", req.SessionID, "error", err)
		common.Error(c, common.CodeWhatsAppError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "連接已斷開", nil)
}

// RestoreSession 恢復會話連接
// TODO: 需要實現對應的 Gateway Command
func (h *SessionHandler) RestoreSession(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
	}

	if !common.BindAndValidate(c, &req) {
		return
	}

	// TODO: 透過 Gateway 發送恢復會話命令
	logger.Ctx(c.Request.Context()).Warnw("RestoreSession 尚未遷移到 Connector 架構", "session_id", req.SessionID)
	common.Error(c, common.CodeInternalError, "此功能暫時不可用，正在遷移到新架構")
}

// CleanupSessions 清理過期會話
// TODO: 需要實現對應的 Gateway Command
func (h *SessionHandler) CleanupSessions(c *gin.Context) {
	// TODO: 透過 Gateway 清理過期的登入會話
	logger.Ctx(c.Request.Context()).Warnw("CleanupSessions 尚未遷移到 Connector 架構")
	common.Error(c, common.CodeInternalError, "此功能暫時不可用，正在遷移到新架構")
}

// VerifyQRCode 驗證二維碼登入狀態
// 在 Connector 架構中，QR Code 驗證是自動的，前端只需等待登入成功事件
func (h *SessionHandler) VerifyQRCode(c *gin.Context) {
	// 在 Connector 架構中，QR Code 掃描後會自動觸發登入流程
	// 登入結果通過事件回調返回
	c.JSON(http.StatusOK, VerifyQRCodeResponse{
		Status:  "waiting",
		Message: "請掃描 QR Code 並等待登入結果",
	})
}
