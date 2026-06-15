// HTTP metrics 中间件（C-P5）。
package middleware

import (
	"strconv"
	"time"

	"network-monitor-platform/internal/metrics"

	"github.com/gin-gonic/gin"
)

// HTTPMetrics 返回一个 gin middleware，统计每个 HTTP 请求的耗时与计数。
// 必须传入同一个 metrics.Registry（与 /metrics 端点共用）。
//
// 写入两个 metric：
//   - http_requests_total{method, path, status}：计数器
//   - http_request_duration_seconds{method, path}：直方图
//
// path 取 c.FullPath()（即路由模板，如 "/assets/:id"），
// 避免高基数（如果直接取 c.Request.URL.Path 会因 :id 爆炸）。
func HTTPMetrics(reg *metrics.Registry) gin.HandlerFunc {
	if reg == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		dur := time.Since(start).Seconds()

		// 路由模板优先；未匹配走 "unmatched" 防基数爆炸
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		method := c.Request.Method
		status := strconv.Itoa(c.Writer.Status())

		// 写入 metric（Registry 内部按 name 查找，未注册则 noop）
		reg.IncCounter("http_requests_total", method, path, status)
		reg.ObserveHistogram("http_request_duration_seconds", dur, method, path)
	}
}
