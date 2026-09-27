package middleware

import (
	"net"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS 跨域中间件：仅放行本地网络来源（回环 / RFC1918 私网 / IPv6 ULA 与链路本地）
// 令牌置于 Authorization 头，本中间件主要防的是公网恶意页面携窃取令牌发起跨域请求。
// TODO: 公网部署时要把allowed_origins配上
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[strings.ToLower(strings.TrimSpace(o))] = true
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && originAllowed(origin, allowed) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")
		}
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

func originAllowed(origin string, allowed map[string]bool) bool {
	if allowed[strings.ToLower(origin)] {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if ip == nil {
		return false // 主机名来源仅靠白名单放行
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// 10/8、172.16/12、192.168/16
		return v4[0] == 10 ||
			(v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31) ||
			v4[0] == 192 && v4[1] == 168
	}
	b := ip.To16()
	return b[0] == 0xfc || b[0] == 0xfd
}
