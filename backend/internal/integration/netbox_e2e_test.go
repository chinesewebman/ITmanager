package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/httpx"
)

// recorderE2E 真实用 mutex 保护的 MetricsRecorder
type recorderE2E struct {
	mu   sync.Mutex
	reqs map[string]int
	durs []float64
}

func newRecorderE2E() *recorderE2E {
	return &recorderE2E{reqs: map[string]int{}}
}
func (r *recorderE2E) IncRequest(sys, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs[sys+"|"+status]++
}
func (r *recorderE2E) ObserveDuration(sys string, s float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.durs = append(r.durs, s)
}
func (r *recorderE2E) Count(sys, status string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reqs[sys+"|"+status]
}

// TestNetBoxE2E_HappyPath 真 mock server + 真 httpx client + 真 NewNetBoxClient。
// 验证：HTTP 200 + JSON 解析 + ctx 透传 + metrics 计数。
func TestNetBoxE2E_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证 ctx 透传：超时 cancel 应取消
		if r.Context().Err() != nil {
			t.Errorf("ctx already cancelled: %v", r.Context().Err())
		}
		// 验证 auth header
		if got := r.Header.Get("Authorization"); got != "Token secret-token" {
			t.Errorf("auth header = %q, want %q", got, "Token secret-token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"count": 2,
			"results": [
				{"id": 1, "name": "sw01", "device_type": {"slug": "cisco", "model": "Catalyst"}, "device_role": {"slug": "switch", "name": "Switch"}, "site": {"id": 1, "slug": "dc1", "name": "DC1"}, "serial_number": "SN001"},
				{"id": 2, "name": "srv01", "device_type": {"slug": "dell", "model": "PowerEdge"}, "device_role": {"slug": "server", "name": "Server"}, "site": {"id": 1, "slug": "dc1", "name": "DC1"}, "serial_number": "SN002"}
			]
		}`))
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.NetboxConfig{URL: srv.URL, Token: "secret-token"}
	c := NewNetBoxClient(cfg, rec)
	devices, err := c.SyncDevices(context.Background())
	if err != nil {
		t.Fatalf("SyncDevices: %v", err)
	}
	if len(devices) != 2 {
		t.Errorf("got %d devices, want 2", len(devices))
	}
	if devices[0].Name != "sw01" {
		t.Errorf("first device name = %q, want sw01", devices[0].Name)
	}
	// 验证 metrics
	if rec.Count("netbox", "200") != 1 {
		t.Errorf("metrics count for 200 = %d, want 1", rec.Count("netbox", "200"))
	}
}

// TestNetBoxE2E_5xxRetry 真 503 → 真 httpx 重试 2 次后成功（验证重试真生效）。
func TestNetBoxE2E_5xxRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"count": 0, "results": []}`))
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.NetboxConfig{URL: srv.URL, Token: "t"}
	c := NewNetBoxClient(cfg, rec)
	// 加快重试速度：httpx 默认 500ms backoff，我们走 MaxRetries=2 共 3 次 ≈ 1.5s
	// 改 httpx 默认 backoff：直接修改 config 不行（httpx 写死），所以接受 1.5s 等待
	devices, err := c.SyncDevices(context.Background())
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("expected 0 devices, got %d", len(devices))
	}
	if calls != 3 {
		t.Errorf("server called %d times, want 3 (2 fail + 1 success)", calls)
	}
	// 验证 metrics：2 次 503 + 1 次 200
	if rec.Count("netbox", "503") != 2 {
		t.Errorf("expected 2 x 503 metrics, got %d", rec.Count("netbox", "503"))
	}
	if rec.Count("netbox", "200") != 1 {
		t.Errorf("expected 1 x 200 metrics, got %d", rec.Count("netbox", "200"))
	}
}

// TestNetBoxE2E_4xxNoRetry 404 不应重试（业务错）。
func TestNetBoxE2E_4xxNoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"detail":"not found"}`))
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.NetboxConfig{URL: srv.URL, Token: "t"}
	c := NewNetBoxClient(cfg, rec)
	_, _, err := c.c.Do(context.Background(), "GET", "/api/dcim/devices/", nil)
	if err == nil {
		t.Fatal("expected 4xx error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("4xx should not retry, got %d calls", calls)
	}
}

// TestNetBoxE2E_CtxCancel ctx 取消应立即停止。
func TestNetBoxE2E_CtxCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟慢响应，等 ctx 取消
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.NetboxConfig{URL: srv.URL, Token: "t"}
	c := NewNetBoxClient(cfg, rec)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.SyncDevices(ctx)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected ctx error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("ctx cancel should be fast, took %v", elapsed)
	}
}

// TestNetBoxE2E_BreakerOpens 连续 5xx 触发熔断。
func TestNetBoxE2E_BreakerOpens(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	// 走自定义 httpx.Client 直接（绕开 client 默认 30s timeout）
	hcfg := httpx.DefaultConfig(srv.URL)
	hcfg.HeaderName = "Authorization"
	hcfg.HeaderValue = "Token t"
	hcfg.BreakerThresh = 3 // 快速触发
	hcfg.MaxRetries = 0    // 不重试，测纯熔断
	c := &NetBoxClient{c: httpx.New(hcfg, "netbox", rec)}

	// 3 次失败应触发熔断
	for i := 0; i < 3; i++ {
		_, _, _ = c.c.Do(context.Background(), "GET", "/x", nil)
	}
	// 第 4 次应被熔断挡掉
	_, _, err := c.c.Do(context.Background(), "GET", "/x", nil)
	if err == nil || err.Error() != "httpx: circuit breaker open" {
		t.Errorf("expected ErrCircuitOpen, got: %v", err)
	}
	if atomic.LoadInt32(&calls) > 3 {
		t.Errorf("circuit should block calls after 3, got %d", calls)
	}
}
