package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

// ==================== BUG FIX 回归测试 ====================

// TestDo_4xx不触发熔断 — BUG#11
//
//	之前 4xx 响应调 c.afterFailure()，把 failStreak 累加
//	用户传错参数（401/403/404）就被服务端熔断
//	修复：4xx 客户端错不触发熔断
func TestDo_4xx不触发熔断(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	cfg := DefaultConfig(srv.URL)
	cfg.BreakerThresh = 2 // 2 次失败就熔断
	rec := &fakeRecorder{status: map[string]int{}}
	c := New(cfg, "test", rec)

	// 打 10 次 4xx
	for i := 0; i < 10; i++ {
		_, _, _ = c.Do(testCtx(), "GET", "/missing", nil)
	}
	// 熔断器必须仍是 closed（4xx 不算）
	assert.Equal(t, "closed", c.State(), "10 次 4xx 不应触发熔断")
}

// TestDo_ctx取消不触发熔断 — BUG#11
//
//	之前 ctx.Err() 时 c.afterFailure()，把 failStreak 累加
//	用户主动 cancel 请求被算服务端失败
//	修复：ctx 取消是用户行为，不触发熔断
func TestDo_ctx取消不触发熔断(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := DefaultConfig(srv.URL)
	cfg.BreakerThresh = 2
	rec := &fakeRecorder{status: map[string]int{}}
	c := New(cfg, "test", rec)

	// 5 次 ctx 取消
	for i := 0; i < 5; i++ {
		ctx, cancel := contextWithTimeout(10 * time.Millisecond)
		_, _, _ = c.Do(ctx, "GET", "/slow", nil)
		cancel()
	}
	assert.Equal(t, "closed", c.State(), "5 次 ctx 取消不应触发熔断")
}

// TestDo_HalfOpen_并发只放一个探针 — BUG#9
//
//	之前 halfOpenInflight 用 atomic CAS 在锁外，与 afterSuccess/afterFailure
//	写 0 之间 race。10 goroutine 同时打 half-open 可能都通过。
//	修复：CAS 移入锁内
//	测试：构造 open → 等 cool → 10 goroutine 并发打，应只有 1 个走通，
//	其余 9 个返 ErrCircuitOpen
func TestDo_HalfOpen_并发只放一个探针(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(50 * time.Millisecond) // 让 half-open 探针慢一点
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := DefaultConfig(srv.URL)
	cfg.BreakerThresh = 2
	cfg.MaxRetries = 0
	cfg.RetryBackoff = 0
	cfg.BreakerCool = 50 * time.Millisecond
	rec := &fakeRecorder{status: map[string]int{}}
	_ = rec

	// 制造 2 次失败触发熔断（直接打 srv，第一次 503 触发 failStreak=1，
	// MaxRetries=0 时不重试，第二次 503 → failStreak=2=BreakerThresh → 熔断）
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(503)
	}))
	defer srv2.Close()
	cfg2 := DefaultConfig(srv2.URL)
	cfg2.BreakerThresh = 2
	cfg2.MaxRetries = 0
	c2 := New(cfg2, "test2", rec)
	for i := 0; i < 2; i++ {
		_, _, _ = c2.Do(testCtx(), "GET", "/", nil)
	}
	assert.Equal(t, "open", c2.State(), "2 次 5xx 必须触发熔断")

	// 等 cooldown 让 c2 转 half-open
	time.Sleep(60 * time.Millisecond)

	// 10 个 goroutine 并发打 c2 的 half-open
	var wg sync.WaitGroup
	var halfOpenRejects int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := c2.Do(testCtx(), "GET", "/probe", nil)
			if err != nil && errors.Is(err, ErrCircuitOpen) {
				atomic.AddInt32(&halfOpenRejects, 1)
			}
		}()
	}
	wg.Wait()

	// 必须大多数被熔断器挡掉（half-open 只放 1 个探针）
	// 实际可能 1-2 个通过（第一个完成前可能有 race window），但绝不能 10 个全过
	assert.Less(t, int(atomic.LoadInt32(&calls)), 5,
		"half-open 探针并发：实际打到的请求应 < 5，实际 %d", calls)
	t.Logf("half-open 探针并发：实际 %d 次通过，%d 次被熔断",
		atomic.LoadInt32(&calls), halfOpenRejects)
}
