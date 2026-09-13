// Package apierr 提供统一的 HTTP 错误响应契约，前后端共用。
// 抽出 apierr 的动机：解除 handlers ↔ middleware 之间的 import cycle，
// 任何包都可以引用 apierr 而不破坏分层（F4 from codex audit）。
package apierr

import (
	"errors"
	"net/http"

	"network-monitor-platform/internal/redact"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ErrorResponse 统一错误响应结构（不向客户端泄露内部错误细节）
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// 业务错误码常量（前端可基于 code 走分支逻辑）
const (
	CodeBadRequest       = "bad_request"
	CodeUnauthorized     = "unauthorized"
	CodeForbidden        = "forbidden"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeInternal         = "internal_error"
	CodeDatabaseError    = "database_error"
	CodeValidationFailed = "validation_failed"
)

// Respond 统一错误响应。
// 内部错误（DB / 第三方）只暴露通用文案，原始 err 记到日志。
func Respond(c *gin.Context, status int, code, message string, internalErr error) {
	if internalErr != nil && status >= 500 {
		// 5xx 错误：仅记录原始 err，对外不暴露。
		// G-28：内部错误文本可能带凭据（*url.Error 的完整 URL、DSN、userinfo）→ 出口脱敏。
		// G-43：这条日志是行式消费的（容器 json-file），而 URL.Path 是**解码后**的请求
		// 路径——gin 按解码后路径匹配路由，所以 `GET /api/assets/abc%0d%0a[ERR]%20FORGED`
		// 能带着真 CR/LF 走到这里（实测见 docs/FIX-PLAN-LOG-INJECTION.md §1.2），
		// 裸拼就伪造出一行无从分辨的假错误。整行过 StripControl（method/path/code 全不可信），
		// 再补回换行；顺序必须是 Strip→Text（见 redact.StripControl 注释）。
		// 内层先 Strip 再 Text：Text 必须看到完整的凭据才能整条遮盖，否则 CR/LF 会
		// 截断它的值类，外层的 Strip 再把尾部接回去 = 泄漏（§1.4 的实测反例）。
		internal := redact.Text(redact.StripControl(internalErr.Error()))
		line := redact.StripControl("[ERR] " + c.Request.Method + " " + c.Request.URL.Path +
			" code=" + code + " internal=" + internal)
		gin.DefaultErrorWriter.Write([]byte(line + "\n"))
	}
	c.AbortWithStatusJSON(status, ErrorResponse{
		Code:    code,
		Message: message,
	})
}

// BadRequest 400 — message 已脱敏 + 去控制字符 (M45 / G-31 收口).
// 之前 5xx 路径已过 redact+StripControl (G-28), 4xx helper 入口**也**加,
// 防 caller 拼 `err.Error()` 把凭据/控制字符带进 400 body.
func BadRequest(c *gin.Context, message string) {
	Respond(c, http.StatusBadRequest, CodeBadRequest, sanitizeMessage(message), nil)
}

// Unauthorized 401
func Unauthorized(c *gin.Context, message string) {
	if message == "" {
		message = "未授权或登录已过期"
	}
	Respond(c, http.StatusUnauthorized, CodeUnauthorized, sanitizeMessage(message), nil)
}

// Forbidden 403
func Forbidden(c *gin.Context, message string) {
	if message == "" {
		message = "无访问权限"
	}
	Respond(c, http.StatusForbidden, CodeForbidden, sanitizeMessage(message), nil)
}

// NotFound 404
func NotFound(c *gin.Context, message string) {
	if message == "" {
		message = "资源不存在"
	}
	Respond(c, http.StatusNotFound, CodeNotFound, sanitizeMessage(message), nil)
}

// Conflict 409（资源冲突，如唯一键冲突）
func Conflict(c *gin.Context, message string) {
	if message == "" {
		message = "资源冲突"
	}
	Respond(c, http.StatusConflict, CodeConflict, sanitizeMessage(message), nil)
}

// sanitizeMessage 4xx 路径统一脱敏 + 去控制字符 (M45 / G-31).
// 顺序: StripControl → Text (同 apierr.Respond 5xx 路径的「内层先 Strip 再 Text」,
// 否则 CR/LF 截断 redact.Text 的值类, 外层 Strip 把尾部接回去 = 泄漏).
// 中文静态文案 no-op (StripControl 只去 < 0x20 控制字符, Text 只去 URL/凭据类).
func sanitizeMessage(msg string) string {
	return redact.Text(redact.StripControl(msg))
}

// Internal 500 - 不向客户端暴露原始 err
func Internal(c *gin.Context, message string, internalErr error) {
	if message == "" {
		message = "服务器内部错误"
	}
	Respond(c, http.StatusInternalServerError, CodeInternal, message, internalErr)
}

// TranslateDBError 将 gorm 错误翻译为对外的 4xx 响应，避免泄露 SQL 细节。
// 返回 true 表示已处理（已写响应），false 表示不是 DB 错误（调用方自行处理）。
func TranslateDBError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(c, "")
		return true
	}
	// 其他 DB 错误视为 5xx，不泄露 SQL
	Internal(c, "数据库操作失败", err)
	return true
}
