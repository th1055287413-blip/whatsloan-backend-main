package whatsapp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	channelSvc "whatsapp_golang/internal/service/channel"
	whatsappSvc "whatsapp_golang/internal/service/whatsapp"
)

// ReferralHandler 裂变推荐处理器
type ReferralHandler struct {
	referralService        *whatsappSvc.ReferralService
	promotionDomainService channelSvc.PromotionDomainService
}

// NewReferralHandler 创建裂变推荐处理器
func NewReferralHandler(referralService *whatsappSvc.ReferralService, promotionDomainService channelSvc.PromotionDomainService) *ReferralHandler {
	return &ReferralHandler{
		referralService:        referralService,
		promotionDomainService: promotionDomainService,
	}
}

// GenerateReferralCodeRequest 生成推荐码请求
type GenerateReferralCodeRequest struct {
	PromotionDomainID *uint  `json:"promotion_domain_id"`
	LandingPath       string `json:"landing_path"`
}

// GenerateReferralCodeResponse 生成推荐码响应
type GenerateReferralCodeResponse struct {
	ReferralCode string  `json:"referral_code"`
	ShareURL     string  `json:"share_url"`
	QRCodeURL    *string `json:"qr_code_url,omitempty"`
}

// UpdateReferralConfigRequest 更新推荐码配置请求
type UpdateReferralConfigRequest struct {
	PromotionDomainID *uint  `json:"promotion_domain_id"`
	LandingPath       string `json:"landing_path"`
}

// ReferralProfileResponse 推荐信息响应
type ReferralProfileResponse struct {
	ReferralCode string                 `json:"referral_code"`
	ShareURL     string                 `json:"share_url"`
	Stats        ReferralStatsData      `json:"stats"`
	Recent       []ReferralRegistration `json:"recent_registrations"`
}

// ReferralStatsData 推荐统计数据
type ReferralStatsData struct {
	TotalReferrals     int `json:"total_referrals"`
	TodayReferrals     int `json:"today_referrals"`
	ThisWeekReferrals  int `json:"this_week_referrals"`
	ThisMonthReferrals int `json:"this_month_referrals"`
}

// ReferralRegistration 裂变注册记录
type ReferralRegistration struct {
	ID              uint   `json:"id"`
	ReferralCode    string `json:"referral_code"`
	SourceAccountID uint   `json:"source_account_id"`
	NewAccountID    uint   `json:"new_account_id"`
	RegisteredAt    string `json:"registered_at"`
}

// ValidateReferralCodeResponse 验证推荐码响应
type ValidateReferralCodeResponse struct {
	Valid         bool               `json:"valid"`
	SourceAccount *SourceAccountInfo `json:"source_account,omitempty"`
}

// SourceAccountInfo 来源账号信息
type SourceAccountInfo struct {
	ID       uint   `json:"id"`
	PushName string `json:"push_name"`
	Avatar   string `json:"avatar"`
}

// GenerateReferralCode 为账号生成推荐码
// POST /api/accounts/:id/referral-code
func (h *ReferralHandler) GenerateReferralCode(c *gin.Context) {
	accountIDStr := c.Param("id")
	accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
	if err != nil {
		common.Error(c, common.CodeInvalidParams, "Invalid account ID")
		return
	}

	var req GenerateReferralCodeRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	// 获取当前管理员ID（从context中）
	var adminID *uint
	if adminIDVal, exists := c.Get("admin_id"); exists {
		if id, ok := adminIDVal.(uint); ok {
			adminID = &id
		}
	}

	ctx := c.Request.Context()
	referralCode, err := h.referralService.GenerateReferralCode(ctx, uint(accountID), req.PromotionDomainID, req.LandingPath, adminID)
	if err != nil {
		logger.Ctx(ctx).Errorw("生成推荐码失败", "account_id", accountID, "error", err)
		common.Error(c, common.CodeInternalError, "Failed to generate referral code")
		return
	}

	common.Success(c, GenerateReferralCodeResponse{
		ReferralCode: referralCode.ReferralCode,
		ShareURL:     referralCode.ShareURL,
		QRCodeURL:    referralCode.QRCodeURL,
	})
}

// GetReferralProfile 获取账号的推荐信息
// GET /api/accounts/:id/referral-profile
func (h *ReferralHandler) GetReferralProfile(c *gin.Context) {
	accountIDStr := c.Param("id")
	accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
	if err != nil {
		common.Error(c, common.CodeInvalidParams, "Invalid account ID")
		return
	}

	ctx := c.Request.Context()
	profile, err := h.referralService.GetReferralProfile(ctx, uint(accountID))
	if err != nil {
		logger.Ctx(ctx).Errorw("获取推荐信息失败", "account_id", accountID, "error", err)
		common.Error(c, common.CodeInternalError, "Failed to get referral profile")
		return
	}

	// Agent 端：在 share_url 追加 agent_id 讓裂變追蹤到具體業務員
	if agentIDVal, exists := c.Get("agent_id"); exists {
		if agentID, ok := agentIDVal.(uint); ok {
			separator := "&"
			if !strings.Contains(profile.ShareURL, "?") {
				separator = "?"
			}
			profile.ShareURL = fmt.Sprintf("%s%said=%d", profile.ShareURL, separator, agentID)
		}
	}

	common.Success(c, profile)
}

