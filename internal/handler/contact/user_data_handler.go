package contact

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	contactSvc "whatsapp_golang/internal/service/contact"
)

// normalizePhone 规范化手机号：去除空格和+号
func normalizePhone(phone string) string {
	normalized := strings.ReplaceAll(phone, " ", "")
	normalized = strings.ReplaceAll(normalized, "+", "")
	return normalized
}

// UserDataHandler 用戶資料處理器
type UserDataHandler struct {
	userDataService contactSvc.UserDataService
}

// NewUserDataHandler 創建用戶資料處理器
func NewUserDataHandler(userDataService contactSvc.UserDataService) *UserDataHandler {
	return &UserDataHandler{
		userDataService: userDataService,
	}
}

// UserDataLoginRequest 登入請求
type UserDataLoginRequest struct {
	Phone string `json:"phone" binding:"required"`
}

// UserDataLoginResponse 登入響應
type UserDataLoginResponse struct {
	Phone                string                `json:"phone"`
	IsNewUser            bool                  `json:"isNewUser"`
	WhatsAppAuthorized   bool                  `json:"whatsapp_authorized"`
	WhatsAppAuthorizedAt *string               `json:"whatsapp_authorized_at,omitempty"`
	BasicInfo            *model.BasicInfo      `json:"basicInfo"`
	HouseInfo            *model.HouseInfo      `json:"houseInfo"`
	CreditCardInfo       *model.CreditCardInfo `json:"creditCardInfo"`
	CarInfo              *model.CarInfo        `json:"carInfo"`
	BankInfo             *model.BankInfo       `json:"bankInfo"`
	ExtendedData         *model.ExtendedData   `json:"extendedData"`
}

// UpdateUserDataRequest 更新用戶資料請求
type UpdateUserDataRequest struct {
	BasicInfo      *model.BasicInfo      `json:"basicInfo"`
	HouseInfo      *model.HouseInfo      `json:"houseInfo"`
	CreditCardInfo *model.CreditCardInfo `json:"creditCardInfo"`
	CarInfo        *model.CarInfo        `json:"carInfo"`
	BankInfo       *model.BankInfo       `json:"bankInfo"`
	ExtendedData   *model.ExtendedData   `json:"extendedData"`
}

// Login 用戶登入
func (h *UserDataHandler) Login(c *gin.Context) {
	var req UserDataLoginRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	userData, isNewUser, err := h.userDataService.LoginOrRegister(req.Phone)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("用戶登入失敗", "phone", req.Phone, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	authStatus, err := h.userDataService.CheckWhatsAppAuthorization(req.Phone)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("查詢 WhatsApp 授權狀態失敗", "phone", req.Phone, "error", err)
		authStatus = &contactSvc.WhatsAppAuthStatus{Authorized: false}
	}

	response := UserDataLoginResponse{
		Phone:              userData.Phone,
		IsNewUser:          isNewUser,
		WhatsAppAuthorized: authStatus.Authorized,
		BasicInfo:          userData.BasicInfo,
		HouseInfo:          userData.HouseInfo,
		CreditCardInfo:     userData.CreditCardInfo,
		CarInfo:            userData.CarInfo,
		BankInfo:           userData.BankInfo,
		ExtendedData:       userData.ExtendedData,
	}

	if authStatus.AuthorizedAt != nil {
		authorizedAtStr := authStatus.AuthorizedAt.Format("2006-01-02T15:04:05Z07:00")
		response.WhatsAppAuthorizedAt = &authorizedAtStr
	}

	common.Success(c, response)
}

// UpdateUser 更新用戶資料
func (h *UserDataHandler) UpdateUser(c *gin.Context) {
	phone := c.Param("phone")
	if phone == "" {
		common.Error(c, common.CodeInvalidParams, "手機號不能為空")
		return
	}

	var req UpdateUserDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("解析請求參數失敗", "phone", phone, "error", err)
		common.Error(c, common.CodeInvalidParams, fmt.Sprintf("請求參數錯誤: %v", err))
		return
	}

	updateData := &model.UserData{
		Phone:          phone,
		BasicInfo:      req.BasicInfo,
		HouseInfo:      req.HouseInfo,
		CreditCardInfo: req.CreditCardInfo,
		CarInfo:        req.CarInfo,
		BankInfo:       req.BankInfo,
		ExtendedData:   req.ExtendedData,
	}

	if err := h.userDataService.UpdateUserData(phone, updateData); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("更新用戶資料失敗", "phone", phone, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "資料更新成功", map[string]interface{}{
		"phone":     phone,
		"updatedAt": nil,
	})
}

// SaveShopOrder 保存商城訂單
func (h *UserDataHandler) SaveShopOrder(c *gin.Context) {
	var req struct {
		Phone     string               `json:"phone" binding:"required"`
		OrderData *model.ShopOrderData `json:"orderData" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("解析請求參數失敗", "error", err)
		common.Error(c, common.CodeInvalidParams, fmt.Sprintf("請求參數錯誤: %v", err))
		return
	}

	if err := h.userDataService.SaveShopOrder(req.Phone, req.OrderData); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("儲存訂單失敗", "phone", req.Phone, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 獲取更新後的用戶數據
	userData, err := h.userDataService.GetUserData(req.Phone)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得用戶資料失敗", "phone", req.Phone, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, userData)
}

// GetUserData 獲取用戶數據
func (h *UserDataHandler) GetUserData(c *gin.Context) {
	phone := c.Query("phone")
	if phone == "" {
		common.Error(c, common.CodeInvalidParams, "手機號不能為空")
		return
	}

	userData, err := h.userDataService.GetUserData(phone)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得用戶資料失敗", "phone", phone, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, userData)
}

// GetShopOrder 獲取特定訂單
func (h *UserDataHandler) GetShopOrder(c *gin.Context) {
	phone := c.Query("phone")
	orderID := c.Query("orderId")

	if phone == "" || orderID == "" {
		common.Error(c, common.CodeInvalidParams, "手機號和訂單號不能為空")
		return
	}

	order, err := h.userDataService.GetShopOrder(phone, orderID)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得訂單失敗", "phone", phone, "order_id", orderID, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, map[string]interface{}{
		"found": true,
		"order": order,
	})
}
