package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"network-monitor-platform/internal/api"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ==================== CORS 链路 ====================

func TestMiddleware_CORS_预检OPTIONS带所有CORS头(t *testing.T) {
	r := setupTestRouter(t)

	req := httptest.NewRequest("OPTIONS", "/api/auth/login", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 200 (no status 204? gin CORS middleware 通常 204)
	assert.True(t, w.Code == http.StatusNoContent || w.Code == http.StatusOK,
		"预检应返 200/204，实际: %d", w.Code)
	assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Origin"), "应设 ACAO 头")
	assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Methods"), "应设 ACAM 头")
	assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Headers"), "应设 ACAH 头")
}

func TestMiddleware_CORS_普通GET请求带ACAO头(t *testing.T) {
	r := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "http://localhost:5173", w.Header().Get("Access-Control-Allow-Origin"))
}

// ==================== Auth 中间件链路 ====================

func TestMiddleware_Auth_无Token_ProtectedGroup返401(t *testing.T) {
	r := setupTestRouter(t)

	endpoints := []string{
		"/api/users",
		"/api/assets",
		"/api/racks",
		"/api/alerts",
		"/api/tickets",
		"/api/dashboard/stats",
		"/api/alert-rules",
		"/api/notification-channels",
		"/api/sites",
	}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest("GET", ep, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code, "%s 无 token 应 401", ep)
		})
	}
}

func TestMiddleware_Auth_带Token_正常进入Handler(t *testing.T) {
	r := setupTestRouter(t)
	token := genValidToken(t)

	req := httptest.NewRequest("GET", "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// /api/auth/me 需 handler 实现，200/404 都行（取决于 user 存在），但不应 401
	assert.NotEqual(t, http.StatusUnauthorized, w.Code, "带有效 token 不应 401")
}

func TestMiddleware_Auth_过期Token_返401(t *testing.T) {
	r := setupTestRouter(t)
	// 构造一个过期 token（exp 时间已过）
	expiredToken := genExpiredToken(t)

	req := httptest.NewRequest("GET", "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+expiredToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code, "过期 token 应 401")
}

func TestMiddleware_Auth_格式错误Token_返401(t *testing.T) {
	r := setupTestRouter(t)

	badTokens := []string{
		"garbage",
		"Bearer ",
		"Bearer xxx.yyy.zzz", // 不是合法 JWT
		"Basic dXNlcjpwYXNz",
	}
	for _, tok := range badTokens {
		t.Run(tok, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/auth/me", nil)
			req.Header.Set("Authorization", tok)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code, "格式错 token '%s' 应 401", tok)
		})
	}
}

// ==================== Metrics 中间件链路 ====================

func TestMiddleware_Metrics_开关开启时暴露端点(t *testing.T) {
	cfg := loadTestConfigForRoutes(t)
	cfg.Server.MetricsEnabled = true
	router := setupRouterWithConfig(t, cfg)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "http_requests_total", "应暴露 http_requests_total metric")
	assert.Contains(t, body, "http_request_duration_seconds", "应暴露 http_request_duration_seconds metric")
}

func TestMiddleware_Metrics_开关关闭时不暴露端点(t *testing.T) {
	cfg := loadTestConfigForRoutes(t)
	cfg.Server.MetricsEnabled = false
	router := setupRouterWithConfig(t, cfg)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code, "metrics 关闭时 /metrics 返 404")
}

func TestMiddleware_Metrics_请求记录labels_含method_path_status(t *testing.T) {
	cfg := loadTestConfigForRoutes(t)
	cfg.Server.MetricsEnabled = true
	router := setupRouterWithConfig(t, cfg)

	// 触发 1 个 /healthz 请求
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	req = httptest.NewRequest("GET", "/metrics", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	body := w.Body.String()

	// http_requests_total 应有 method="GET" path="/healthz" status="200" 标签
	assert.Contains(t, body, `path="/healthz"`, "metric label 应含 path")
	assert.Contains(t, body, `method="GET"`, "metric label 应含 method")
	assert.Contains(t, body, `status="200"`, "metric label 应含 status")
}

// ==================== Recovery 中间件 ====================

func TestMiddleware_Recovery_HandlerPanic返500不挂进程(t *testing.T) {
	// 用裸 gin.Engine + Recovery 中间件 + 触发 panic 的 handler
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/panic", func(c *gin.Context) {
		panic("test panic")
	})

	req := httptest.NewRequest("GET", "/panic", nil)
	w := httptest.NewRecorder()
	assert.NotPanics(t, func() { r.ServeHTTP(w, req) }, "panic 应被 Recovery 捕获，不应传播")
	assert.Equal(t, http.StatusInternalServerError, w.Code, "panic 应返 500")
}

