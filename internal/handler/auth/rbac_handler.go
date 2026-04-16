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

// RBACHandler 權限管理處理器
type RBACHandler struct {
	rbacService  authSvc.RBACService
	opLogService systemSvc.OperationLogService
}

// NewRBACHandler 創建權限管理處理器
func NewRBACHandler(rbacService authSvc.RBACService, opLogService systemSvc.OperationLogService) *RBACHandler {
	return &RBACHandler{
		rbacService:  rbacService,
		opLogService: opLogService,
	}
}

// ========== 角色管理 ==========

// CreateRole 創建角色
func (h *RBACHandler) CreateRole(c *gin.Context) {
	var role model.Role
	if !common.BindAndValidate(c, &role) {
		return
	}

	if err := h.rbacService.CreateRole(&role); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("創建角色失敗", "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log create operation
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpCreate,
		ResourceType:  model.ResRole,
		ResourceID:    fmt.Sprint(role.ID),
		ResourceName:  role.Name,
		AfterValue: map[string]interface{}{
			"name":         role.Name,
			"display_name": role.DisplayName,
			"description":  role.Description,
		},
	}, c)

	common.SuccessWithMessage(c, "角色創建成功", role)
}

// UpdateRole 更新角色
func (h *RBACHandler) UpdateRole(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// Get before value
	beforeRole, _ := h.rbacService.GetRole(id)

	var updates map[string]interface{}
	if !common.BindAndValidate(c, &updates) {
		return
	}

	if err := h.rbacService.UpdateRole(id, updates); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("更新角色失敗", "role_id", id, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log update operation
	var resourceName string
	var beforeValue map[string]interface{}
	if beforeRole != nil {
		resourceName = beforeRole.Name
		beforeValue = map[string]interface{}{
			"name":         beforeRole.Name,
			"display_name": beforeRole.DisplayName,
			"description":  beforeRole.Description,
			"status":       beforeRole.Status,
		}
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUpdate,
		ResourceType:  model.ResRole,
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
		BeforeValue:   beforeValue,
		AfterValue:    updates,
	}, c)

	common.SuccessWithMessage(c, "角色更新成功", nil)
}

// DeleteRole 刪除角色
func (h *RBACHandler) DeleteRole(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// Get before value for logging
	beforeRole, _ := h.rbacService.GetRole(id)

	if err := h.rbacService.DeleteRole(id); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("刪除角色失敗", "role_id", id, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log delete operation
	var resourceName string
	var beforeValue map[string]interface{}
	if beforeRole != nil {
		resourceName = beforeRole.Name
		beforeValue = map[string]interface{}{
			"name":         beforeRole.Name,
			"display_name": beforeRole.DisplayName,
		}
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpDelete,
		ResourceType:  model.ResRole,
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
		BeforeValue:   beforeValue,
	}, c)

	common.SuccessWithMessage(c, "角色刪除成功", nil)
}

// GetRole 獲取角色詳情
func (h *RBACHandler) GetRole(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	role, err := h.rbacService.GetRole(id)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得角色失敗", "role_id", id, "error", err)
		common.HandleNotFoundError(c, "角色")
		return
	}

	common.Success(c, role)
}

// GetRoleList 獲取角色列表
func (h *RBACHandler) GetRoleList(c *gin.Context) {
	params := common.ParsePaginationParams(c)
	filters := common.ParseFilterParamsWithKeyword(c, []string{"status"})

	if isSystemStr := c.Query("is_system"); isSystemStr != "" {
		filters["is_system"] = isSystemStr == "true"
	}

	roles, total, err := h.rbacService.GetRoleList(params.Page, params.PageSize, filters)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得角色列表失敗", "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.PaginatedList(c, roles, total, params.Page, params.PageSize)
}

// GetRoleWithPermissions 獲取角色及其權限
func (h *RBACHandler) GetRoleWithPermissions(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	role, err := h.rbacService.GetRoleWithPermissions(id)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得角色權限失敗", "role_id", id, "error", err)
		common.HandleNotFoundError(c, "角色")
		return
	}

	common.Success(c, role)
}

// ========== 權限管理 ==========

// GetPermissionList 獲取權限列表
func (h *RBACHandler) GetPermissionList(c *gin.Context) {
	params := common.ParsePaginationParamsWithDefaults(c, common.PaginationParams{
		Page:     1,
		PageSize: 100,
	})
	filters := common.ParseFilterParamsWithKeyword(c, []string{"module", "resource"})

	permissions, total, err := h.rbacService.GetPermissionList(params.Page, params.PageSize, filters)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得權限列表失敗", "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.PaginatedList(c, permissions, total, params.Page, params.PageSize)
}

