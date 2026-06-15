package handlers

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

// RespondError 统一错误响应。
// 内部错误（DB / 第三方）只暴露通用文案，原始 err 记到日志。
func RespondError(c *gin.Context, status int, code, message string, internalErr error) {
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

// RespondBadRequest 400 快捷方法
func RespondBadRequest(c *gin.Context, message string) {
	RespondError(c, http.StatusBadRequest, CodeBadRequest, message, nil)
}

// RespondUnauthorized 401
func RespondUnauthorized(c *gin.Context, message string) {
	if message == "" {
		message = "未授权或登录已过期"
	}
	RespondError(c, http.StatusUnauthorized, CodeUnauthorized, message, nil)
}

// RespondForbidden 403
func RespondForbidden(c *gin.Context, message string) {
	if message == "" {
		message = "无访问权限"
	}
	RespondError(c, http.StatusForbidden, CodeForbidden, message, nil)
}

// RespondNotFound 404
func RespondNotFound(c *gin.Context, message string) {
	if message == "" {
		message = "资源不存在"
	}
	RespondError(c, http.StatusNotFound, CodeNotFound, message, nil)
}

// RespondInternal 500 - 不向客户端暴露原始 err
func RespondInternal(c *gin.Context, message string, internalErr error) {
	if message == "" {
		message = "服务器内部错误"
	}
	RespondError(c, http.StatusInternalServerError, CodeInternal, message, internalErr)
}

// TranslateDBError 将 gorm 错误翻译为对外的 4xx 响应，避免泄露 SQL 细节。
// 返回 true 表示已处理（已写响应），false 表示不是 DB 错误（调用方自行处理）。
func TranslateDBError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		RespondNotFound(c, "")
		return true
	}
	// 其他 DB 错误视为 5xx，不泄露 SQL
	RespondInternal(c, "数据库操作失败", err)
	return true
}
