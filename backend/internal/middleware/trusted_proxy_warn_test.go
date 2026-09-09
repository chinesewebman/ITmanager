package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// serveWithXFF 起一个挂载了告警中间件的 router 并打一次带 XFF 的请求。
func serveWithXFF(t *testing.T, trusted []string, remoteAddr string) {
	t.Helper()
	r := gin.New()
	r.Use(WarnUntrustedForwardedFor(trusted))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "告警中间件不得影响主流程")
}

// countingHandler 收集 slog 记录，用来断言「真的只打了一条日志」。
// 只断言 flag 置位是不够的：变异成「每次都打日志但仍置位」照样能过。
type countingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r)
	h.mu.Unlock()
	return nil
}

func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

func (h *countingHandler) count(level slog.Level) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level == level {
			n++
		}
	}
	return n
}

// TestWarnUntrustedForwardedFor_只告警一次 钉住「进程内一次性」语义：
// 告警由真实流量触发，如果每次都打日志，配错的生产环境会被日志淹没。
func TestWarnUntrustedForwardedFor_只告警一次(t *testing.T) {
	resetUntrustedForwardedForWarn()
	t.Cleanup(resetUntrustedForwardedForWarn)

	var h countingHandler
	old := slog.Default()
	slog.SetDefault(slog.New(&h))
	t.Cleanup(func() { slog.SetDefault(old) })

	for i := 0; i < 3; i++ {
		serveWithXFF(t, nil, "")
	}
	assert.True(t, forwardedForWarned.Load(), "见过未受信来源的 XFF 后标记必须置位（否则会重复告警）")
	assert.Equal(t, 1, h.count(slog.LevelWarn), "3 次请求只允许 1 条 WARN")
}

// TestWarnUntrustedForwardedFor_无XFF不告警：直连部署（无代理）不应产生噪声。
func TestWarnUntrustedForwardedFor_无XFF不告警(t *testing.T) {
	resetUntrustedForwardedForWarn()
	t.Cleanup(resetUntrustedForwardedForWarn)

	r := gin.New()
	r.Use(WarnUntrustedForwardedFor(nil))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.False(t, forwardedForWarned.Load(), "没有 XFF 就不该告警")
}

// TestWarnUntrustedForwardedFor_受信对端不告警：正常拓扑（nginx 是受信代理）不该刷噪声。
func TestWarnUntrustedForwardedFor_受信对端不告警(t *testing.T) {
	resetUntrustedForwardedForWarn()
	t.Cleanup(resetUntrustedForwardedForWarn)

	serveWithXFF(t, []string{"172.28.0.10"}, "172.28.0.10:5000")

	assert.False(t, forwardedForWarned.Load(), "对端在受信表内，XFF 会被采信，无需告警")
}

// TestWarnUntrustedForwardedFor_配了但配错仍告警 — 安全审计建议 2/5。
//
// 「配了但配错」比「没配」更危险：非空配置没有启动期告警，故障完全静默。
// 用「对端是否在表内」判定才能覆盖：配错 IP、多跳漏配、代理换 IP、LB 直连容器。
func TestWarnUntrustedForwardedFor_配了但配错仍告警(t *testing.T) {
	resetUntrustedForwardedForWarn()
	t.Cleanup(resetUntrustedForwardedForWarn)

	serveWithXFF(t, []string{"172.28.0.10"}, "192.0.2.1:5000")

	assert.True(t, forwardedForWarned.Load(), "表非空但对端不在表内，必须告警")
}

// TestWarnUntrustedForwardedFor_受信表支持裸IP与CIDR：两种写法都要能识别对端。
func TestWarnUntrustedForwardedFor_受信表支持裸IP与CIDR(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trusted []string
		peer    string
	}{
		{"裸IPv4", []string{"203.0.113.7"}, "203.0.113.7:5000"},
		{"CIDR", []string{"203.0.113.0/24"}, "203.0.113.9:5000"},
		{"裸IPv6", []string{"::1"}, "[::1]:5000"},
		{"RemoteAddr无端口", []string{"203.0.113.7"}, "203.0.113.7"},
		{"表内混有非法条目", []string{"bogus", "300.1.1.1/24", "203.0.113.0/24"}, "203.0.113.9:5000"},
		{"表内混有空条目", []string{"  ", "203.0.113.0/24"}, "203.0.113.9:5000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetUntrustedForwardedForWarn()
			t.Cleanup(resetUntrustedForwardedForWarn)
			serveWithXFF(t, tc.trusted, tc.peer)
			assert.False(t, forwardedForWarned.Load(), "对端 %s 应被识别为受信", tc.peer)
		})
	}
}

// TestWarnUntrustedForwardedFor_对端无法解析时告警：RemoteAddr 异常（如空串）时
// 不能误判为受信，宁可告警。
func TestWarnUntrustedForwardedFor_对端无法解析时告警(t *testing.T) {
	resetUntrustedForwardedForWarn()
	t.Cleanup(resetUntrustedForwardedForWarn)

	serveWithXFF(t, []string{"203.0.113.0/24"}, "not-an-ip")

	assert.True(t, forwardedForWarned.Load(), "对端无法解析 → 不在受信表内 → 应告警")
}
