package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// Recovery 替代 gin.Recovery()。
//
// 为什么不用 gin.CustomRecovery：请求头 dump 发生在 gin 的
// CustomRecoveryWithWriter **内部**，在调用自定义 handler **之前**
// （gin@v1.9.1/recovery.go:82-100）—— 也就是说无论传什么 handle，debug 模式下
// 它都会把 httputil.DumpRequest 的结果写进 DefaultErrorWriter，且只把 Authorization
// 换成 *：**Cookie 原样保留**。本项目的浏览器端凭据正是 httpOnly cookie auth_token
// （JWT，默认 24h 有效），而 server.mode 默认 debug（config.go 的 SetDefault +
// docker-compose.yml），于是任意 handler panic 都会把可直接重放的会话令牌写进日志。
//
// 所以这里完全手写：只记 panic 值、路由与调用栈 —— debug.Stack 只含文件/行号/
// 函数名，不含局部变量，也不碰请求头。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered",
					slog.Any("panic", err),
					slog.String("method", c.Request.Method),
					slog.String("path", c.Request.URL.Path),
					slog.String("request_id", c.GetHeader("X-Request-ID")),
					slog.String("stack", string(debug.Stack())),
				)
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}
