package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"network-monitor-platform/internal/config"
)

// TestZabbixE2E_AuthTTLExpires_TriggerRelogin v1.1 P2-B-2: 验证 expiresAt
// 过期前 60s 自动重登 — 模拟首次 GetTriggers 设一个已过期的 expiresAt。
func TestZabbixE2E_AuthTTLExpires_TriggerRelogin(t *testing.T) {
	var loginCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		method := ""
		// 简单解析 method 字段
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body[:n], &req)
		method = req.Method

		w.Header().Set("Content-Type", "application/json")
		if method == "user.login" {
			atomic.AddInt32(&loginCount, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "result": "fake-auth-token-2", "id": 1,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"result":  []map[string]interface{}{},
			"id":      2,
		})
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.ZabbixConfig{URL: srv.URL, User: "u", Password: "p"}
	c := NewZabbixClient(cfg, rec)

	// 1) 第一次调用 — auth 空 → Login
	_, err := c.GetTriggers(context.Background())
	if err != nil {
		t.Fatalf("first GetTriggers: %v", err)
	}
	if got := atomic.LoadInt32(&loginCount); got != 1 {
		t.Errorf("expected 1 login, got %d", got)
	}

	// 2) 手动把 expiresAt 拨到过去 1s — 模拟过期
	c.mu.Lock()
	c.expiresAt = time.Now().Add(-1 * time.Second)
	c.mu.Unlock()

	// 3) 第二次调用 — expiresAt - 60s 已在过去 → 自动 Login
	_, err = c.GetTriggers(context.Background())
	if err != nil {
		t.Fatalf("second GetTriggers: %v", err)
	}
	if got := atomic.LoadInt32(&loginCount); got != 2 {
		t.Errorf("expected 2 logins (one fresh + one re-login), got %d", got)
	}
}

// TestZabbixE2E_SessionExpired_10002_AutoRelogin v1.1 P2-B-2: 验证
// Zabbix 返回 -10002 (Session terminated) 时自动重登一次再重试。
func TestZabbixE2E_SessionExpired_10002_AutoRelogin(t *testing.T) {
	var callIndex int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body[:n], &req)

		w.Header().Set("Content-Type", "application/json")
		idx := atomic.AddInt32(&callIndex, 1)

		switch {
		case req.Method == "user.login":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "result": "fresh-auth-" + string(rune('0'+idx)), "id": 1,
			})
		case idx == 2: // 第一次 trigger.get → -10002
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"error":   map[string]interface{}{"code": -10002, "message": "Session terminated, re-login."},
				"id":      2,
			})
		default: // 第二次 trigger.get（重试）→ 成功
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "result": []map[string]interface{}{}, "id": 2,
			})
		}
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.ZabbixConfig{URL: srv.URL, User: "u", Password: "p"}
	c := NewZabbixClient(cfg, rec)

	// Login first (call #1)
	if err := c.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	// GetTriggers → first try -10002 → auto re-login → retry → ok
	triggers, err := c.GetTriggers(context.Background())
	if err != nil {
		t.Fatalf("GetTriggers should recover from -10002, got: %v", err)
	}
	if triggers == nil {
		t.Error("expected non-nil triggers after auto re-login")
	}
}
