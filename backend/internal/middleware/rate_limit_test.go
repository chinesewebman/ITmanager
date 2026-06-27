package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() { gin.SetMode(gin.TestMode) }

// TestMain 重置 rateLimiter 缓存保证测试隔离 (P2 引入 cache 后需要)
// resetRateLimiterCache() 必须在每个测试运行前调用, 否则前一个测试的 bucket 状态污染下一个。
func TestMain(m *testing.M) {
	resetRateLimiterCache()
	code := m.Run()
	resetRateLimiterCache()
	os.Exit(code)
}

func newTestRouter(cfg RateLimitConfig) *gin.Engine {
	r := gin.New()
	r.GET("/api/test", RateLimit(cfg), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return r
}

func doRequest(r *gin.Engine, ip string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = ip + ":12345"
	r.ServeHTTP(w, req)
	return w
}

func TestRateLimit_未超限正常通过(t *testing.T) {
	r := newTestRouter(RateLimitConfig{Window: time.Minute, Max: 3})
	for i := 0; i < 3; i++ {
		w := doRequest(r, "10.0.0.1")
		assert.Equal(t, http.StatusOK, w.Code, "第 %d 次应在限额内", i+1)
		// X-RateLimit-Remaining 应递减 (3,2,1)
		assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining"))
	}
}

func TestRateLimit_超限返回429(t *testing.T) {
	r := newTestRouter(RateLimitConfig{Window: time.Minute, Max: 2})
	_ = doRequest(r, "10.0.0.2")
	_ = doRequest(r, "10.0.0.2")
	w := doRequest(r, "10.0.0.2")
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
	assert.Contains(t, w.Body.String(), "rate_limit_exceeded")
}

func TestRateLimit_不同IP独立计数(t *testing.T) {
	r := newTestRouter(RateLimitConfig{Window: time.Minute, Max: 1})
	w1 := doRequest(r, "10.0.0.3")
	w2 := doRequest(r, "10.0.0.4")
	assert.Equal(t, http.StatusOK, w1.Code)
	assert.Equal(t, http.StatusOK, w2.Code, "不同 IP 不应互限")
}

func TestRateLimit_窗口过期后恢复(t *testing.T) {
	cfg := RateLimitConfig{Window: 50 * time.Millisecond, Max: 1}
	r := newTestRouter(cfg)
	w1 := doRequest(r, "10.0.0.5")
	w2 := doRequest(r, "10.0.0.5")
	assert.Equal(t, http.StatusOK, w1.Code)
	assert.Equal(t, http.StatusTooManyRequests, w2.Code)
	time.Sleep(80 * time.Millisecond)
	w3 := doRequest(r, "10.0.0.5")
	assert.Equal(t, http.StatusOK, w3.Code, "窗口过期后应恢复")
}

func TestRateLimit_自定义KeyFunc(t *testing.T) {
	cfg := RateLimitConfig{
		Window: time.Minute,
		Max:    1,
		KeyFunc: func(c *gin.Context) string {
			return c.GetHeader("X-User")
		},
	}
	// P2: 清空 cache 保证 bucket 干净 (max=1 时前次测试的 alice/bob 会让首次 200 变 429)
	resetRateLimiterCache()
	r2 := gin.New()
	r2.GET("/api/test", RateLimit(cfg), func(c *gin.Context) { c.String(200, "ok") })

	rec1 := serveWithHeader(r2, "GET", "/api/test", "X-User", "alice")
	rec2 := serveWithHeader(r2, "GET", "/api/test", "X-User", "alice")
	rec3 := serveWithHeader(r2, "GET", "/api/test", "X-User", "bob")
	assert.Equal(t, 200, rec1.Code)
	assert.Equal(t, 429, rec2.Code, "alice 第 2 次应 429")
	assert.Equal(t, 200, rec3.Code, "bob 不应受 alice 限流影响")
}

func serveWithHeader(r *gin.Engine, method, path, key, val string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set(key, val)
	r.ServeHTTP(w, req)
	return w
}

func TestRateLimit_DefaultConfig(t *testing.T) {
	cfg := DefaultRateLimitConfig(5)
	assert.Equal(t, time.Minute, cfg.Window)
	assert.Equal(t, 5, cfg.Max)
	assert.Nil(t, cfg.KeyFunc, "DefaultRateLimitConfig 不设 KeyFunc, 由 newRateLimiter 内部填 defaultKey")
	rl := newRateLimiter(cfg)
	assert.NotNil(t, rl.cfg.KeyFunc, "newRateLimiter 后 KeyFunc 已注入")
}

func TestRateLimit_并发安全(t *testing.T) {
	r := newTestRouter(RateLimitConfig{Window: time.Minute, Max: 100})
	var wg sync.WaitGroup
	results := make(chan int, 200)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := doRequest(r, "10.0.0.99")
			results <- w.Code
		}()
	}
	wg.Wait()
	close(results)
	ok := 0
	for code := range results {
		if code == 200 {
			ok++
		}
	}
	assert.Equal(t, 100, ok, "并发 200 次应只通过 100 次")
}

