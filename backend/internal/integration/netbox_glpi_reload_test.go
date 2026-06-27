package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"network-monitor-platform/internal/config"
)

// TestNetBox_Reload_v2_2 v2.2: 改 URL/Token 后 → 下次 TestConnection 走到新地址。
func TestNetBox_Reload_v2_2(t *testing.T) {
	var oldHits, newHits int32
	oldSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&oldHits, 1)
		// 期望 Authorization 头含旧 token
		if r.Header.Get("Authorization") != "Token old-token" {
			t.Errorf("old server got wrong Authorization: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"count": 0, "results": []map[string]interface{}{}})
	}))
	defer oldSrv.Close()
	newSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&newHits, 1)
		// 期望新 token
		if r.Header.Get("Authorization") != "Token new-token" {
			t.Errorf("new server got wrong Authorization: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"count": 1, "results": []map[string]interface{}{}})
	}))
	defer newSrv.Close()

	rec := newRecorderE2E()
	cfg := &config.NetboxConfig{URL: oldSrv.URL, Token: "old-token"}
	c := NewNetBoxClient(cfg, rec)

	// 1) 第一次 → 打到 old server
	if err := c.TestConnection(context.Background()); err != nil {
		t.Fatalf("first TestConnection: %v", err)
	}
	if atomic.LoadInt32(&oldHits) != 1 {
		t.Fatalf("expected 1 hit on old, got %d", oldHits)
	}

	// 2) Reload 到 new
	c.Reload(&config.NetboxConfig{URL: newSrv.URL, Token: "new-token"})

	// 3) 第二次 → 必须打到 new
	if err := c.TestConnection(context.Background()); err != nil {
		t.Fatalf("second TestConnection: %v", err)
	}
	if atomic.LoadInt32(&newHits) != 1 {
		t.Errorf("expected 1 hit on new, got %d (old=%d)", newHits, oldHits)
	}
	if atomic.LoadInt32(&oldHits) != 1 {
		t.Errorf("old server should NOT be hit again, old=%d", oldHits)
	}
}

// TestGLPI_Reload_v2_2 v2.2: Reload 清 session + 换 token → 下次 InitSession 走新凭据。
func TestGLPI_Reload_v2_2(t *testing.T) {
	var oldInitHits, newInitHits int32
	var oldGotToken, newGotToken string
	oldSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/initSession" {
			atomic.AddInt32(&oldInitHits, 1)
			body := make([]byte, 4096)
			n, _ := r.Body.Read(body)
			var p struct {
				UserToken string `json:"user_token"`
			}
			_ = json.Unmarshal(body[:n], &p)
			oldGotToken = p.UserToken
			_ = json.NewEncoder(w).Encode(map[string]string{"session_token": "old-session"})
			return
		}
		w.WriteHeader(404)
	}))
	defer oldSrv.Close()
	newSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/initSession" {
			atomic.AddInt32(&newInitHits, 1)
			body := make([]byte, 4096)
			n, _ := r.Body.Read(body)
			var p struct {
				UserToken string `json:"user_token"`
			}
			_ = json.Unmarshal(body[:n], &p)
			newGotToken = p.UserToken
			_ = json.NewEncoder(w).Encode(map[string]string{"session_token": "new-session"})
			return
		}
		w.WriteHeader(404)
	}))
	defer newSrv.Close()

	rec := newRecorderE2E()
	cfg := &config.GLPIConfig{URL: oldSrv.URL, AppToken: "old-app", UserToken: "old-user"}
	c := NewGLPIClient(cfg, rec)

	// 1) 第一次 InitSession → old server
	if err := c.InitSession(context.Background()); err != nil {
		t.Fatalf("first InitSession: %v", err)
	}
	if atomic.LoadInt32(&oldInitHits) != 1 {
		t.Fatalf("expected 1 hit on old, got %d", oldInitHits)
	}
	if oldGotToken != "old-user" {
		t.Fatalf("old got token %q, want old-user", oldGotToken)
	}

	// 2) Reload → 清 session + 换 token
	c.Reload(&config.GLPIConfig{URL: newSrv.URL, AppToken: "new-app", UserToken: "new-user"})

	// 3) session 必须清空
	c.mu.Lock()
	sessAfter := c.session
	c.mu.Unlock()
	if sessAfter != "" {
		t.Errorf("session not cleared after Reload: %q", sessAfter)
	}

	// 4) 第二次 InitSession → 必须打 new server
	if err := c.InitSession(context.Background()); err != nil {
		t.Fatalf("second InitSession: %v", err)
	}
	if atomic.LoadInt32(&newInitHits) != 1 {
		t.Errorf("expected 1 hit on new, got %d", newInitHits)
	}
	if newGotToken != "new-user" {
		t.Errorf("new got token %q, want new-user", newGotToken)
	}
	if atomic.LoadInt32(&oldInitHits) != 1 {
		t.Errorf("old server should NOT be hit again, old=%d", oldInitHits)
	}
}
