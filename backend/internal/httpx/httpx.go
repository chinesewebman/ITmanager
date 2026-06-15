// Package httpx 提供集成层共享 HTTP 工具：retry / circuit breaker / metrics / ctx。
//
// 设计原则：
//   - 零依赖：标准库 + 内置 metrics
//   - 线程安全：sync.Map + atomic 计数
//   - 集成层只关心业务 URL 与 body，retry/熔断/ctx 一律走这里
//
// 用法：
//
//	c := httpx.New(cfg, "netbox", &metrics)
//	body, err := c.Do(ctx, "GET", "/api/dcim/devices/", nil)
//
// Circuit breaker 状态机：
//
//	closed → open（连续 ≥ threshold 次失败）
//	open → half-open（cooldown 后下次请求）
//	half-open → closed（成功）
//	half-open → open（失败 → 重置 cooldown）
package httpx

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Config 客户端配置。
type Config struct {
	BaseURL     string
	BearerToken string // 可选；填了走 Authorization: Bearer ...
	// 也可以用 Header 注入（NetBox 用 Token xxx 格式）
	HeaderName  string
	HeaderValue string

	Timeout       time.Duration // 单次请求 timeout
	MaxRetries    int           // 重试次数（不含首次）
	RetryBackoff  time.Duration // 重试基础退避（实际 = backoff * 2^attempt）
	BreakerThresh int           // 触发熔断的连续失败次数
	BreakerCool   time.Duration // 熔断 cooldown
}

// DefaultConfig 返回保守默认（10s timeout / 2 retries / 5 失败熔断 / 30s cooldown）。
func DefaultConfig(baseURL string) Config {
	return Config{
		BaseURL:       baseURL,
		Timeout:       10 * time.Second,
		MaxRetries:    2,
		RetryBackoff:  500 * time.Millisecond,
		BreakerThresh: 5,
		BreakerCool:   30 * time.Second,
	}
}

// MetricsRecorder 注入 counter/histogram 写入（避免 httpx 直接依赖 metrics 包）。
type MetricsRecorder interface {
	IncRequest(system, status string)
	ObserveDuration(system string, seconds float64)
}

// Client HTTP 客户端（含 retry + circuit breaker + ctx）。
type Client struct {
	cfg     Config
	hc      *http.Client
	metrics MetricsRecorder
	system  string

	mu               sync.Mutex
	failStreak       int32
	openUntil        time.Time // 熔断打开到此时（之后转 half-open）
	openStreak       int32
	halfOpenInflight int32
}

// New 构造客户端。
func New(cfg Config, system string, m MetricsRecorder) *Client {
	return &Client{
		cfg:     cfg,
		hc:      &http.Client{Timeout: cfg.Timeout},
		metrics: m,
		system:  system,
	}
}

// Do 发起请求，返回响应 body（已读全）。
// 行为：
//   - 检查熔断：open 立即返 ErrCircuitOpen
//   - 每次尝试带 ctx（ctx 取消立即返）
//   - 失败（5xx / 网络错）按指数退避重试至 MaxRetries
//   - 4xx 不重试（client 错非服务端问题）
//   - 成功重置 failStreak
func (c *Client) Do(ctx context.Context, method, path string, body io.Reader) ([]byte, int, error) {
	return c.DoWithHeaders(ctx, method, path, body, nil)
}

// DoWithHeaders 允许 per-call 注入额外 header（GLPI 这类需要会话 token 的场景）。
func (c *Client) DoWithHeaders(ctx context.Context, method, path string, body io.Reader, extra map[string]string) ([]byte, int, error) {
	// 1. 熔断检查
	if err := c.beforeRequest(); err != nil {
		return nil, 0, err
	}

	url := c.cfg.BaseURL + path
	var lastErr error
	attempts := c.cfg.MaxRetries + 1

	for attempt := 0; attempt < attempts; attempt++ {
		// ctx 取消检查
		if err := ctx.Err(); err != nil {
			c.afterFailure()
			return nil, 0, fmt.Errorf("httpx: ctx: %w", err)
		}

		// 退避（除首次）
		if attempt > 0 {
			backoff := c.cfg.RetryBackoff * time.Duration(math.Pow(2, float64(attempt-1)))
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				c.afterFailure()
				return nil, 0, ctx.Err()
			}
		}

		// 单次请求
		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			lastErr = err
			continue
		}
		c.applyAuth(req)
		for k, v := range extra {
			req.Header.Set(k, v)
		}

		start := time.Now()
		resp, err := c.hc.Do(req)
		dur := time.Since(start).Seconds()

		if err != nil {
			lastErr = err
			c.recordMetrics("error", dur)
			continue // 重试
		}

		// 读 body
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		status := fmt.Sprintf("%d", resp.StatusCode)
		c.recordMetrics(status, dur)

		// 5xx → 失败
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("httpx: %s %s → %d", method, path, resp.StatusCode)
			continue
		}

		// 4xx → 终态（不重试）
		if resp.StatusCode >= 400 {
			c.afterFailure()
			return respBody, resp.StatusCode, fmt.Errorf("httpx: %s %s → %d: %s", method, path, resp.StatusCode, string(respBody))
		}

		// 2xx/3xx → 成功
		c.afterSuccess()
		return respBody, resp.StatusCode, nil
	}

	c.afterFailure()
	return nil, 0, fmt.Errorf("httpx: %s %s 失败 %d 次: %w", method, path, attempts, lastErr)
}

// applyAuth 注入鉴权 header。
func (c *Client) applyAuth(req *http.Request) {
	if c.cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.BearerToken)
	}
	if c.cfg.HeaderName != "" {
		req.Header.Set(c.cfg.HeaderName, c.cfg.HeaderValue)
	}
}

// recordMetrics 写入 metric。
func (c *Client) recordMetrics(status string, seconds float64) {
	if c.metrics == nil {
		return
	}
	c.metrics.IncRequest(c.system, status)
	c.metrics.ObserveDuration(c.system, seconds)
}

// ErrCircuitOpen 熔断开启错误。
var ErrCircuitOpen = fmt.Errorf("httpx: circuit breaker open")

// beforeRequest 熔断检查：open 立即返；half-open 只放一个探针。
func (c *Client) beforeRequest() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if c.openUntil.After(now) {
		return ErrCircuitOpen
	}
	// half-open 探针：只允许一个请求通过
	if c.openStreak > 0 && !c.openUntil.IsZero() {
		if !atomic.CompareAndSwapInt32(&c.halfOpenInflight, 0, 1) {
			return ErrCircuitOpen
		}
	}
	return nil
}

func (c *Client) afterSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failStreak = 0
	c.openStreak = 0
	c.openUntil = time.Time{}
	atomic.StoreInt32(&c.halfOpenInflight, 0)
}

func (c *Client) afterFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failStreak++
	if c.failStreak >= int32(c.cfg.BreakerThresh) {
		c.openStreak++
		c.openUntil = time.Now().Add(c.cfg.BreakerCool)
		c.failStreak = 0
	}
	atomic.StoreInt32(&c.halfOpenInflight, 0)
}

// State 返回熔断当前状态（用于 /metrics 暴露）。
func (c *Client) State() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.openUntil.IsZero() {
		return "closed"
	}
	if c.openUntil.After(time.Now()) {
		return "open"
	}
	return "half_open"
}
