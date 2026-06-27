package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"network-monitor-platform/internal/apierr"

	"github.com/gin-gonic/gin"
)

// RateLimitConfig 限流配置（per-route 用）
type RateLimitConfig struct {
	// 窗口长度（默认 1 分钟）
	Window time.Duration
	// 窗口内允许的最大请求数
	Max int
	// 限流 key 提取器（默认 ClientIP + path）
	KeyFunc func(*gin.Context) string
	// 超限时的拒绝信息
	Message string
}

// DefaultRateLimitConfig 默认: 100 req/min per IP+path
func DefaultRateLimitConfig(max int) RateLimitConfig {
	return RateLimitConfig{
		Window:  time.Minute,
		Max:     max,
		KeyFunc: nil, // 用 defaultKey
		Message: "请求过于频繁，请稍后再试",
	}
}

func defaultKey(c *gin.Context) string {
	return c.ClientIP() + "|" + c.FullPath()
}

// bucket 单 IP+path 滑动窗口计数
type bucket struct {
	mu       sync.Mutex
	window   time.Duration
	max      int
	hits     []time.Time
	lastSeen time.Time
}

// record 记录一次访问, 返回是否超限
func (b *bucket) record(now time.Time) (allowed bool, retryAfter time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 滑动窗口: 砍掉窗口外的旧 hit
	cutoff := now.Add(-b.window)
	kept := b.hits[:0]
	for _, t := range b.hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	b.hits = kept

	if len(b.hits) >= b.max {
		// 超限: retryAfter = 最早 hit 退出窗口的等待时间
		retryAfter = b.window - now.Sub(b.hits[0])
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, retryAfter
	}
	b.hits = append(b.hits, now)
	b.lastSeen = now
	return true, 0
}

// rateLimiter 进程内单例（per (key) bucket）
type rateLimiter struct {
	mu      sync.RWMutex
	buckets map[string]*bucket
	cfg     RateLimitConfig
}

// P2: rateLimiter 包级缓存 — 同一 cfg 复用同一 rateLimiter, 避免 N 路由 = N goroutine
// key 由 (Window, Max, Message) 构成; KeyFunc 走 default 时归一
var (
	rlCacheMu sync.Mutex
	rlCache   = make(map[string]*rateLimiter)
)

func newRateLimiter(cfg RateLimitConfig) *rateLimiter {
	if cfg.KeyFunc == nil {
		cfg.KeyFunc = defaultKey
	}
	if cfg.Window == 0 {
		cfg.Window = time.Minute
	}
	if cfg.Max <= 0 {
		cfg.Max = 100
	}
	rl := &rateLimiter{
		buckets: make(map[string]*bucket),
		cfg:     cfg,
	}
	// 1% 命中率时清理过期 bucket, 防止内存膨胀
	go rl.gc()
	return rl
}

// rateLimiterCacheKey 生成缓存 key (cfg 归一化后)
func rateLimiterCacheKey(cfg RateLimitConfig) string {
	return fmt.Sprintf("%d|%d|%s", cfg.Window, cfg.Max, cfg.Message)
}

// getOrCreateRateLimiter 包级缓存查找/创建
func getOrCreateRateLimiter(cfg RateLimitConfig) *rateLimiter {
	key := rateLimiterCacheKey(cfg)
	rlCacheMu.Lock()
	defer rlCacheMu.Unlock()
	if rl, ok := rlCache[key]; ok {
		return rl
	}
	rl := newRateLimiter(cfg)
	rlCache[key] = rl
	return rl
}

// resetRateLimiterCache 测试用：清空 rateLimiter 缓存 (gc goroutine 会因没有 bucket 引用而退出)
// 注意：调用此函数后, 旧 rateLimiter 的 gc goroutine 仍在跑（无 bucket 时空转）,
// 真正清理由下一次 RateLimit() 调用复用同 key 时继续; 或测试结束进程退出。
// 生产代码不应调用此函数。
func resetRateLimiterCache() {
	rlCacheMu.Lock()
	defer rlCacheMu.Unlock()
	rlCache = make(map[string]*rateLimiter)
}

func (rl *rateLimiter) getBucket(key string) *bucket {
	rl.mu.RLock()
	b, ok := rl.buckets[key]
	rl.mu.RUnlock()
	if ok {
		return b
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if b, ok := rl.buckets[key]; ok {
		return b
	}
	b = &bucket{window: rl.cfg.Window, max: rl.cfg.Max}
	rl.buckets[key] = b
	return b
}

// gc 每 5 分钟清理 idle 超过 2 个 window 的 bucket
func (rl *rateLimiter) gc() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-2 * rl.cfg.Window)
		rl.mu.Lock()
		for k, b := range rl.buckets {
			b.mu.Lock()
			if b.lastSeen.Before(cutoff) {
				delete(rl.buckets, k)
			}
			b.mu.Unlock()
		}
		rl.mu.Unlock()
	}
}

// RateLimit 返回一个 per-route 限流中间件（per-IP+path sliding window）
//
// P2: 同 cfg 复用同一 rateLimiter + gc goroutine (包级缓存), N 路由不再 = N goroutine
//
// 用法:
//
//	r.GET("/api/auth/login", middleware.RateLimit(middleware.DefaultRateLimitConfig(5)))
//
// 升级到 ulule/limiter/v3 (Redis 后端) 时, 替换 record() 实现即可, 接口不变。
func RateLimit(cfg RateLimitConfig) gin.HandlerFunc {
	rl := getOrCreateRateLimiter(cfg)
	keyFn := rl.cfg.KeyFunc
	msg := rl.cfg.Message
	return func(c *gin.Context) {
		key := keyFn(c)
		allowed, retryAfter := rl.getBucket(key).record(time.Now())
		if !allowed {
			// 标头提示 Retry-After (RFC 6585)
			seconds := int(retryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", itoa(seconds))
			c.Header("X-RateLimit-Remaining", "0")
			apierr.Respond(c, http.StatusTooManyRequests, "rate_limit_exceeded", msg, nil)
			c.Abort()
			return
		}
		// 成功路径: 标头提示剩余配额（估算: max - 当前窗口内 hit 数）
		b := rl.getBucket(key)
		b.mu.Lock()
		remaining := b.max - len(b.hits)
		b.mu.Unlock()
		if remaining < 0 {
			remaining = 0
		}
		c.Header("X-RateLimit-Limit", itoa(b.max))
		c.Header("X-RateLimit-Remaining", itoa(remaining))
		c.Next()
	}
}

// itoa 简单 int → string 转换, 避免 strconv 依赖
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := make([]byte, 0, 8)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
