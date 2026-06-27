package middleware

import (
	"context"
	"log"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AuditConfig 审计日志配置
type AuditConfig struct {
	DB *gorm.DB
	// SkipPaths 不记录审计的路径 (e.g. /healthz, /readyz, /metrics)
	SkipPaths map[string]bool
	// ActionFunc 从 context 推断 Action (默认用 HTTP method)
	ActionFunc func(c *gin.Context) string
	// Async 是否异步写入 (默认 true — 不阻塞请求)
	Async bool
}

// DefaultSkipPaths 默认不审计的路径
func DefaultSkipPaths() map[string]bool {
	return map[string]bool{
		"/healthz":            true,
		"/readyz":             true,
		"/metrics":            true,
		"/api/health":         true,
		"/swagger/index.html": true,
	}
}

// AuditLog 返回审计日志中间件
//
// 用法:
//
//	r.Use(middleware.AuditLog(middleware.AuditConfig{DB: db}))
//
// 写入 models.AuditLog: user/method/path/status/IP/UA/request_id, 异步落库
// 失败仅 log, 不影响主流程
func AuditLog(cfg AuditConfig) gin.HandlerFunc {
	if cfg.SkipPaths == nil {
		cfg.SkipPaths = DefaultSkipPaths()
	}
	if cfg.ActionFunc == nil {
		cfg.ActionFunc = func(c *gin.Context) string { return c.Request.Method }
	}
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if cfg.SkipPaths[path] {
			c.Next()
			return
		}

		c.Next() // 等下游处理完拿到 status

		// 构造审计条目
		entry := buildAuditEntry(c, cfg)

		if cfg.Async {
			// 异步写: 30s timeout, 失败 log
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				err := cfg.DB.WithContext(ctx).
					Session(&gorm.Session{SkipDefaultTransaction: true}).
					Create(entry).Error
				if err != nil {
					log.Printf("[audit] failed to write audit log: %v", err)
				}
			}()
			return
		}

		// 同步写
		err := cfg.DB.
			Session(&gorm.Session{SkipDefaultTransaction: true}).
			Create(entry).Error
		if err != nil {
			log.Printf("[audit] failed to write audit log (sync): %v", err)
		}
	}
}

// buildAuditEntry 收集请求上下文
func buildAuditEntry(c *gin.Context, cfg AuditConfig) *models.AuditLog {
	entry := &models.AuditLog{
		ID:        uuid.New(),
		Action:    cfg.ActionFunc(c),
		Resource:  resourceFromPath(c),
		Method:    c.Request.Method,
		Path:      c.Request.URL.Path,
		IP:        c.ClientIP(),
		UserAgent: truncate(c.GetHeader("User-Agent"), 500),
		Status:    c.Writer.Status(),
		RequestID: c.GetHeader("X-Request-ID"),
		CreatedAt: time.Now(),
	}

	// 优先从 context 拿 user info (AuthMiddleware 已 set)
	if userID := c.GetString("user_id"); userID != "" {
		if parsed, err := uuid.Parse(userID); err == nil {
			entry.UserID = &parsed
		}
	}
	if username := c.GetString("username"); username != "" {
		entry.Username = truncate(username, 100)
	}

	// resource_id from URL param :id
	if idStr := c.Param("id"); idStr != "" {
		if parsed, err := uuid.Parse(idStr); err == nil {
			entry.ResourceID = &parsed
		}
	}

	// 错误信息 (如果有)
	if errMsg := c.GetString("error_msg"); errMsg != "" {
		entry.ErrorMsg = truncate(errMsg, 1000)
	}

	return entry
}

// resourceFromPath 推断资源名 (e.g. /api/assets/:id → "assets")
func resourceFromPath(c *gin.Context) string {
	p := c.FullPath()
	if p == "" {
		return "unknown"
	}
	// /api/assets/:id → "assets"
	// /api/alert-rules/:id → "alert-rules"
	// 简化: 取 path 第一个非空段 (除 /api)
	parts := splitPath(p)
	for _, p := range parts {
		if p == "" || p == "api" {
			continue
		}
		// 跳过动态段
		if len(p) > 0 && p[0] == ':' {
			return ""
		}
		return truncate(p, 100)
	}
	return "unknown"
}

func splitPath(p string) []string {
	var out []string
	start := 0
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			if i > start {
				out = append(out, p[start:i])
			}
			start = i + 1
		}
	}
	if start < len(p) {
		out = append(out, p[start:])
	}
	return out
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
