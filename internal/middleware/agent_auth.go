package middleware

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	handler "whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	agentSvc "whatsapp_golang/internal/service/agent"
)

// AgentAuthMiddleware Agent JWT 認證中間件
func AgentAuthMiddleware(agentAuthService agentSvc.AgentAuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var token string

		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}

		if token == "" {
			handler.Error(c, handler.CodeAuthFailed, "缺少認證令牌")
			c.Abort()
			return
		}

		agent, err := agentAuthService.GetAgentByToken(token)
		if err != nil {
			handler.Error(c, handler.CodeAuthFailed, err.Error())
			c.Abort()
			return
		}

		c.Set("agent", agent)
		c.Set("agent_id", agent.ID)
		c.Set("workgroup_id", agent.WorkgroupID)
		c.Set("agent_role", agent.Role)
		c.Set("agent_token", token)

		c.Next()
	}
}

// AgentChatAccessMiddleware 檢查 agent 是否有權操作指定對話
func AgentChatAccessMiddleware(opsSvc agentSvc.AgentOperationsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID, exists := c.Get("agent_id")
		if !exists {
			handler.Error(c, handler.CodeAuthFailed, "未登入")
			c.Abort()
			return
		}

		chatIDStr := c.Param("chatId")
		if chatIDStr == "" {
			handler.Error(c, handler.CodeInvalidParams, "缺少 chatId")
			c.Abort()
			return
		}

		chatID, err := strconv.ParseUint(chatIDStr, 10, 32)
		if err != nil {
			handler.Error(c, handler.CodeInvalidParams, "無效的 chatId")
			c.Abort()
			return
		}

		if err := opsSvc.VerifyChatAccess(agentID.(uint), uint(chatID)); err != nil {
			handler.Error(c, handler.CodePermissionDenied, err.Error())
			c.Abort()
			return
		}

		c.Next()
	}
}
// AgentAccountAccessMiddleware 檢查 agent 是否有權存取指定帳號（從 query param account_id 讀取）
func AgentAccountAccessMiddleware(opsSvc agentSvc.AgentOperationsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID, exists := c.Get("agent_id")
		if !exists {
			handler.Error(c, handler.CodeAuthFailed, "未登入")
			c.Abort()
			return
		}

		accountIDStr := c.Query("account_id")
		if accountIDStr == "" {
			handler.Error(c, handler.CodeInvalidParams, "缺少 account_id")
			c.Abort()
			return
		}

		accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
		if err != nil {
			handler.Error(c, handler.CodeInvalidParams, "無效的 account_id")
			c.Abort()
			return
		}

		ok, err := opsSvc.CanAccessAccount(agentID.(uint), uint(accountID))
		if err != nil || !ok {
			handler.Error(c, handler.CodePermissionDenied, "無權存取此帳號")
			c.Abort()
			return
		}

		c.Next()
	}
}

// AgentAccountPathAccessMiddleware 檢查 agent 是否有權存取指定帳號（從 path param :id 讀取）
func AgentAccountPathAccessMiddleware(opsSvc agentSvc.AgentOperationsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID, exists := c.Get("agent_id")
		if !exists {
			handler.Error(c, handler.CodeAuthFailed, "未登入")
			c.Abort()
			return
		}

		accountIDStr := c.Param("id")
		if accountIDStr == "" {
			handler.Error(c, handler.CodeInvalidParams, "缺少 id")
			c.Abort()
			return
		}

		accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
		if err != nil {
			handler.Error(c, handler.CodeInvalidParams, "無效的 id")
			c.Abort()
			return
		}

		ok, err := opsSvc.CanAccessAccount(agentID.(uint), uint(accountID))
		if err != nil || !ok {
			handler.Error(c, handler.CodePermissionDenied, "無權存取此帳號")
			c.Abort()
			return
		}

		c.Next()
	}
}

// WorkgroupActiveMiddleware 檢查 agent 所屬工作組是否啟用
func WorkgroupActiveMiddleware(mgmtSvc agentSvc.AgentManagementService) gin.HandlerFunc {
	return func(c *gin.Context) {
		wgID, exists := c.Get("workgroup_id")
		if !exists {
			handler.Error(c, handler.CodeAuthFailed, "未登入")
			c.Abort()
			return
		}

		wg, err := mgmtSvc.GetWorkgroup(wgID.(uint))
		if err != nil {
			handler.Error(c, handler.CodeInternalError, "工作組不存在")
			c.Abort()
			return
		}

		if wg.Status != "active" {
			handler.Error(c, handler.CodePermissionDenied, "workgroup_disabled")
			c.Abort()
			return
		}

		c.Next()
	}
}

// AgentWritePermission 唯讀模式檢查：leader 放行，member read_only=true 擋掉
func AgentWritePermission() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get("agent_role")
		if role == "leader" {
			c.Next()
			return
		}

		agent, exists := c.Get("agent")
		if !exists {
			handler.Error(c, handler.CodeAuthFailed, "未登入")
			c.Abort()
			return
		}

		if a, ok := agent.(*model.Agent); ok && a.ReadOnly {
			handler.Error(c, handler.CodePermissionDenied, "唯讀模式，無法執行寫入操作")
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireAgentRole 角色檢查中間件
func RequireAgentRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentRole, exists := c.Get("agent_role")
		if !exists {
			handler.Error(c, handler.CodeAuthFailed, "未登入")
			c.Abort()
			return
		}

		role, ok := agentRole.(string)
		if !ok {
			handler.Error(c, handler.CodeInternalError, "角色資訊錯誤")
			c.Abort()
			return
		}

		for _, r := range roles {
			if role == r {
				c.Next()
				return
			}
		}

		handler.Error(c, handler.CodePermissionDenied, "權限不足")
		c.Abort()
	}
}
