package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"network-monitor-platform/internal/service"
)

// actorFromContext 从 gin 上下文取出「这次写是谁经手的」。
//
// JWT 与 API Key 两条路径都由 auth 中间件写入 user_id / username
// （middleware/auth.go:120-122 与 :194-197），所以这里只需读，不必再查库 ——
// 与 audit_logs 同一口径（它记的也是这两个值，不复查用户是否仍存在）。
//
// 为什么收成一处而不是每个 handler 各写一遍：两处各自 parse 迟早分叉，而这里分叉的后果是
// **历史归属错人** —— 错的历史比没有历史更糟，它会被当成证据用。
//
// user_id 解析不出来（缺失、非法 UUID、内部调用没有 ctx）时 ID 留 nil：测试与非常规路径下
// 中间件可能只写了 username，宁可有名无 id，也不要把「谁经手」整个丢掉。
// username 缺失时兜底 "unknown"，与 CreateTicketFromAlert 原有行为一致。
func actorFromContext(c *gin.Context) service.Actor {
	a := service.Actor{Name: c.GetString("username")}
	if a.Name == "" {
		a.Name = "unknown"
	}
	if id, err := uuid.Parse(c.GetString("user_id")); err == nil {
		a.ID = &id
	}
	return a
}
