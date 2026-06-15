package apierr

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// fmtErrorf 是 fmt.Errorf 的间接调用，方便测试文件内重写
var fmtErrorf = fmt.Errorf

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestCtx 创建一个测试 gin context（带 response recorder）
func newTestCtx() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/test/path", nil)
	c.Request = req
	return c, w
}

// ==================== Respond 测试 ====================

func TestRespond_Basic4xx_NoInternalLog(t *testing.T) {
	c, w := newTestCtx()
	Respond(c, http.StatusBadRequest, CodeBadRequest, "参数错误", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"bad_request"`)
	assert.Contains(t, w.Body.String(), `"message":"参数错误"`)
}

func TestRespond_5xx_InternalErr_DoesNotLeakToBody(t *testing.T) {
	c, w := newTestCtx()
	internalErr := errors.New("connection refused")
	Respond(c, http.StatusInternalServerError, CodeInternal, "服务器内部错误", internalErr)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	// 响应里不能暴露 internal err 文本（防信息泄露）
	assert.NotContains(t, w.Body.String(), "connection refused")
	assert.Contains(t, w.Body.String(), `"code":"internal_error"`)
}

func TestRespond_5xx_NoInternalErr_StillWorks(t *testing.T) {
	c, w := newTestCtx()
	Respond(c, http.StatusServiceUnavailable, CodeInternal, "服务不可用", nil)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "服务不可用")
}

func TestRespond_BodyIsValidJSON(t *testing.T) {
	c, w := newTestCtx()
	Respond(c, http.StatusForbidden, CodeForbidden, "无权限", nil)
	body := w.Body.String()
	assert.Contains(t, body, `{"code":"forbidden","message":"无权限"}`)
}

func TestRespond_AbortChainSoMiddlewareStops(t *testing.T) {
	c, _ := newTestCtx()
	Respond(c, http.StatusBadRequest, CodeBadRequest, "x", nil)
	assert.True(t, c.IsAborted(), "Respond 应调用 AbortWithStatusJSON")
}

// ==================== Helper 默认消息测试 ====================

func TestUnauthorized_EmptyMessage_DefaultText(t *testing.T) {
	c, w := newTestCtx()
	Unauthorized(c, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "未授权")
}

func TestForbidden_EmptyMessage_DefaultText(t *testing.T) {
	c, w := newTestCtx()
	Forbidden(c, "")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "无访问权限")
}

func TestNotFound_EmptyMessage_DefaultText(t *testing.T) {
	c, w := newTestCtx()
	NotFound(c, "")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "资源不存在")
}

func TestInternal_EmptyMessage_DefaultText_NoLeak(t *testing.T) {
	c, w := newTestCtx()
	Internal(c, "", errors.New("secret-db-error-text"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "服务器内部错误")
	// 内部 err 文本不应泄露
	assert.NotContains(t, w.Body.String(), "secret-db-error-text")
}

func TestBadRequest_PassesMessage(t *testing.T) {
	c, w := newTestCtx()
	BadRequest(c, "字段缺失")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "字段缺失")
}

func TestBadRequest_EmptyMessage_StillValidJSON(t *testing.T) {
	c, w := newTestCtx()
	BadRequest(c, "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"bad_request"`)
}

func TestHelpers_CustomMessageOverridesDefault(t *testing.T) {
	// 自定义 message 应优先于默认值
	c, w := newTestCtx()
	NotFound(c, "工单 #42 不存在")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "工单 #42 不存在")
	assert.NotContains(t, w.Body.String(), "资源不存在")
}

// ==================== TranslateDBError 测试 ====================

func TestTranslateDBError_Nil_ReturnsFalse(t *testing.T) {
	c, w := newTestCtx()
	handled := TranslateDBError(c, nil)
	assert.False(t, handled, "nil err 应返 false 让 caller 自处理")
	// 不应写任何响应
	assert.Equal(t, http.StatusOK, w.Code) // 默认 200（未写）
	assert.Empty(t, w.Body.String())
}

func TestTranslateDBError_RecordNotFound_Returns404(t *testing.T) {
	c, w := newTestCtx()
	handled := TranslateDBError(c, gorm.ErrRecordNotFound)
	assert.True(t, handled)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"not_found"`)
}

func TestTranslateDBError_OtherDBError_Returns500_NoSQLLeak(t *testing.T) {
	c, w := newTestCtx()
	dbErr := errors.New(`pq: duplicate key value violates unique constraint "users_username_key"`)
	handled := TranslateDBError(c, dbErr)
	assert.True(t, handled)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	// 关键：SQL 错误细节（含表名/列名）不暴露给客户端
	assert.NotContains(t, w.Body.String(), "users_username_key")
	assert.NotContains(t, w.Body.String(), "pq:")
	assert.NotContains(t, w.Body.String(), "duplicate")
	assert.Contains(t, w.Body.String(), "数据库操作失败")
}

func TestTranslateDBError_WrappedRecordNotFound_StillRecognized(t *testing.T) {
	// errors.Join 应让 errors.Is 链生效
	c, w := newTestCtx()
	realWrap := errors.Join(errors.New("context"), gorm.ErrRecordNotFound)
	handled := TranslateDBError(c, realWrap)
	assert.True(t, handled)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTranslateDBError_PercentWWrap_StillRecognized(t *testing.T) {
	// fmt.Errorf("...: %w", gorm.ErrRecordNotFound) 也应识别
	c, w := newTestCtx()
	wrapped := wrapErr(gorm.ErrRecordNotFound, "context")
	handled := TranslateDBError(c, wrapped)
	assert.True(t, handled)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// wrapErr 辅助：用 fmt.Errorf %w 包装（errors.Is 应能穿透）
func wrapErr(err error, ctx string) error {
	return fmtErrorf(ctx+": %w", err)
}

// ==================== 综合场景 ====================

func TestEndToEnd_NotFoundFlow(t *testing.T) {
	// 模拟 service 返 ErrRecordNotFound → handler 调 TranslateDBError
	// → 客户端收到 404 + 标准 JSON
	c, w := newTestCtx()
	if !TranslateDBError(c, gorm.ErrRecordNotFound) {
		// 理论上不会进 else 分支
		NotFound(c, "fallback")
	}
	require.Equal(t, http.StatusNotFound, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"code":"not_found"`)
	assert.Contains(t, body, `"message":"资源不存在"`)
}

func TestEndToEnd_InternalFlow_NoLeak(t *testing.T) {
	// 模拟 service 返通用错误 → handler 调 Internal 包装 → 5xx 不泄露原始
	c, w := newTestCtx()
	Internal(c, "操作失败", errors.New("stack: pq syntax error near FROM"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "操作失败")
	assert.NotContains(t, body, "syntax error")
	assert.NotContains(t, body, "FROM")
}
