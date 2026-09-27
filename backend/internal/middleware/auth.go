package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"ky/internal/pkg/response"
	"ky/internal/service"
)

// Auth 返回鉴权中间件：校验 Authorization: Bearer <token> 对应的会话有效性，
func Auth(svc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractBearerToken(c.GetHeader("Authorization"))
		if token == "" {
			response.Unauthorized(c, "未登录")
			c.Abort()
			return
		}
		session, err := svc.ValidateToken(c.Request.Context(), token)
		if err != nil {
			response.Unauthorized(c, "未登录或会话已过期")
			c.Abort()
			return
		}
		c.Set("account_id", session.AccountID)
		c.Next()
	}
}

func extractBearerToken(header string) string {
	const prefix = "bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}
