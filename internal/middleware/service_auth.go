package middleware

import (
	"crypto/subtle"

	"github.com/gin-gonic/gin"
	handler "whatsapp_golang/internal/handler/common"
)

// ServiceAPIKeyMiddleware 內部微服務 API Key 認證中間件
func ServiceAPIKeyMiddleware(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if apiKey == "" {
			handler.Error(c, handler.CodeAuthFailed, "service API 未啟用")
			c.Abort()
			return
		}

		provided := c.GetHeader("X-Service-API-Key")
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(apiKey)) != 1 {
			handler.Error(c, handler.CodeAuthFailed, "無效的 API Key")
			c.Abort()
			return
		}

		c.Next()
	}
}
