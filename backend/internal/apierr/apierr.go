// Package apierr 提供统一的 HTTP 错误响应契约，前后端共用。
// 抽出 apierr 的动机：解除 handlers ↔ middleware 之间的 import cycle，
// 任何包都可以引用 apierr 而不破坏分层（F4 from codex audit）。
package apierr

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ErrorResponse 统一错误响应结构（不向客户端泄露内部错误细节）
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id,omitempty"`
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
		// 5xx 错误：仅记录原始 err，对外不暴露
		gin.DefaultErrorWriter.Write([]byte(
			"[ERR] " + c.Request.Method + " " + c.Request.URL.Path +
				" code=" + code + " internal=" + internalErr.Error() + "\n",
		))
	}
	c.AbortWithStatusJSON(status, ErrorResponse{
		Code:    code,
		Message: message,
	})
}

// BadRequest 400
func BadRequest(c *gin.Context, message string) {
	Respond(c, http.StatusBadRequest, CodeBadRequest, message, nil)
}

// Unauthorized 401
func Unauthorized(c *gin.Context, message string) {
	if message == "" {
		message = "未授权或登录已过期"
	}
	Respond(c, http.StatusUnauthorized, CodeUnauthorized, message, nil)
}

// Forbidden 403
func Forbidden(c *gin.Context, message string) {
	if message == "" {
		message = "无访问权限"
	}
	Respond(c, http.StatusForbidden, CodeForbidden, message, nil)
}

// NotFound 404
func NotFound(c *gin.Context, message string) {
	if message == "" {
		message = "资源不存在"
	}
	Respond(c, http.StatusNotFound, CodeNotFound, message, nil)
}

// Conflict 409（资源冲突，如唯一键冲突）
func Conflict(c *gin.Context, message string) {
	if message == "" {
		message = "资源冲突"
	}
	Respond(c, http.StatusConflict, CodeConflict, message, nil)
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
