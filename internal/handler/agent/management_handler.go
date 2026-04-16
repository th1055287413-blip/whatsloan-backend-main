package agent

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	agentSvc "whatsapp_golang/internal/service/agent"
	systemSvc "whatsapp_golang/internal/service/system"
)

// ManagementHandler Admin Agent 管理處理器
type ManagementHandler struct {
	svc          agentSvc.AgentManagementService
	opLogService systemSvc.OperationLogService
}

// NewManagementHandler 建立 Admin Agent 管理處理器
func NewManagementHandler(svc agentSvc.AgentManagementService, opLogService systemSvc.OperationLogService) *ManagementHandler {
	return &ManagementHandler{svc: svc, opLogService: opLogService}
}

// List 業務員列表（Admin）
func (h *ManagementHandler) List(c *gin.Context) {
	p := common.ParsePaginationParams(c)
	filters := common.ParseFilterParamsWithKeyword(c, []string{"workgroup_id", "role", "status"})

	items, total, err := h.svc.List(p.Page, p.PageSize, filters)
	if err != nil {
		common.HandleServiceError(c, err, "業務員")
		return
	}
	common.PaginatedList(c, items, total, p.Page, p.PageSize)
}

// GetByID 業務員詳情（Admin）
func (h *ManagementHandler) GetByID(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	agent, err := h.svc.GetByID(id)
	if err != nil {
		common.HandleServiceError(c, err, "業務員")
		return
	}
	common.Success(c, agent)
}

type createAgentRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required,min=6"`
	WorkgroupID uint   `json:"workgroup_id" binding:"required"`
	Role        string `json:"role" binding:"required,oneof=leader member"`
}

// Create 建立業務員（Admin）
func (h *ManagementHandler) Create(c *gin.Context) {
	var req createAgentRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	agent := &model.Agent{
		Username:    req.Username,
		Password:    req.Password,
		WorkgroupID: req.WorkgroupID,
		Role:        req.Role,
	}

	if err := h.svc.Create(agent); err != nil {
		common.HandleServiceError(c, err, "業務員")
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpCreate,
		ResourceType:  model.ResAgent,
		ResourceID:    fmt.Sprint(agent.ID),
		ResourceName:  agent.Username,
		AfterValue:    map[string]interface{}{"username": agent.Username, "workgroup_id": agent.WorkgroupID, "role": agent.Role},
	}, c)

	common.Created(c, agent)
}

type updateAgentRequest struct {
	Status *string `json:"status"`
	Role   *string `json:"role"`
}

// Update 更新業務員（Admin）
func (h *ManagementHandler) Update(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req updateAgentRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	updates := make(map[string]interface{})
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.Role != nil {
		updates["role"] = *req.Role
	}

	if err := h.svc.Update(id, updates); err != nil {
		common.HandleServiceError(c, err, "業務員")
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUpdate,
		ResourceType:  model.ResAgent,
		ResourceID:    fmt.Sprint(id),
		AfterValue:    updates,
	}, c)

	common.Success(c, nil)
}

// Delete 刪除業務員（Admin）
func (h *ManagementHandler) Delete(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.svc.Delete(id); err != nil {
		common.HandleServiceError(c, err, "業務員")
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpDelete,
		ResourceType:  model.ResAgent,
		ResourceID:    fmt.Sprint(id),
	}, c)

	common.NoContent(c)
}

type resetPasswordRequest struct {
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// ResetPassword 重置密碼（Admin）
func (h *ManagementHandler) ResetPassword(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req resetPasswordRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.svc.ResetPassword(id, req.NewPassword); err != nil {
		common.HandleServiceError(c, err, "業務員")
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpPasswordReset,
		ResourceType:  model.ResAgent,
		ResourceID:    fmt.Sprint(id),
	}, c)

	common.Success(c, nil)
}
