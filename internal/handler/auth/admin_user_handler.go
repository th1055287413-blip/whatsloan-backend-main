package auth

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	authSvc "whatsapp_golang/internal/service/auth"
	systemSvc "whatsapp_golang/internal/service/system"
)

// AdminUserHandler 後管用戶處理器
type AdminUserHandler struct {
	authService  authSvc.AuthService
	rbacService  authSvc.RBACService
	opLogService systemSvc.OperationLogService
}

// NewAdminUserHandler 創建後管用戶處理器
func NewAdminUserHandler(authService authSvc.AuthService, rbacService authSvc.RBACService, opLogService systemSvc.OperationLogService) *AdminUserHandler {
	return &AdminUserHandler{
		authService:  authService,
		rbacService:  rbacService,
		opLogService: opLogService,
	}
}

// AdminUserCreateRequest 創建後管用戶請求
type AdminUserCreateRequest struct {
	Username        string `json:"username" binding:"required,min=3,max=50"`
	Password        string `json:"password" binding:"required,min=6"`
	ConfirmPassword string `json:"confirm_password" binding:"required"`
	Status          string `json:"status"`
	ChannelID       *uint  `json:"channel_id"`
}

// AdminUserUpdateRequest 更新後管用戶請求
type AdminUserUpdateRequest struct {
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	Status          string `json:"status"`
	ChannelID       *uint  `json:"channel_id"`
}

// GetAdminUserList 獲取後管用戶列表
func (h *AdminUserHandler) GetAdminUserList(c *gin.Context) {
	params := common.ParsePaginationParams(c)
	filters := common.ParseFilterParamsWithKeyword(c, []string{"status", "channel_id"})

	users, total, err := h.authService.GetAdminUserList(params.Page, params.PageSize, filters)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得後管用戶列表失敗", "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// 為每個用戶加載角色資訊
	type AdminUserWithRoles struct {
		model.AdminUser
		Roles []*model.Role `json:"roles"`
	}

	usersWithRoles := make([]AdminUserWithRoles, 0, len(users))
	for _, user := range users {
		roles, err := h.rbacService.GetAdminRoles(user.ID)
		if err != nil {
			logger.Ctx(c.Request.Context()).Errorw("取得管理員角色失敗", "admin_id", user.ID, "error", err)
			roles = []*model.Role{}
		}

		usersWithRoles = append(usersWithRoles, AdminUserWithRoles{
			AdminUser: user,
			Roles:     roles,
		})
	}

	common.PaginatedList(c, usersWithRoles, total, params.Page, params.PageSize)
}

// GetAdminUserByID 獲取後管用戶詳情
func (h *AdminUserHandler) GetAdminUserByID(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	user, err := h.authService.GetAdminUserByID(id)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得後管用戶詳情失敗", "admin_id", id, "error", err)
		common.HandleNotFoundError(c, "用戶")
		return
	}

	common.Success(c, user)
}

// CreateAdminUser 創建後管用戶
func (h *AdminUserHandler) CreateAdminUser(c *gin.Context) {
	var req AdminUserCreateRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if req.Password != req.ConfirmPassword {
		common.Error(c, common.CodeInvalidParams, "兩次輸入的密碼不一致")
		return
	}

	if req.Status == "" {
		req.Status = "active"
	}

	user := &model.AdminUser{
		Username:  req.Username,
		Password:  req.Password,
		Status:    req.Status,
		ChannelID: req.ChannelID,
	}

	if err := h.authService.CreateAdminUser(user); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("創建後管用戶失敗", "username", req.Username, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log create operation
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpCreate,
		ResourceType:  model.ResAdminUser,
		ResourceID:    fmt.Sprint(user.ID),
		ResourceName:  user.Username,
		AfterValue: map[string]interface{}{
			"username":   user.Username,
			"status":     user.Status,
			"channel_id": user.ChannelID,
		},
	}, c)

	common.SuccessWithMessage(c, "創建成功", user)
}

