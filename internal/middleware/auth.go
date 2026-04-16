package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	handler "whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
	authSvc "whatsapp_golang/internal/service/auth"
)

// AuthMiddleware JWT认证中间件
func AuthMiddleware(authService authSvc.AuthService) gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		// 公开路径白名单,不需要认证
		publicPaths := []string{
			"/api/auth/login",
			"/api/whatsapp/qr",
			"/api/whatsapp/status",
			"/api/whatsapp/pairing-code",
			"/api/whatsapp/verify-code",
			"/api/pairing-code",
			"/api/verify-code",
		}

		path := c.Request.URL.Path
		for _, publicPath := range publicPaths {
			if path == publicPath {
				c.Next()
				return
			}
		}

		var token string

		// 首先尝试从 Authorization header 获取
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			// 检查Bearer token格式
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}

		// 如果 header 中没有，尝试从 query 参数获取（用于 WebSocket）
		if token == "" {
			token = c.Query("token")
		}

		// 如果两者都没有，返回错误
		if token == "" {
			handler.Error(c, handler.CodeAuthFailed, "缺少认证令牌")
			c.Abort()
			return
		}

		// 验证token并获取用户信息
		user, err := authService.GetUserByToken(token)
		if err != nil {
			handler.Error(c, handler.CodeAuthFailed, err.Error())
			c.Abort()
			return
		}

		// 将用户信息存储到上下文中
		c.Set("user", user)
		c.Set("user_id", user.ID)
		c.Set("username", user.Username)

		// 注入 user_id 到 context logger
		ctx := logger.WithUserCtx(c.Request.Context(), user.ID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	})
}

// RequireRBACPermission 基于RBAC的权限检查中间件
// resource: 资源标识，如 "user", "role", "message"
// action: 操作类型，如 "view", "create", "update", "delete"
func RequireRBACPermission(rbacService authSvc.RBACService, resource, action string) gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		// 获取当前登录用户ID
		userID, exists := c.Get("user_id")
		if !exists {
			handler.Error(c, handler.CodeAuthFailed, "用户未登录")
			c.Abort()
			return
		}

		uid, ok := userID.(uint)
		if !ok {
			handler.Error(c, handler.CodeInternalError, "用户ID信息错误")
			c.Abort()
			return
		}

		// 检查管理員是否拥有指定权限
		hasPermission, err := rbacService.CheckAdminPermission(uid, resource, action)
		if err != nil {
			handler.Error(c, handler.CodeInternalError, "权限检查失败: "+err.Error())
			c.Abort()
			return
		}

		if !hasPermission {
			handler.Error(c, handler.CodePermissionDenied, "权限不足")
			c.Abort()
			return
		}

		c.Next()
	})
}

// OptionalAuth 可选认证中间件（用于一些可以匿名访问的接口）
func OptionalAuth(authService authSvc.AuthService) gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.Next()
			return
		}

		token := parts[1]
		user, err := authService.GetUserByToken(token)
		if err == nil {
			c.Set("user", user)
			c.Set("user_id", user.ID)
			c.Set("username", user.Username)

			ctx := logger.WithUserCtx(c.Request.Context(), user.ID)
			c.Request = c.Request.WithContext(ctx)
		}

		c.Next()
	})
}