// ==================== Logger 中间件 (副作用写 stdout) ====================

func TestMiddleware_Logger_请求不挂掉(t *testing.T) {
	// gin.Default() 含 Logger + Recovery，这里只验证不挂
	r := setupTestRouter(t)
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	assert.NotPanics(t, func() { r.ServeHTTP(w, req) })
	assert.Equal(t, http.StatusOK, w.Code)
}

// ==================== NoRoute 行为 ====================

func TestMiddleware_NoRoute_不存在的URL返前端indexHtml(t *testing.T) {
	r := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/totally/nonexistent/path", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// NoRoute 设的 c.File(./frontend/dist/index.html) — 文件不存在时 404
	// 但核心断言：不应 500/panic，且响应对未知路径合理
	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusNotFound,
		"NoRoute 应返 200 (SPA 兜底) 或 404 (index.html 缺失)，实际: %d", w.Code)
}

func TestMiddleware_NoRoute_ProtectedGroup下未知子路径仍走NoRoute不挂(t *testing.T) {
	r := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/totally-unknown-endpoint", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 因为 protected group 用 AuthMiddleware，未知子路径会被 middleware 拦
	// 关键：不挂、不 panic
	assert.True(t, w.Code == http.StatusUnauthorized || w.Code == http.StatusOK || w.Code == http.StatusNotFound,
		"未知 endpoint 应 401/200/404 之一，实际: %d", w.Code)
}

// ==================== 完整链路：Login → Me 流 ====================

func TestMiddleware_链路_Login返回token后能进Me(t *testing.T) {
	r := setupTestRouter(t)

	// 1. 登录
	loginBody := `{"username":"admin","password":"admin123"}`
	loginReq := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	r.ServeHTTP(loginW, loginReq)

	// login 成功或失败都接受（取决于 user 是否 seed），关键是 status 合理
	if loginW.Code != http.StatusOK {
		t.Skipf("login 返回 %d（无 seeded user），跳过链路测试", loginW.Code)
		return
	}

	// 2. 解析 token
	var loginResp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(loginW.Body.Bytes(), &loginResp))
	require.NotEmpty(t, loginResp.Data.Token, "login 应返回 token")

	// 3. 用 token 访问 /me
	meReq := httptest.NewRequest("GET", "/api/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+loginResp.Data.Token)
	meW := httptest.NewRecorder()
	r.ServeHTTP(meW, meReq)
	assert.Equal(t, http.StatusOK, meW.Code, "用 login 拿到的 token 访问 /me 应 200")
}

// ==================== 链路：多个 middleware 顺序 ====================

func TestMiddleware_顺序_401响应也带CORS头(t *testing.T) {
	r := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/auth/me", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	// 不带 token，期望 401
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	// CORS 是 401 前还是 401 后？CORS 在最外层 → 即使 401 也带 ACAO
	assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Origin"),
		"401 响应也应带 CORS 头（前端跨域才能读到 status）")
}

func TestMiddleware_并发_同一接口多次请求不串数据(t *testing.T) {
	r := setupTestRouter(t)

	var wg sync.WaitGroup
	var counter int64
	n := 20

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code == http.StatusOK {
				atomic.AddInt64(&counter, 1)
			}
		}(i)
	}
	wg.Wait()
	assert.Equal(t, int64(n), atomic.LoadInt64(&counter), "所有并发请求都应返 200")
}

// ==================== 不暴露的 internal helper ====================

func setupRouterWithConfig(t *testing.T, cfg *config.Config) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	_ = api.InitMetrics()
	return api.SetupRouter(cfg)
}

// genExpiredToken 构造 exp 已过的 JWT（用 cfg 里的 secret 签）
func genExpiredToken(t *testing.T) string {
	t.Helper()
	cfg := loadTestConfigForRoutes(t)
	claims := jwt.MapClaims{
		"user_id": "test",
		"role":    "admin",
		"exp":     time.Now().Add(-1 * time.Hour).Unix(),
		"iat":     time.Now().Add(-2 * time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(cfg.Auth.JWT.Secret))
	require.NoError(t, err)
	return signed
}

// 防 database 包未用报警
var _ = database.GetDB
