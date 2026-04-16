package auth

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
	authSvc "whatsapp_golang/internal/service/auth"
	systemSvc "whatsapp_golang/internal/service/system"
)

// UserAssignmentHandler 用户分配处理器
type UserAssignmentHandler struct {
	rbacService  authSvc.RBACService
	authService  authSvc.AuthService
	opLogService systemSvc.OperationLogService
}

// NewUserAssignmentHandler 创建用户分配处理器
func NewUserAssignmentHandler(rbacService authSvc.RBACService, authService authSvc.AuthService, opLogService systemSvc.OperationLogService) *UserAssignmentHandler {
	return &UserAssignmentHandler{
		rbacService:  rbacService,
		authService:  authService,
		opLogService: opLogService,
	}
}

// AssignUsersRequest 分配用户请求
type AssignUsersRequest struct {
	Type           string `json:"type" binding:"required,oneof=explicit range"` // explicit 或 range
	UserIDs        []uint `json:"user_ids"`                                     // 显式分配
	UserRangeStart *uint  `json:"user_range_start"`                             // 范围分配起始
	UserRangeEnd   *uint  `json:"user_range_end"`                               // 范围分配结束
}

// GetAdminAssignedUsers 获取管理员已分配的用户
func (h *UserAssignmentHandler) GetAdminAssignedUsers(c *gin.Context) {
	adminIDStr := c.Param("id")
	adminID, err := strconv.ParseUint(adminIDStr, 10, 64)
	if err != nil {
		common.Error(c, common.CodeInvalidParams, "无效的管理员 ID")
		return
	}

	// 检查管理员是否存在
	admin, err := h.authService.GetAdminUserByID(uint(adminID))
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得管理員資訊失敗", "admin_id", adminID, "error", err)
		common.Error(c, common.CodeResourceNotFound, "管理员不存在")
		return
	}

	// 获取用户分配范围
	dataScope, err := h.rbacService.GetAdminUserAssignments(uint(adminID))
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得用戶分配範圍失敗", "admin_id", adminID, "error", err)
		common.Error(c, common.CodeInternalError, "获取分配信息失败")
		return
	}

	// 构建响应
	response := map[string]interface{}{
		"admin_id":   admin.ID,
		"admin_name": admin.Username,
		"type":       "",
	}

	if dataScope != nil && dataScope.Type == "assigned_users" {
		response["type"] = dataScope.Type
		if len(dataScope.AssignedUserIDs) > 0 {
			response["assigned_user_ids"] = dataScope.AssignedUserIDs
			response["total"] = len(dataScope.AssignedUserIDs)
		} else if dataScope.UserRangeStart != nil && dataScope.UserRangeEnd != nil {
			response["user_range_start"] = *dataScope.UserRangeStart
			response["user_range_end"] = *dataScope.UserRangeEnd
			response["total"] = *dataScope.UserRangeEnd - *dataScope.UserRangeStart + 1
		} else {
			response["total"] = 0
		}
	} else {
		response["total"] = 0
	}

	common.Success(c, response)
}

// AssignUsersToAdmin 为管理员分配用户
func (h *UserAssignmentHandler) AssignUsersToAdmin(c *gin.Context) {
	adminIDStr := c.Param("id")
	adminID, err := strconv.ParseUint(adminIDStr, 10, 64)
	if err != nil {
		common.Error(c, common.CodeInvalidParams, "无效的管理员 ID")
		return
	}

	var req AssignUsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.HandleBindError(c, err)
		return
	}

	// 检查管理员是否存在
	admin, err := h.authService.GetAdminUserByID(uint(adminID))
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得管理員資訊失敗", "admin_id", adminID, "error", err)
		common.Error(c, common.CodeResourceNotFound, "管理员不存在")
		return
	}

	// 根据分配类型执行不同逻辑
	var assignedCount int
	switch req.Type {
	case "explicit":
		if len(req.UserIDs) == 0 {
			common.Error(c, common.CodeInvalidParams, "请至少选择一个用户")
			return
		}
		if err := h.rbacService.AssignUsersToAdmin(uint(adminID), req.UserIDs); err != nil {
			logger.Ctx(c.Request.Context()).Errorw("分配用戶失敗", "admin_id", adminID, "error", err)
			common.Error(c, common.CodeInternalError, "分配用户失败")
			return
		}
		assignedCount = len(req.UserIDs)

	case "range":
		if req.UserRangeStart == nil || req.UserRangeEnd == nil {
			common.Error(c, common.CodeInvalidParams, "请提供有效的用户 ID 范围")
			return
		}
		if *req.UserRangeStart > *req.UserRangeEnd {
			common.Error(c, common.CodeInvalidParams, "起始 ID 不能大于结束 ID")
			return
		}
		if err := h.rbacService.AssignUserRangeToAdmin(uint(adminID), *req.UserRangeStart, *req.UserRangeEnd); err != nil {
			logger.Ctx(c.Request.Context()).Errorw("分配用戶範圍失敗", "admin_id", adminID, "error", err)
			common.Error(c, common.CodeInternalError, "分配用户失败")
			return
		}
		assignedCount = int(*req.UserRangeEnd - *req.UserRangeStart + 1)

	default:
		common.Error(c, common.CodeInvalidParams, "无效的分配类型")
		return
	}

	// 记录操作日志
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: "assign_users",
		ResourceType:  "admin_user",
		ResourceID:    adminIDStr,
		AfterValue: map[string]interface{}{
			"type":        req.Type,
			"user_ids":    req.UserIDs,
			"range_start": req.UserRangeStart,
			"range_end":   req.UserRangeEnd,
			"total":       assignedCount,
		},
	}, c)

	logger.Ctx(c.Request.Context()).Infow("用戶分配完成", "admin_id", adminID, "admin_name", admin.Username, "assigned_count", assignedCount)

	common.Success(c, map[string]interface{}{
		"message":        "用户分配成功",
		"assigned_count": assignedCount,
	})
}

// RemoveUserAssignments 移除用户分配
func (h *UserAssignmentHandler) RemoveUserAssignments(c *gin.Context) {
	adminIDStr := c.Param("id")
	adminID, err := strconv.ParseUint(adminIDStr, 10, 64)
	if err != nil {
		common.Error(c, common.CodeInvalidParams, "无效的管理员 ID")
		return
	}

	// 检查管理员是否存在
	admin, err := h.authService.GetAdminUserByID(uint(adminID))
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得管理員資訊失敗", "admin_id", adminID, "error", err)
		common.Error(c, common.CodeResourceNotFound, "管理员不存在")
		return
	}

	// 移除分配
	if err := h.rbacService.RemoveUserAssignments(uint(adminID)); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("移除用戶分配失敗", "admin_id", adminID, "error", err)
		common.Error(c, common.CodeInternalError, "移除分配失败")
		return
	}

	// 记录操作日志
	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: "remove_user_assignments",
		ResourceType:  "admin_user",
		ResourceID:    adminIDStr,
	}, c)

	logger.Ctx(c.Request.Context()).Infow("已移除用戶分配", "admin_id", adminID, "admin_name", admin.Username)

	common.Success(c, map[string]interface{}{
		"message": "用户分配已移除",
	})
}
