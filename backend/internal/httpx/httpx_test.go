package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRecorder struct {
	reqs   int32
	sumDur float64
	status map[string]int
}

func (f *fakeRecorder) IncRequest(system, status string) {
	atomic.AddInt32(&f.reqs, 1)
	f.status[status]++
}
func (f *fakeRecorder) ObserveDuration(system string, s float64) { f.sumDur += s }

func TestDo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	rec := &fakeRecorder{status: map[string]int{}}
	c := New(DefaultConfig(srv.URL), "test", rec)
	body, status, err := c.Do(testCtx(), "GET", "/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 || string(body) != "ok" {
		t.Errorf("status=%d body=%s", status, body)
	}
	if rec.reqs != 1 {
		t.Errorf("metrics not called")
	}
}

func TestDo_Retry5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("finally"))
	}))
	defer srv.Close()

	cfg := DefaultConfig(srv.URL)
	cfg.MaxRetries = 3
	cfg.RetryBackoff = 10 * time.Millisecond
	rec := &fakeRecorder{status: map[string]int{}}
	c := New(cfg, "test", rec)

	body, status, err := c.Do(testCtx(), "GET", "/x", nil)
	if err != nil {
		t.Fatalf("should succeed on 3rd attempt: %v", err)
	}
	if status != 200 || string(body) != "finally" {
		t.Errorf("status=%d body=%s", status, body)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDo_NoRetry4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(404)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	cfg := DefaultConfig(srv.URL)
	cfg.MaxRetries = 3
	c := New(cfg, "test", &fakeRecorder{status: map[string]int{}})

	_, status, err := c.Do(testCtx(), "GET", "/x", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if status != 404 {
		t.Errorf("status=%d", status)
	}
	if calls != 1 {
		t.Errorf("4xx should not retry, got %d calls", calls)
	}
}

func TestDo_AuthHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := DefaultConfig(srv.URL)
	cfg.HeaderName = "Authorization"
	cfg.HeaderValue = "Token secret123"
	c := New(cfg, "test", &fakeRecorder{status: map[string]int{}})

	_, _, _ = c.Do(testCtx(), "GET", "/x", nil)
	if got != "Token secret123" {
		t.Errorf("auth header not set: %q", got)
	}
}

func TestCircuitBreaker_OpensAfterThresh(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	cfg := DefaultConfig(srv.URL)
	cfg.MaxRetries = 0
	cfg.BreakerThresh = 2
	cfg.BreakerCool = 50 * time.Millisecond
	c := New(cfg, "test", &fakeRecorder{status: map[string]int{}})

	// 2 次失败触发熔断
	for i := 0; i < 2; i++ {
		_, _, _ = c.Do(testCtx(), "GET", "/x", nil)
	}
	// 第 3 次应被熔断挡掉
	_, _, err := c.Do(testCtx(), "GET", "/x", nil)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen, got: %v", err)
	}
	if atomic.LoadInt32(&calls) > 2 {
		t.Errorf("circuit should have stopped calls, got %d", calls)
	}
	// 等 cooldown 后状态转 half-open
	time.Sleep(60 * time.Millisecond)
	if c.State() != "half_open" {
		t.Errorf("state should be half_open, got %s", c.State())
	}
}

func TestDo_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := New(DefaultConfig(srv.URL), "test", &fakeRecorder{status: map[string]int{}})
	ctx, cancel := contextWithTimeout(50 * time.Millisecond)
	defer cancel()

	_, _, err := c.Do(ctx, "GET", "/x", nil)
	if err == nil || !strings.Contains(err.Error(), "ctx") {
		t.Errorf("expected ctx error, got: %v", err)
	}
}

// --- helpers ---

func testCtx() context.Context { return context.Background() }
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
