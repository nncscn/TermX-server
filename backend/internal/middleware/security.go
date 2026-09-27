// Package middleware 提供 HTTP 中间件。security.go 承载通用安全加固：
// 请求体大小限制（防远程 DoS）+ 安全响应头（防点击劫持/MIME 嗅探/缓存泄露）。
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// maxBodyBytes 请求体上限：凭据密文最大 16KB，导出/导入最大几 MB——
const maxBodyBytes = 10 << 20 // 10MB

// Security 通用安全加固中间件：
//   - 限制请求体 10MB，超限返回 413（P1：防远程无认证 DoS）
//   - 设置安全响应头（P3：防点击劫持/MIME 嗅探/来源泄露/缓存泄露）
func Security() gin.HandlerFunc {
	return func(c *gin.Context) {
		// P3: 安全响应头
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		// API 响应含敏感数据（令牌/密钥/凭据），禁止任何层缓存
		if len(c.Request.URL.Path) > 4 && c.Request.URL.Path[:4] == "/api" {
			c.Header("Cache-Control", "no-store")
		}

		// P1: 请求体大小限制
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
		}
		c.Next()
	}
}
