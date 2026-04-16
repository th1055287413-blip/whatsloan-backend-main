package agent

import (
	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	agentSvc "whatsapp_golang/internal/service/agent"
)

// LeaderHandler Leader 功能處理器
type LeaderHandler struct {
	svc agentSvc.AgentManagementService
}

// NewLeaderHandler 建立 Leader 處理器
func NewLeaderHandler(svc agentSvc.AgentManagementService) *LeaderHandler {
	return &LeaderHandler{svc: svc}
}

func getWorkgroupID(c *gin.Context) (uint, bool) {
	wgID, exists := c.Get("workgroup_id")
	if !exists {
		common.Error(c, common.CodeAuthFailed, "未登入")
		return 0, false
	}
	id, ok := wgID.(uint)
	if !ok {
		common.Error(c, common.CodeInternalError, "工作組資訊錯誤")
		return 0, false
	}
	return id, true
}

// GetMembers 組員列表
func (h *LeaderHandler) GetMembers(c *gin.Context) {
	wgID, ok := getWorkgroupID(c)
	if !ok {
		return
	}

	members, err := h.svc.GetMembers(wgID)
	if err != nil {
		common.HandleServiceError(c, err, "組員")
		return
	}
	common.SuccessList(c, members, int64(len(members)))
}

type createMemberRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
}

// CreateMember 建立組員帳號
func (h *LeaderHandler) CreateMember(c *gin.Context) {
	wgID, ok := getWorkgroupID(c)
	if !ok {
		return
	}

	var req createMemberRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	member := &model.Agent{
		Username: req.Username,
		Password: req.Password,
	}

	if err := h.svc.CreateMember(wgID, member); err != nil {
		common.HandleServiceError(c, err, "組員")
		return
	}
	common.Created(c, member)
}

// GetMember 組員詳情
func (h *LeaderHandler) GetMember(c *gin.Context) {
	memberID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	agent, err := h.svc.GetByID(memberID)
	if err != nil {
		common.HandleServiceError(c, err, "組員")
		return
	}

	// 確認是同工作組
	wgID, _ := getWorkgroupID(c)
	if agent.WorkgroupID != wgID || agent.Role != "member" {
		common.Error(c, common.CodePermissionDenied, "無權查看此組員")
		return
	}

	common.Success(c, agent)
}

type updateMemberRequest struct {
	Status   *string `json:"status"`
	ReadOnly *bool   `json:"read_only"`
}

// UpdateMember 更新組員
func (h *LeaderHandler) UpdateMember(c *gin.Context) {
	wgID, ok := getWorkgroupID(c)
	if !ok {
		return
	}
	memberID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req updateMemberRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	updates := make(map[string]interface{})
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.ReadOnly != nil {
		updates["read_only"] = *req.ReadOnly
	}

	if err := h.svc.UpdateMember(wgID, memberID, updates); err != nil {
		common.HandleServiceError(c, err, "組員")
		return
	}
	common.Success(c, nil)
}

// DeleteMember 刪除組員
func (h *LeaderHandler) DeleteMember(c *gin.Context) {
	wgID, ok := getWorkgroupID(c)
	if !ok {
		return
	}
	memberID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.svc.DeleteMember(wgID, memberID); err != nil {
		common.HandleServiceError(c, err, "組員")
		return
	}
	common.NoContent(c)
}

// ResetMemberPassword 重置組員密碼
func (h *LeaderHandler) ResetMemberPassword(c *gin.Context) {
	wgID, ok := getWorkgroupID(c)
	if !ok {
		return
	}
	memberID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req resetPasswordRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.svc.ResetMemberPassword(wgID, memberID, req.NewPassword); err != nil {
		common.HandleServiceError(c, err, "組員密碼")
		return
	}
	common.Success(c, nil)
}

type assignMemberAccountsRequest struct {
	AccountIDs []uint `json:"account_ids" binding:"required,min=1"`
}

// AssignMemberAccounts 分配帳號給組員
func (h *LeaderHandler) AssignMemberAccounts(c *gin.Context) {
	wgID, ok := getWorkgroupID(c)
	if !ok {
		return
	}
	memberID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req assignMemberAccountsRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.svc.AssignAccountsToMember(wgID, memberID, req.AccountIDs); err != nil {
		common.HandleServiceError(c, err, "帳號分配")
		return
	}
	common.Success(c, nil)
}

// RemoveMemberAccounts 移除組員帳號
func (h *LeaderHandler) RemoveMemberAccounts(c *gin.Context) {
	wgID, ok := getWorkgroupID(c)
	if !ok {
		return
	}
	memberID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req assignMemberAccountsRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.svc.RemoveAccountsFromMember(wgID, memberID, req.AccountIDs); err != nil {
		common.HandleServiceError(c, err, "帳號移除")
		return
	}
	common.Success(c, nil)
}

// GetMemberAccounts 組員帳號列表
func (h *LeaderHandler) GetMemberAccounts(c *gin.Context) {
	wgID, ok := getWorkgroupID(c)
	if !ok {
		return
	}
	memberID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	accounts, err := h.svc.GetMemberAccounts(wgID, memberID)
	if err != nil {
		common.HandleServiceError(c, err, "組員帳號")
		return
	}
	common.SuccessList(c, accounts, int64(len(accounts)))
}

// GetWorkgroupSettings 取得工作組設定
func (h *LeaderHandler) GetWorkgroupSettings(c *gin.Context) {
	wgID, ok := getWorkgroupID(c)
	if !ok {
		return
	}

	wg, err := h.svc.GetWorkgroup(wgID)
	if err != nil {
		common.HandleServiceError(c, err, "工作組")
		return
	}
	common.Success(c, wg)
}

type updateWorkgroupSettingsRequest struct {
	AccountVisibility *string `json:"account_visibility" binding:"omitempty,oneof=assigned shared"`
}

// UpdateWorkgroupSettings 更新工作組設定
func (h *LeaderHandler) UpdateWorkgroupSettings(c *gin.Context) {
	wgID, ok := getWorkgroupID(c)
	if !ok {
		return
	}

	var req updateWorkgroupSettingsRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	updates := make(map[string]interface{})
	if req.AccountVisibility != nil {
		updates["account_visibility"] = *req.AccountVisibility
	}

	if len(updates) == 0 {
		common.Error(c, common.CodeInvalidParams, "沒有要更新的欄位")
		return
	}

	if err := h.svc.UpdateWorkgroupSettings(wgID, updates); err != nil {
		common.HandleServiceError(c, err, "工作組設定")
		return
	}
	common.Success(c, nil)
}
