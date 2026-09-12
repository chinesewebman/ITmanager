package middleware

import (
	"context"
	"log/slog"
	"time"

	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/redact"

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
	// Async 是否异步写入（零值 false = 同步写，响应返回时审计行已落库；
	// 登录路由依赖这一点做测试断言，见 docs/FIX-PLAN-AUTHZ-CLOSURE.md §2 D-E）
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
					slog.Warn("audit: failed to write audit log (async)", slog.String("err", err.Error()))
				}
			}()
			return
		}

		// 同步写
		err := cfg.DB.
			Session(&gorm.Session{SkipDefaultTransaction: true}).
			Create(entry).Error
		if err != nil {
			slog.Warn("audit: failed to write audit log (sync)", slog.String("err", err.Error()))
		}
	}
}

// buildAuditEntry 收集请求上下文
func buildAuditEntry(c *gin.Context, cfg AuditConfig) *models.AuditLog {
	entry := &models.AuditLog{
		ID:       uuid.New(),
		Action:   cfg.ActionFunc(c),
		Resource: resourceFromPath(c),
		Method:   c.Request.Method,
		// G-44：以下字段全部走 sanitizeField（净化 + 按字符截断到列宽）。
		// 审计行是取证链，丢一行比日志难看严重得多 —— 而 PG 的 varchar(n) 按**字符**
		// 计数、且拒收非法 UTF-8，所以「超长」和「按字节切出半个汉字」都会让整行
		// INSERT 被拒（22001/22021）→ 静默丢行。两者都在这里一次性关掉。
		Path:      sanitizeField(c.Request.URL.Path, 500),
		IP:        c.ClientIP(),
		UserAgent: sanitizeField(c.GetHeader("User-Agent"), 500),
		Status:    c.Writer.Status(),
		// 列宽 varchar(50)，而它此前完全不截断，且挂载点包含**未认证**的登录路由
		// （routes.go:227）→ 攻击者发个超长 X-Request-ID 就能让登录审计行消失。
		RequestID: sanitizeField(c.GetHeader("X-Request-ID"), 50),
		CreatedAt: time.Now(),
	}

	// 优先从 context 拿 user info (AuthMiddleware 已 set)
	if userID := c.GetString("user_id"); userID != "" {
		if parsed, err := uuid.Parse(userID); err == nil {
			entry.UserID = &parsed
		} else {
			// P2: parse 失败时 log warn 便于 forensic (审计元数据保底, 原始字符串仍在 Username)
			slog.Warn("audit: user_id 不是合法 uuid", slog.String("raw", userID), slog.String("err", err.Error()))
		}
	}
	if username := c.GetString("username"); username != "" {
		// 纵深防御：登录路由的 username 已经过 handlers.sanitizeAuditUsername，
		// 但 protected 组走的是 auth.go 直接 set 的原值（不经那道净化）。
		entry.Username = sanitizeField(username, 100)
	}

	// resource_id from URL param :id
	if idStr := c.Param("id"); idStr != "" {
		if parsed, err := uuid.Parse(idStr); err == nil {
			entry.ResourceID = &parsed
		}
	}

	// 错误信息 (如果有)。
	// 注：全仓没有 error_msg 的 setter，这是条**当前不可达**的路径；仍一并改掉是因为
	// 它就在同一函数里、同一类缺陷（按字节截断 varchar(1000)），留着就是给下一个人
	// 埋的同一个坑，而改动对可达输入零影响（分支不执行）。
	if errMsg := c.GetString("error_msg"); errMsg != "" {
		entry.ErrorMsg = sanitizeField(errMsg, 1000)
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
		// 跳过动态段, 继续找第一个静态段
		// 修 audit-P1: /api/:tenant/users → "users" (旧版返 "")
		// G-55: 列宽 VARCHAR(50)（migrations/000001_init.up.sql:1097），不再用 100——那是
		// 历史副本漂移，截到 100 仍可能超 50 → 22001 → 整行 INSERT 被拒 → 审计链少一行。
		if len(p) > 0 && p[0] == ':' {
			continue
		}
		return sanitizeField(p, 50)
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

// truncateRunes 按**字符**截断到 max 个 rune（口径同 service.truncateRunes）。
// 不能用上面的 truncate（按 byte）：`truncate("中"*200, 500)` 会切在半个汉字中间，
// 产出非法 UTF-8，PG 拒收（22021）→ 审计行照样丢，只是从「超长」换成「编码非法」。
// 尺子必须是 rune —— varchar(n) 数的是字符。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// sanitizeField 把不可信文本规整成可安全落审计列的字段：剥控制字符 → 按字符截断到列宽。
//
// 两件事各有理由，缺一不可：
//   - 剥控制字符（redact.StripControl）：审计行是行式消费的（SIEM / 日志导出 / CSV），
//     CR/LF 能把一行伪造成多条记录（CWE-117）；NUL/非法字节让 PG 拒收（22021）。
//   - 按字符截断：varchar(n) 按字符计数，超长撞 22001 → 整行丢失。
//
// **不做 redact.Text（脱敏）**：审计字段是取证材料，脱敏会破坏其证据价值。
// 这里只防「伪造」与「丢行」，不防「泄漏」——那是日志出口的职责，别把两件事混一起。
func sanitizeField(s string, maxRunes int) string {
	return truncateRunes(redact.StripControl(s), maxRunes)
}
