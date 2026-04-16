package agent

import (
	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	agentSvc "whatsapp_golang/internal/service/agent"
)

// AuthHandler Agent 認證處理器
type AuthHandler struct {
	authService agentSvc.AgentAuthService
	mgmtService agentSvc.AgentManagementService
}

// NewAuthHandler 建立 Agent 認證處理器
func NewAuthHandler(authService agentSvc.AgentAuthService, mgmtService agentSvc.AgentManagementService) *AuthHandler {
	return &AuthHandler{authService: authService, mgmtService: mgmtService}
}

type agentLoginRequest struct {
	WorkgroupCode string `json:"workgroup_code" binding:"required"`
	Username      string `json:"username" binding:"required"`
	Password      string `json:"password" binding:"required"`
}

type agentLoginResponse struct {
	Token string            `json:"token"`
	Agent *agentProfileData `json:"agent"`
}

type agentProfileData struct {
	ID              uint   `json:"id"`
	Username        string `json:"username"`
	WorkgroupID     uint   `json:"workgroup_id"`
	WorkgroupCode   string `json:"workgroup_code,omitempty"`
	WorkgroupName   string `json:"workgroup_name,omitempty"`
	WorkgroupStatus string `json:"workgroup_status,omitempty"`
	Role            string `json:"role"`
	Status          string `json:"status"`
	ReadOnly        bool   `json:"read_only"`
}

func toAgentProfile(a *model.Agent) *agentProfileData {
	return &agentProfileData{
		ID:          a.ID,
		Username:    a.Username,
		WorkgroupID: a.WorkgroupID,
		Role:        a.Role,
		Status:      a.Status,
		ReadOnly:    a.ReadOnly,
	}
}

func toAgentProfileWithWorkgroup(a *model.Agent, wg *model.Workgroup) *agentProfileData {
	p := toAgentProfile(a)
	if wg != nil {
		p.WorkgroupCode = wg.Code
		p.WorkgroupName = wg.Name
		p.WorkgroupStatus = wg.Status
	}
	return p
}

// Login Agent 登入
func (h *AuthHandler) Login(c *gin.Context) {
	var req agentLoginRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	result, err := h.authService.Login(req.WorkgroupCode, req.Username, req.Password, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		common.Error(c, common.CodeAuthFailed, err.Error())
		return
	}

	common.Success(c, agentLoginResponse{
		Token: result.Token,
		Agent: toAgentProfileWithWorkgroup(result.Agent, result.Workgroup),
	})
}

// Logout Agent 登出
func (h *AuthHandler) Logout(c *gin.Context) {
	token, _ := c.Get("agent_token")
	if t, ok := token.(string); ok {
		_ = h.authService.Logout(t)
	}
	common.Success(c, nil)
}

// GetProfile 取得個人資料
func (h *AuthHandler) GetProfile(c *gin.Context) {
	agent, exists := c.Get("agent")
	if !exists {
		common.Error(c, common.CodeAuthFailed, "未登入")
		return
	}
	a := agent.(*model.Agent)
	wg, _ := h.mgmtService.GetWorkgroup(a.WorkgroupID)
	common.Success(c, toAgentProfileWithWorkgroup(a, wg))
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// ChangePassword 修改密碼
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req changePasswordRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	agentID, _ := c.Get("agent_id")
	if err := h.authService.ChangePassword(agentID.(uint), req.OldPassword, req.NewPassword); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, nil)
}
