package auth

import (
	"strings"

	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	authSvc "whatsapp_golang/internal/service/auth"
	systemSvc "whatsapp_golang/internal/service/system"
)

// AuthHandler 認證處理器
type AuthHandler struct {
	authService  authSvc.AuthService
	rbacService  authSvc.RBACService
	opLogService systemSvc.OperationLogService
}

// NewAuthHandler 創建認證處理器
func NewAuthHandler(authService authSvc.AuthService, rbacService authSvc.RBACService, opLogService systemSvc.OperationLogService) *AuthHandler {
	return &AuthHandler{
		authService:  authService,
		rbacService:  rbacService,
		opLogService: opLogService,
	}
}

// LoginRequest 登入請求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse 登入響應
type LoginResponse struct {
	Token string           `json:"token"`
	User  *UserProfileData `json:"user"`
}

// UserProfileData 用戶資料數據
type UserProfileData struct {
	ID          uint     `json:"id"`
	Username    string   `json:"username"`
	RealName    string   `json:"real_name"`
	Phone       string   `json:"phone"`
	Avatar      string   `json:"avatar"`
	Role        string   `json:"role"`
	Status      string   `json:"status"`
	Permissions []string `json:"permissions"`
	CreatedAt   string   `json:"created_at"`
}

// ChangePasswordRequest 修改密碼請求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// Login 用戶登入
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	clientIP := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")

	user, token, err := h.authService.Login(req.Username, req.Password, clientIP, userAgent)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("用戶登入失敗", "username", req.Username, "error", err)
		// Log failed login attempt
		h.opLogService.LogAsync(&model.LogEntry{
			OperationType:    model.OpLoginFailed,
			OperatorUsername: req.Username,
			ResourceType:     model.ResSession,
			ResourceName:     req.Username,
			Status:           model.StatusFailed,
			ErrorMessage:     err.Error(),
		}, c)
		common.Error(c, common.CodeAuthFailed, err.Error())
		return
	}

	roles, err := h.rbacService.GetAdminRoles(user.ID)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得管理員角色失敗", "admin_id", user.ID, "error", err)
		roles = []*model.Role{}
	}

	if len(roles) > 0 {
		user.Role = roles[0].Name
	}

	permissions, err := h.rbacService.GetAdminPermissions(user.ID)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得管理員權限失敗", "admin_id", user.ID, "error", err)
		permissions = []*model.Permission{}
	}

	permCodes := make([]string, 0, len(permissions))
	for _, perm := range permissions {
		permCodes = append(permCodes, perm.Code)
	}

	response := LoginResponse{
		Token: token,
		User: &UserProfileData{
			ID:          user.ID,
			Username:    user.Username,
			RealName:    user.RealName,
			Phone:       user.Phone,
			Avatar:      user.Avatar,
			Role:        user.Role,
			Status:      user.Status,
			Permissions: permCodes,
			CreatedAt:   user.CreatedAt.Format("2006-01-02 15:04:05"),
		},
	}

	// Log successful login
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType:    model.OpLogin,
		OperatorID:       &user.ID,
		OperatorUsername: user.Username,
		ResourceType:     model.ResSession,
		ResourceName:     user.Username,
	}, c)

	common.SuccessWithMessage(c, "登入成功", response)
}

// GetProfile 獲取用戶資料
func (h *AuthHandler) GetProfile(c *gin.Context) {
	userInterface, exists := c.Get("user")
	if !exists {
		common.HandleUnauthorized(c, "用戶未登入")
		return
	}

	user, ok := userInterface.(*model.AdminUser)
	if !ok {
		common.Error(c, common.CodeInternalError, "用戶資訊格式錯誤")
		return
	}

	roles, err := h.rbacService.GetAdminRoles(user.ID)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得管理員角色失敗", "admin_id", user.ID, "error", err)
		roles = []*model.Role{}
	}

	if len(roles) > 0 {
		user.Role = roles[0].Name
	}

	permissions, err := h.rbacService.GetAdminPermissions(user.ID)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得管理員權限失敗", "admin_id", user.ID, "error", err)
		permissions = []*model.Permission{}
	}

	permCodes := make([]string, 0, len(permissions))
	for _, perm := range permissions {
		permCodes = append(permCodes, perm.Code)
	}

	response := UserProfileData{
		ID:          user.ID,
		Username:    user.Username,
		RealName:    user.RealName,
		Phone:       user.Phone,
		Avatar:      user.Avatar,
		Role:        user.Role,
		Status:      user.Status,
		Permissions: permCodes,
		CreatedAt:   user.CreatedAt.Format("2006-01-02 15:04:05"),
	}

	common.Success(c, response)
}

// Logout 用戶登出
func (h *AuthHandler) Logout(c *gin.Context) {
	token := GetTokenFromHeader(c)
	if token == "" {
		common.Error(c, common.CodeAuthFailed, "令牌不存在")
		return
	}

	err := h.authService.Logout(token)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("用戶登出失敗", "error", err)
		common.Error(c, common.CodeInternalError, "登出失敗")
		return
	}

	// Log logout
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpLogout,
		ResourceType:  model.ResSession,
	}, c)

	common.SuccessWithMessage(c, "登出成功", nil)
}

// RefreshToken 刷新令牌
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	token := GetTokenFromHeader(c)
	if token == "" {
		common.Error(c, common.CodeAuthFailed, "令牌不存在")
		return
	}

	newToken, err := h.authService.RefreshToken(token)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("刷新令牌失敗", "error", err)
		common.Error(c, common.CodeAuthFailed, err.Error())
		return
	}

	common.SuccessWithMessage(c, "令牌刷新成功", gin.H{"token": newToken})
}

// ChangePassword 修改密碼
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	userInterface, exists := c.Get("user")
	if !exists {
		common.HandleUnauthorized(c, "用戶未登入")
		return
	}

	user, ok := userInterface.(*model.AdminUser)
	if !ok {
		common.Error(c, common.CodeInternalError, "用戶資訊格式錯誤")
		return
	}

	err := h.authService.ChangePassword(user.ID, req.OldPassword, req.NewPassword)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("修改密碼失敗", "user_id", user.ID, "error", err)
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	common.SuccessWithMessage(c, "密碼修改成功", nil)
}

// GetTokenFromHeader 從請求頭獲取 Token
func GetTokenFromHeader(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}

	return parts[1]
}
