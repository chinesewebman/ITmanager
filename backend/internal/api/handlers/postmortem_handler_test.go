package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "asset"},
		{"web-server-01", "web-server-01"},
		{"web server 01", "web_server_01"},
		{"../../etc/passwd", "etcpasswd"},
		{"中文资产", "asset"},
		{string(bytes.Repeat([]byte{'a'}, 100)), string(bytes.Repeat([]byte{'a'}, 50))},
	}
	for _, c := range cases {
		got := sanitizeFilename(c.in)
		if got != c.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPostmortemHandler_DownloadReport_缺ID格式(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// mock service（这里用 nil，不走 service 路径因为 ID 格式检查在 handler 层）
	h := &PostmortemHandler{svc: nil}
	r.GET("/api/v1/postmortem/assets/:id/report", h.DownloadReport)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/postmortem/assets/not-a-uuid/report", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "资产 ID 格式错误")
}

// smoke test: 验证 handler 能装入 router 不 panic
func TestPostmortemHandler_构造(t *testing.T) {
	svc := service.NewPostmortemService(nil, nil)
	h := NewPostmortemHandler(svc)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}