// GetAllPermissions 獲取所有權限
func (h *RBACHandler) GetAllPermissions(c *gin.Context) {
	permissions, err := h.rbacService.GetAllPermissions()
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得所有權限失敗", "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, permissions)
}

// GetPermissionsByModule 按模組獲取權限
func (h *RBACHandler) GetPermissionsByModule(c *gin.Context) {
	module := c.Param("module")
	if module == "" {
		common.Error(c, common.CodeInvalidParams, "模組名稱不能為空")
		return
	}

	permissions, err := h.rbacService.GetPermissionsByModule(module)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得模組權限失敗", "module", module, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, permissions)
}

// ========== 角色權限關聯 ==========

// AssignPermissionsToRole 為角色分配權限
func (h *RBACHandler) AssignPermissionsToRole(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	// Get role info and before permissions
	role, _ := h.rbacService.GetRole(id)
	beforePermissions, _ := h.rbacService.GetRolePermissions(id)

	var req struct {
		PermissionIDs []uint `json:"permission_ids" binding:"required"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.rbacService.AssignPermissionsToRole(id, req.PermissionIDs); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("分配權限失敗", "role_id", id, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log permission assignment
	var resourceName string
	var beforePermIDs []uint
	if role != nil {
		resourceName = role.Name
	}
	for _, p := range beforePermissions {
		beforePermIDs = append(beforePermIDs, p.ID)
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUpdate,
		ResourceType:  model.ResRole,
		ResourceID:    fmt.Sprint(id),
		ResourceName:  resourceName,
		BeforeValue:   map[string]interface{}{"permission_ids": beforePermIDs},
		AfterValue:    map[string]interface{}{"permission_ids": req.PermissionIDs},
	}, c)

	common.SuccessWithMessage(c, "權限分配成功", nil)
}

// GetRolePermissions 獲取角色的所有權限
func (h *RBACHandler) GetRolePermissions(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	permissions, err := h.rbacService.GetRolePermissions(id)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得角色權限失敗", "role_id", id, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, permissions)
}

// ========== 管理員角色關聯 ==========

// AssignRolesToAdmin 為管理員分配角色
func (h *RBACHandler) AssignRolesToAdmin(c *gin.Context) {
	adminID, ok := common.MustParseUintParam(c, "adminId")
	if !ok {
		return
	}

	// Get before roles
	beforeRoles, _ := h.rbacService.GetAdminRoles(adminID)

	var req struct {
		RoleIDs   []uint `json:"role_ids" binding:"required"`
		IsPrimary bool   `json:"is_primary"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.rbacService.AssignRolesToAdmin(adminID, req.RoleIDs, req.IsPrimary); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("分配角色失敗", "admin_id", adminID, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	// Log role assignment
	var beforeRoleIDs []uint
	for _, r := range beforeRoles {
		beforeRoleIDs = append(beforeRoleIDs, r.ID)
	}
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUpdate,
		ResourceType:  model.ResAdminUser,
		ResourceID:    fmt.Sprint(adminID),
		BeforeValue:   map[string]interface{}{"role_ids": beforeRoleIDs},
		AfterValue: map[string]interface{}{
			"role_ids":   req.RoleIDs,
			"is_primary": req.IsPrimary,
		},
	}, c)

	common.SuccessWithMessage(c, "角色分配成功", nil)
}

// GetAdminRoles 獲取管理員的所有角色
func (h *RBACHandler) GetAdminRoles(c *gin.Context) {
	adminID, ok := common.MustParseUintParam(c, "adminId")
	if !ok {
		return
	}

	roles, err := h.rbacService.GetAdminRoles(adminID)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得管理員角色失敗", "admin_id", adminID, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, roles)
}

// GetAdminPermissions 獲取管理員的所有權限
func (h *RBACHandler) GetAdminPermissions(c *gin.Context) {
	adminID, ok := common.MustParseUintParam(c, "adminId")
	if !ok {
		return
	}

	permissions, err := h.rbacService.GetAdminPermissions(adminID)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得管理員權限失敗", "admin_id", adminID, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, permissions)
}

// CheckAdminPermission 檢查管理員權限
func (h *RBACHandler) CheckAdminPermission(c *gin.Context) {
	adminID, ok := common.MustParseUintParam(c, "adminId")
	if !ok {
		return
	}

	resource := c.Query("resource")
	action := c.Query("action")

	if resource == "" || action == "" {
		common.Error(c, common.CodeInvalidParams, "resource 和 action 參數不能為空")
		return
	}

	hasPermission, err := h.rbacService.CheckAdminPermission(adminID, resource, action)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("檢查管理員權限失敗", "admin_id", adminID, "resource", resource, "action", action, "error", err)
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, gin.H{
		"has_permission": hasPermission,
		"resource":       resource,
		"action":         action,
	})
}