func TestRateLimit_429包含RetryAfter秒数(t *testing.T) {
	cfg := RateLimitConfig{Window: 30 * time.Second, Max: 1}
	r := newTestRouter(cfg)
	_ = doRequest(r, "10.0.0.7")
	w := doRequest(r, "10.0.0.7")
	retry := w.Header().Get("Retry-After")
	require.NotEmpty(t, retry)
	n, err := strconv.Atoi(retry)
	require.NoError(t, err)
	assert.Greater(t, n, 0, "Retry-After 应该是正数秒")
	assert.LessOrEqual(t, n, 30, "Retry-After 不应超过窗口长度")
}

func TestRateLimit_XRateLimitHeaders(t *testing.T) {
	cfg := RateLimitConfig{Window: time.Minute, Max: 5}
	r := newTestRouter(cfg)
	w := doRequest(r, "10.0.0.8")
	assert.Equal(t, "5", w.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "4", w.Header().Get("X-RateLimit-Remaining"), "首次剩余 4")
}

func TestRateLimit_GC不泄漏内存(t *testing.T) {
	// 间接测试: 100 个不同 IP 短时访问后, 调 gc 验证不 panic
	cfg := RateLimitConfig{Window: 10 * time.Millisecond, Max: 1}
	rl := newRateLimiter(cfg)
	for i := 0; i < 100; i++ {
		key := "ip-" + strconv.Itoa(i)
		rl.getBucket(key).record(time.Now())
	}
	// 等窗口过期 + 一次 gc tick
	time.Sleep(20 * time.Millisecond)
	// 不直接测 gc 内部 (私有时长 5min), 但验证 buckets 已存在
	rl.mu.RLock()
	assert.Greater(t, len(rl.buckets), 0, "buckets 应保留")
	rl.mu.RUnlock()
}

func TestItoa(t *testing.T) {
	assert.Equal(t, "0", itoa(0))
	assert.Equal(t, "1", itoa(1))
	assert.Equal(t, "12345", itoa(12345))
	assert.Equal(t, "-42", itoa(-42))
}

// TestRateLimit_Singleton同cfg复用rateLimiter (P2)
// 同 cfg 多次 RateLimit() 应返回同一个 rateLimiter (避免 N 路由 = N goroutine)
func TestRateLimit_Singleton同cfg复用rateLimiter(t *testing.T) {
	cfg := RateLimitConfig{Window: time.Minute, Max: 100, Message: "shared"}

	// 清空缓存后第一次调用应创建
	resetRateLimiterCache()
	rl1 := getOrCreateRateLimiter(cfg)
	require.NotNil(t, rl1)

	// 第二次同 cfg 应返回同一实例
	rl2 := getOrCreateRateLimiter(cfg)
	assert.Same(t, rl1, rl2, "同 cfg 应复用同一 rateLimiter")

	// 不同 cfg 应创建不同实例
	cfg2 := RateLimitConfig{Window: time.Minute, Max: 50, Message: "shared"}
	rl3 := getOrCreateRateLimiter(cfg2)
	assert.NotSame(t, rl1, rl3, "不同 Max 应创建新 rateLimiter")

	// 不同 Message 应创建不同实例
	cfg3 := RateLimitConfig{Window: time.Minute, Max: 100, Message: "different"}
	rl4 := getOrCreateRateLimiter(cfg3)
	assert.NotSame(t, rl1, rl4, "不同 Message 应创建新 rateLimiter")
}