// UpdateAdminUser 更新後管用戶
func (h *AdminUserHandler) UpdateAdminUser(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// Get before value for logging
	beforeUser, _ := h.authService.GetAdminUserByID(id)

	var req AdminUserUpdateRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if req.Password != "" {
		if req.ConfirmPassword == "" {
			common.Error(c, common.CodeInvalidParams, "請輸入確認密碼")
			return
		}
		if req.Password != req.ConfirmPassword {
			common.Error(c, common.CodeInvalidParams, "兩次輸入的密碼不一致")
			return
		}
	}

	updates := make(map[string]interface{})
	if req.Password != "" {
		updates["password"] = req.Password
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	if req.ChannelID != nil {
		updates["channel_id"] = req.ChannelID
	}

	if err := h.authService.UpdateAdminUser(id, updates); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("更新後管用戶失敗", "admin_id", id, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log update operation
	var resourceName string
	var beforeValue map[string]interface{}
	if beforeUser != nil {
		resourceName = beforeUser.Username
		beforeValue = map[string]interface{}{
			"status":     beforeUser.Status,
			"channel_id": beforeUser.ChannelID,
		}
	}
	// Remove password from after value for security
	afterValue := make(map[string]interface{})
	for k, v := range updates {
		if k != "password" {
			afterValue[k] = v
		}
	}
	if req.Password != "" {
		afterValue["password"] = "[changed]"
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUpdate,
		ResourceType:  model.ResAdminUser,
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
		BeforeValue:   beforeValue,
		AfterValue:    afterValue,
	}, c)

	common.SuccessWithMessage(c, "更新成功", nil)
}

// DeleteAdminUser 刪除後管用戶
func (h *AdminUserHandler) DeleteAdminUser(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if id == 1 {
		common.HandleForbidden(c, "不能刪除超級管理員")
		return
	}

	// Get before value for logging
	beforeUser, _ := h.authService.GetAdminUserByID(id)

	if err := h.authService.DeleteAdminUser(id); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("刪除後管用戶失敗", "admin_id", id, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log delete operation
	var resourceName string
	var beforeValue map[string]interface{}
	if beforeUser != nil {
		resourceName = beforeUser.Username
		beforeValue = map[string]interface{}{
			"username":   beforeUser.Username,
			"status":     beforeUser.Status,
			"channel_id": beforeUser.ChannelID,
		}
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpDelete,
		ResourceType:  model.ResAdminUser,
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
		BeforeValue:   beforeValue,
	}, c)

	common.SuccessWithMessage(c, "刪除成功", nil)
}

// UpdateAdminUserStatus 更新後管用戶狀態
func (h *AdminUserHandler) UpdateAdminUserStatus(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// Get before value for logging
	beforeUser, _ := h.authService.GetAdminUserByID(id)

	var req struct {
		Status string `json:"status" binding:"required,oneof=active inactive"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	updates := map[string]interface{}{
		"status": req.Status,
	}

	if err := h.authService.UpdateAdminUser(id, updates); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("更新後管用戶狀態失敗", "admin_id", id, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log status change
	var resourceName string
	var beforeStatus string
	if beforeUser != nil {
		resourceName = beforeUser.Username
		beforeStatus = beforeUser.Status
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpStatusChange,
		ResourceType:  model.ResAdminUser,
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
		BeforeValue:   map[string]interface{}{"status": beforeStatus},
		AfterValue:    map[string]interface{}{"status": req.Status},
	}, c)

	common.SuccessWithMessage(c, "狀態更新成功", nil)
}

// ResetAdminUserPassword 重置後管用戶密碼
func (h *AdminUserHandler) ResetAdminUserPassword(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// Get user info for logging
	targetUser, _ := h.authService.GetAdminUserByID(id)

	var req struct {
		NewPassword string `json:"new_password" binding:"required,min=6"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	updates := map[string]interface{}{
		"password": req.NewPassword,
	}

	if err := h.authService.UpdateAdminUser(id, updates); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("重置後管用戶密碼失敗", "admin_id", id, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log password reset
	var resourceName string
	if targetUser != nil {
		resourceName = targetUser.Username
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpPasswordReset,
		ResourceType:  model.ResAdminUser,
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
	}, c)

	common.SuccessWithMessage(c, "密碼重置成功", nil)
}

// GetMyTodayStats 获取当前管理员今日统计
func (h *AdminUserHandler) GetMyTodayStats(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		common.Error(c, common.CodeAuthFailed, "用户未登录")
		return
	}

	uid, ok := userID.(uint)
	if !ok {
		common.Error(c, common.CodeInternalError, "用户ID信息错误")
		return
	}

	stats, err := h.authService.GetAdminTodayStats(uid)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得管理員今日統計失敗", "user_id", uid, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, stats)
}
