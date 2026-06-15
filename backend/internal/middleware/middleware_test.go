package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/metrics"
	"network-monitor-platform/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadTestConfig 加载测试用 config（基于项目 config.yaml 模板 + 改字段）
func loadTestConfig(t *testing.T) *config.Config {
	t.Helper()
	// 复制真实 config.yaml 结构，仅改 secret/pepper/origins
	yaml := `server:
  host: 0.0.0.0
  port: 8080
  mode: debug
  metrics_enabled: true

database:
  host: "localhost"
  port: 5432
  user: "nmp"
  password: "test-pass"
  name: "network_monitor"
  sslmode: "disable"

redis:
  host: "localhost"
  port: 6379
  password: ""
  db: 0

integrations:
  netbox:
    url: "http://localhost:8000"
    token: ""
  zabbix:
    url: "http://localhost:8080"
    user: "Admin"
    password: "zabbix"
  glpi:
    url: "http://localhost"
    app_token: ""
    user_token: ""

auth:
  jwt:
    secret: "test-secret-32-bytes-for-hmac-sha256!"
    expire: 86400
  api_key_pepper: "test-pepper-32-bytes-for-hmac-sha256!!"
  ldap:
    enabled: false
    url: "ldap://localhost:389"
    base_dn: "dc=company,dc=com"
    bind_user: ""
    bind_password: ""

allowed_origins:
  - "http://localhost:5173"
  - "http://example.com"

notifications:
  smtp:
    enabled: false
    smtp_host: "smtp.gmail.com"
    smtp_port: 587
    smtp_user: ""
    smtp_password: ""
    from: "noreply@company.com"

log:
  level: "info"
  format: "json"
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	return cfg
}

// ==================== CORS Middleware 测试 ====================

func TestCORS_白名单Origin回显ACAO头(t *testing.T) {
	cfg := loadTestConfig(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.CORS(cfg))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, "http://localhost:5173", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORS_非白名单Origin不回显(t *testing.T) {
	cfg := loadTestConfig(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.CORS(cfg))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://evil.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, http.StatusOK, w.Code) // 仍放行
}

func TestCORS_预检OPTIONS返204(t *testing.T) {
	cfg := loadTestConfig(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.CORS(cfg))
	r.OPTIONS("/test", func(c *gin.Context) { c.String(200, "should-not-reach") })

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// ==================== Auth 纯函数测试（不依赖 DB） ====================

func TestGenerateToken_生成成功(t *testing.T) {
	loadTestConfig(t)
	userID := uuid.NewString()
	username := "testuser"
	role := "admin"

	tok, err := middleware.GenerateToken(userID, username, role)
	require.NoError(t, err)
	assert.NotEmpty(t, tok)
	assert.Greater(t, len(strings.Split(tok, ".")), 2, "JWT 应有 3 段")
}

func TestGenerateTokenAndVerify_往返成功(t *testing.T) {
	loadTestConfig(t)
	userID := uuid.NewString()
	username := "alice"
	role := "user"

	tok, err := middleware.GenerateToken(userID, username, role)
	require.NoError(t, err)

	claims, err := middleware.VerifyToken(tok)
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, username, claims.Username)
	assert.Equal(t, role, claims.Role)
	assert.Equal(t, "network-monitor-platform", claims.Issuer)
}

func TestVerifyToken_无效签名返err(t *testing.T) {
	loadTestConfig(t)
	_, err := middleware.VerifyToken("invalid.token.here")
	assert.Error(t, err)
}

func TestVerifyToken_过期token返err(t *testing.T) {
	cfg := loadTestConfig(t)
	// 构造一个已过期的 token
	expired := jwt.NewWithClaims(jwt.SigningMethodHS256, &middleware.Claims{
		UserID:   uuid.NewString(),
		Username: "x",
		Role:     "user",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	})
	tok, err := expired.SignedString([]byte(cfg.Auth.JWT.Secret))
	require.NoError(t, err)

	_, err = middleware.VerifyToken(tok)
	assert.Error(t, err, "过期 token 应被拒绝")
}

// ==================== HTTPMetrics Middleware 测试 ====================

func newMetricsRegistry(t *testing.T) *metrics.Registry {
	t.Helper()
	reg := metrics.New()
	// HTTPMetrics 调 IncCounter/ObserveHistogram，必须先注册对应 metric
	reg.NewCounterVec("http_requests_total", "Total HTTP requests", []string{"method", "path", "status"})
	reg.NewHistogramVec("http_request_duration_seconds", "Duration", []string{"method", "path"}, []float64{0.001, 0.01, 0.1, 1})
	return reg
}

func TestHTTPMetrics_写入counter和histogram(t *testing.T) {
	reg := newMetricsRegistry(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.HTTPMetrics(reg))
	r.GET("/api/test", func(c *gin.Context) { c.String(200, "ok") })
	r.GET("/api/error", func(c *gin.Context) { c.String(500, "fail") })

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/error", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 通过 Handler 取 /metrics 文本
	w2 := httptest.NewRecorder()
	reg.Handler().ServeHTTP(w2, httptest.NewRequest("GET", "/metrics", nil))
	out := w2.Body.String()
	assert.Contains(t, out, "http_requests_total")
	assert.Contains(t, out, "http_request_duration_seconds")
}

func TestHTTPMetrics_nilRegistry_NoOp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.HTTPMetrics(nil))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code) // 不 panic
}

func TestHTTPMetrics_未匹配路由unmatched_防基数爆炸(t *testing.T) {
	reg := newMetricsRegistry(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.HTTPMetrics(reg))
	// 注册一个路由，但请求不同路径
	r.GET("/known", func(c *gin.Context) { c.String(200, "ok") })

	for _, p := range []string{"/random1", "/random2", "/random3"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}

	w := httptest.NewRecorder()
	reg.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	out := w.Body.String()
	// 3 个不同 unmatched 路径都归类成 "unmatched"（不爆炸）
	assert.Contains(t, out, "unmatched")
}

// 兼容：context import 防 unused（某些 linter 严格）
var _ = context.Background
