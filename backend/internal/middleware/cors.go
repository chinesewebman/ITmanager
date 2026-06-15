// Package middleware CORS 跨域中间件
// C-F4: 修复后端 0 CORS 配置
// 实现要点：
//   - 允许来源白名单（从 config.AllowedOrigins 读）
//   - 允许方法 + 关键 header
//   - AllowCredentials=true（与 HttpOnly cookie 配对使用）
//   - 预检请求（OPTIONS）直接 204 放行
package middleware

import (
	"net/http"
	"strings"

	"network-monitor-platform/internal/config"

	"github.com/gin-gonic/gin"
)

// CORS 跨域中间件（C-F4）
// 用法：r.Use(middleware.CORS(cfg))
func CORS(cfg *config.Config) gin.HandlerFunc {
	origins := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		origins[strings.TrimSpace(o)] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// 仅在 origin 在白名单内才回显 ACAO 头
		if _, ok := origins[origin]; ok {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key, X-Requested-With")
			c.Header("Access-Control-Expose-Headers", "Content-Length, Content-Disposition")
			c.Header("Access-Control-Max-Age", "600")
		}

		// 预检请求：直接 204 放行
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
