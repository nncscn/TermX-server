package middleware

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"ky/internal/pkg/response"
)

// rateWindow 一个 IP 的计数窗口。
type rateWindow struct {
	count int
	start time.Time
}

// RateLimiter 每 IP 固定窗口限流器（内存实现,重启即清零）
type RateLimiter struct {
	limit  int
	window time.Duration
	mu     sync.Mutex
	ips    map[string]*rateWindow
}

// NewRateLimiter 构造限流器：window 内每 IP 最多 limit 次
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, ips: map[string]*rateWindow{}}
}

// Cleanup 定时清理过期的 IP 条目（防止 map 无限增长耗尽内存）。
func (r *RateLimiter) Cleanup() {
	ticker := time.NewTicker(r.window)
	for range ticker.C {
		r.mu.Lock()
		now := time.Now()
		for ip, w := range r.ips {
			if now.Sub(w.start) >= r.window {
				delete(r.ips, ip)
			}
		}
		r.mu.Unlock()
	}
}

func (r *RateLimiter) Handle() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		r.mu.Lock()
		w, ok := r.ips[ip]
		now := time.Now()
		if !ok || now.Sub(w.start) >= r.window {
			w = &rateWindow{start: now}
			r.ips[ip] = w
		}
		w.count++
		over := w.count > r.limit
		r.mu.Unlock()

		if over {
			response.TooFrequent(c, "操作过于频繁，请稍后再试")
			c.Abort()
			return
		}
		c.Next()
	}
}