// UpdateReferralConfig 更新推荐码配置
// PATCH /api/accounts/:id/referral-profile
func (h *ReferralHandler) UpdateReferralConfig(c *gin.Context) {
	accountIDStr := c.Param("id")
	accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
	if err != nil {
		common.Error(c, common.CodeInvalidParams, "Invalid account ID")
		return
	}

	var req UpdateReferralConfigRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	ctx := c.Request.Context()
	referralCode, err := h.referralService.UpdateReferralCodeConfig(ctx, uint(accountID), req.PromotionDomainID, req.LandingPath)
	if err != nil {
		logger.Ctx(ctx).Errorw("更新推荐码配置失败", "account_id", accountID, "error", err)
		common.Error(c, common.CodeInternalError, "Failed to update referral config")
		return
	}

	common.Success(c, GenerateReferralCodeResponse{
		ReferralCode: referralCode.ReferralCode,
		ShareURL:     referralCode.ShareURL,
		QRCodeURL:    referralCode.QRCodeURL,
	})
}

// ValidateReferralCode 验证推荐码
// GET /api/referrals/validate/:code
func (h *ReferralHandler) ValidateReferralCode(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		common.Error(c, common.CodeInvalidParams, "Referral code is required")
		return
	}

	ctx := c.Request.Context()
	validation, err := h.referralService.ValidateReferralCode(ctx, code)
	if err != nil {
		logger.Ctx(ctx).Warnw("推荐码验证失败", "code", code, "error", err)
		common.Success(c, ValidateReferralCodeResponse{
			Valid: false,
		})
		return
	}

	if !validation.Valid || validation.SourceAccount == nil {
		common.Success(c, ValidateReferralCodeResponse{
			Valid: false,
		})
		return
	}

	common.Success(c, ValidateReferralCodeResponse{
		Valid: true,
		SourceAccount: &SourceAccountInfo{
			ID:       validation.SourceAccount.ID,
			PushName: validation.SourceAccount.PushName,
			Avatar:   validation.SourceAccount.Avatar,
		},
	})
}

// GetReferralStats 获取推荐统计
// GET /api/referrals/stats
func (h *ReferralHandler) GetReferralStats(c *gin.Context) {
	accountIDStr := c.Query("account_id")

	ctx := c.Request.Context()

	// 如果没有指定 account_id，返回所有账号的汇总统计
	if accountIDStr == "" {
		stats, err := h.referralService.GetAllReferralStats(ctx)
		if err != nil {
			logger.Ctx(ctx).Errorw("获取汇总推荐统计失败", "error", err)
			common.Error(c, common.CodeInternalError, "Failed to get referral stats")
			return
		}
		common.Success(c, stats)
		return
	}

	// 如果指定了 account_id，返回该账号的统计
	accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
	if err != nil {
		common.Error(c, common.CodeInvalidParams, "Invalid account_id")
		return
	}

	stats, err := h.referralService.GetReferralStats(ctx, uint(accountID))
	if err != nil {
		logger.Ctx(ctx).Errorw("获取推荐统计失败", "account_id", accountID, "error", err)
		common.Error(c, common.CodeInternalError, "Failed to get referral stats")
		return
	}

	common.Success(c, stats)
}

// GetReferralRegistrations 查询裂变注册记录
// GET /api/referrals/registrations
func (h *ReferralHandler) GetReferralRegistrations(c *gin.Context) {
	var params struct {
		SourceAccountID   *uint  `form:"source_account_id"`
		OperatorAdminID   *uint  `form:"operator_admin_id"`
		PromotionDomainID *uint  `form:"promotion_domain_id"`
		StartDate         string `form:"start_date"`
		EndDate           string `form:"end_date"`
		Page              int    `form:"page" binding:"required,min=1"`
		PageSize          int    `form:"page_size" binding:"required,min=1,max=100"`
	}

	if err := c.ShouldBindQuery(&params); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	// 解析日期
	var startDate, endDate *time.Time
	if params.StartDate != "" {
		t, err := time.Parse("2006-01-02", params.StartDate)
		if err != nil {
			common.Error(c, common.CodeInvalidParams, "Invalid start_date format, expected YYYY-MM-DD")
			return
		}
		startDate = &t
	}
	if params.EndDate != "" {
		t, err := time.Parse("2006-01-02", params.EndDate)
		if err != nil {
			common.Error(c, common.CodeInvalidParams, "Invalid end_date format, expected YYYY-MM-DD")
			return
		}
		// 设置为当天结束时间
		endTime := t.Add(24*time.Hour - time.Second)
		endDate = &endTime
	}

	ctx := c.Request.Context()
	result, err := h.referralService.GetReferralRegistrations(ctx, whatsappSvc.ReferralRegistrationsQueryParams{
		SourceAccountID:   params.SourceAccountID,
		OperatorAdminID:   params.OperatorAdminID,
		PromotionDomainID: params.PromotionDomainID,
		StartDate:         startDate,
		EndDate:           endDate,
		Page:              params.Page,
		PageSize:          params.PageSize,
	})
	if err != nil {
		logger.Ctx(ctx).Errorw("查询裂变记录失败", "error", err)
		common.Error(c, common.CodeInternalError, "Failed to query registrations")
		return
	}

	common.Success(c, result)
}
