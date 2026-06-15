package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"network-monitor-platform/internal/config"
)

// TestZabbixE2E_HappyPath 完整流程：Login → GetTriggers（带缓存复用）。
// 验证：JSON-RPC 协议 + 二次调用复用 auth token。
func TestZabbixE2E_HappyPath(t *testing.T) {
	var calls int32
	var authCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		// 读 body 判 method
		body := make([]byte, 4096)
		n2, _ := r.Body.Read(body)
		bodyStr := string(body[:n2])

		// 验证 Content-Type
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}

		if strings.Contains(bodyStr, `"user.login"`) {
			atomic.AddInt32(&authCount, 1)
			// 登录响应
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"abc123token","id":1}`))
			return
		}
		if strings.Contains(bodyStr, `"trigger.get"`) {
			// 验证 auth 字段
			if !strings.Contains(bodyStr, `"auth":"abc123token"`) {
				t.Errorf("trigger.get missing auth field: %s", bodyStr)
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[
				{"triggerid":"100","description":"CPU > 90%","priority":5,"hosts":[{"hostid":"1","host":"web-01"}],"value":"1"},
				{"triggerid":"101","description":"Disk full","priority":3,"hosts":[{"hostid":"2","host":"db-01"}],"value":"1"}
			],"id":2}`))
			return
		}
		// 兜底
		_ = n
		t.Errorf("unexpected request: %s", bodyStr)
		w.WriteHeader(400)
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.ZabbixConfig{URL: srv.URL, User: "admin", Password: "pass"}
	c := NewZabbixClient(cfg, rec)

	// 第一次：会触发 login
	triggers, err := c.GetTriggers(context.Background())
	if err != nil {
		t.Fatalf("GetTriggers: %v", err)
	}
	if len(triggers) != 2 {
		t.Errorf("got %d triggers, want 2", len(triggers))
	}
	if triggers[0].TriggerID != "100" {
		t.Errorf("first trigger id = %q, want 100", triggers[0].TriggerID)
	}

	// 第二次：复用 auth token（不应再 login）
	_, err = c.GetTriggers(context.Background())
	if err != nil {
		t.Fatalf("GetTriggers #2: %v", err)
	}
	if atomic.LoadInt32(&authCount) != 1 {
		t.Errorf("expected 1 login call, got %d (auth not cached)", authCount)
	}
	if atomic.LoadInt32(&calls) != 3 {
		// 1 login + 2 trigger.get = 3 calls
		t.Errorf("expected 3 server calls, got %d", atomic.LoadInt32(&calls))
	}
}

// TestZabbixE2E_LoginFailure login 失败应返回错误。
func TestZabbixE2E_LoginFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Zabbix 5xx 触发重试
		w.WriteHeader(503)
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.ZabbixConfig{URL: srv.URL, User: "admin", Password: "wrong"}
	c := NewZabbixClient(cfg, rec)

	_, err := c.GetTriggers(context.Background())
	if err == nil {
		t.Fatal("expected error after 5xx retries")
	}
}

// TestZabbixE2E_APIError Zabbix 业务错（HTTP 200 + result.error）。
func TestZabbixE2E_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-1,"message":"Invalid credentials"},"id":1}`))
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.ZabbixConfig{URL: srv.URL, User: "admin", Password: "bad"}
	c := NewZabbixClient(cfg, rec)

	_, err := c.GetTriggers(context.Background())
	if err == nil {
		t.Fatal("expected Zabbix API error")
	}
	if !strings.Contains(err.Error(), "Invalid credentials") {
		t.Errorf("error should mention 'Invalid credentials': %v", err)
	}
}

// TestZabbixE2E_CtxCancel ctx 取消应立即停。
func TestZabbixE2E_CtxCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.ZabbixConfig{URL: srv.URL, User: "u", Password: "p"}
	c := NewZabbixClient(cfg, rec)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.GetTriggers(ctx)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected ctx error")
	}
	if elapsed > 1*time.Second {
		t.Errorf("ctx cancel should be fast, took %v", elapsed)
	}
}
