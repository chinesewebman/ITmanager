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

// TestZabbix_Reload_v2_2 v2.2: UI 改完配置点保存 → Reload 清缓存 → 下次 GetTriggers 重新 Login。
// 验证：①auth 被清空；②新 URL 生效；③user/password 替换。
func TestZabbix_Reload_v2_2(t *testing.T) {
	var loginCount int32
	var lastUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body[:n], &req)

		w.Header().Set("Content-Type", "application/json")
		if req.Method == "user.login" {
			atomic.AddInt32(&loginCount, 1)
			var p struct {
				User string `json:"username"`
			}
			_ = json.Unmarshal(req.Params, &p)
			lastUser = p.User
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "result": "auth-after-reload", "id": 1,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0", "result": []map[string]interface{}{}, "id": 2,
		})
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.ZabbixConfig{URL: srv.URL, User: "old-user", Password: "old-pass"}
	c := NewZabbixClient(cfg, rec)

	// 1) 第一次 GetTriggers → 触发 Login（old-user）
	if _, err := c.GetTriggers(context.Background()); err != nil {
		t.Fatalf("first GetTriggers: %v", err)
	}
	if got := atomic.LoadInt32(&loginCount); got != 1 {
		t.Fatalf("expected 1 login, got %d", got)
	}
	if lastUser != "old-user" {
		t.Fatalf("first login user = %q, want old-user", lastUser)
	}

	// 2) Reload 到新用户
	c.Reload(&config.ZabbixConfig{URL: srv.URL, User: "new-user", Password: "new-pass"})

	// 3) auth 应被清空（ExpiresAt 也被重置）
	c.mu.Lock()
	authAfter := c.auth
	expAfter := c.expiresAt
	c.mu.Unlock()
	if authAfter != "" {
		t.Errorf("auth not cleared after Reload: %q", authAfter)
	}
	if !expAfter.IsZero() {
		t.Errorf("expiresAt not reset after Reload: %v", expAfter)
	}

	// 4) 第二次 GetTriggers → 必然重新 Login（new-user）
	if _, err := c.GetTriggers(context.Background()); err != nil {
		t.Fatalf("second GetTriggers: %v", err)
	}
	if got := atomic.LoadInt32(&loginCount); got != 2 {
		t.Errorf("expected 2 logins after Reload, got %d", got)
	}
	if lastUser != "new-user" {
		t.Errorf("login after reload user = %q, want new-user", lastUser)
	}
}

// TestZabbix_Reload_PreservesExpiresCheck v2.2: 验证 Reload 后 expiresAt 窗口重置，
// 不会因为新 URL 复用旧 session（哪怕 token 字符串相同也视为新上下文）。
func TestZabbix_Reload_PreservesExpiresCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"x","id":1}`))
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	c := NewZabbixClient(&config.ZabbixConfig{URL: srv.URL, User: "u", Password: "p"}, rec)
	c.auth = "stale-token"
	c.expiresAt = time.Now().Add(1 * time.Hour) // 假装还有效

	c.Reload(&config.ZabbixConfig{URL: srv.URL, User: "u2", Password: "p2"})

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.auth != "" {
		t.Errorf("stale auth not cleared: %q", c.auth)
	}
	if !c.expiresAt.IsZero() {
		t.Errorf("stale expiresAt not reset: %v", c.expiresAt)
	}
	if c.user != "u2" || c.password != "p2" {
		t.Errorf("user/password not updated: u=%q p=%q", c.user, c.password)
	}
}
