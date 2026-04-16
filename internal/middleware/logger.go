package middleware

import (
	"whatsapp_golang/internal/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// LoggerMiddleware 注入 request_id 到 context logger
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		ctx := logger.WithRequestCtx(c.Request.Context(), requestID)
		c.Request = c.Request.WithContext(ctx)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}
