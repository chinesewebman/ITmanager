package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordsText 把 countingHandler 收集到的记录拼成可搜索的文本。
func recordsText(h *countingHandler) string {
	var sb strings.Builder
	for _, r := range h.records {
		sb.WriteString(r.Message)
		r.Attrs(func(a slog.Attr) bool {
			sb.WriteString(" " + a.Key + "=" + a.Value.String())
			return true
		})
		sb.WriteString("\n")
	}
	return sb.String()
}

// TestRecovery_不把请求头写进日志 钉住 G-16：gin 的 Recovery 会 dump 整个请求头
// 且只屏蔽 Authorization —— Cookie 里的 auth_token（JWT）会明文落盘，拿到即可重放。
// 注意 dump 发生在 gin.CustomRecoveryWithWriter **内部**、调用自定义 handler 之前，
// 所以这里同时盯住 gin.DefaultErrorWriter：换回 gin.CustomRecovery 也会被抓到。
func TestRecovery_不把请求头写进日志(t *testing.T) {
	gin.SetMode(gin.DebugMode) // 只有 debug 模式 gin 才 dump 请求头（recovery.go:91-96）
	t.Cleanup(func() { gin.SetMode(gin.TestMode) })

	var h countingHandler
	old := slog.Default()
	slog.SetDefault(slog.New(&h))
	t.Cleanup(func() { slog.SetDefault(old) })

	var dump bytes.Buffer
	oldW := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &dump
	t.Cleanup(func() { gin.DefaultErrorWriter = oldW })

	r := gin.New()
	r.Use(Recovery())
	r.GET("/boom", func(c *gin.Context) { panic("kaboom") })

	const jwt = "eyJhbGciOiJIUzI1NiJ9.SECRET-JWT-VALUE"
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	req.Header.Set("Cookie", "auth_token="+jwt)
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("X-Request-ID", "req-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code, "panic 必须转成 500")
	require.Equal(t, 1, h.count(slog.LevelError), "panic 必须恰好记一条 ERROR")

	out := recordsText(&h)
	assert.Contains(t, out, "kaboom", "必须留下 panic 值，否则排障没有线索")
	assert.Contains(t, out, "req-1", "必须留下 request_id 以便关联")
	assert.NotContains(t, out, jwt, "会话令牌不得进日志")
	assert.NotContains(t, out, "auth_token", "gin 默认 Recovery 会 dump 整个 Cookie 头，这里连 cookie 名都不该出现")
	assert.NotContains(t, out, "Authorization")

	assert.Empty(t, dump.String(),
		"gin.DefaultErrorWriter 必须一个字都没有：dump 发生在 CustomRecoveryWithWriter 内部，"+
			"用 gin.CustomRecovery 也挡不住，必须完全手写 recover")
}
