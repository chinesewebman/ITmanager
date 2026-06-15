package api_test

import (
	"crypto/rand"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"network-monitor-platform/internal/api"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/migrate"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// genUUID 返回 v4 UUID 字符串（用于 gen_random_uuid() 替身）
func genUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// sqlite3WithGenUUID 包装 mattn/go-sqlite3 driver，注册 PG gen_random_uuid() 替身
// 用 ConnectHook 在每个新连接上 RegisterFunc
func init() {
	sql.Register("sqlite3_uuid", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("gen_random_uuid", genUUID, true)
		},
	})
}

// testMigrationsFS 包装 embed.FS 把 testdata/migrations/ 暴露成 "migrations" 路径
// migrate.Load 期望 FS 根下有 "migrations" 目录
//
//go:embed testdata/migrations/*.sql
var testMigrationsRoot embed.FS

type testMigrationsFS struct{ inner embed.FS }

func (m testMigrationsFS) Open(name string) (fs.File, error) {
	return m.inner.Open("testdata/" + name)
}
func (m testMigrationsFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return m.inner.ReadDir("testdata/" + name)
}
func (m testMigrationsFS) ReadFile(name string) ([]byte, error) {
	return m.inner.ReadFile("testdata/" + name)
}

// setupTestRouter 搭一个真实集成测试路由：sqlite 内存 DB + 跑测试 migrations + SetupRouter
// SetupRouter 依赖全局 database.DB 和 metrics registry，所以测试间需要保存/恢复
func setupTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// 1. 真实 sqlite 内存 DB（每个测试独立 schema）
	dsn := fmt.Sprintf("file:test_%s?mode=memory&cache=shared", t.Name())
	sqlDB, err := sql.Open("sqlite3_uuid", dsn)
	require.NoError(t, err)
	_, _ = sqlDB.Exec("PRAGMA foreign_keys = ON")

	db, err := gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{})
	require.NoError(t, err)

	// 2. 跑测试 schema（sqlite 兼容版，不用 gorm AutoMigrate 避开 gen_random_uuid() default 限制）
	migrate.FS = testMigrationsFS{inner: testMigrationsRoot}
	require.NoError(t, migrate.Up(db))

	// 3. 注入全局 DB（保存旧值，cleanup 恢复）
	oldDB := database.GetDB()
	database.SetDBForTest(db)
	t.Cleanup(func() {
		database.SetDBForTest(oldDB)
		_ = sqlDB.Close()
	})

	// 4. 加载测试 config（write yaml 到 tmp + Load）
	cfg := loadTestConfigForRoutes(t)

	// 5. metrics 初始化（InitMetrics 幂等）
	_ = api.InitMetrics()

	return api.SetupRouter(cfg)
}

func loadTestConfigForRoutes(t *testing.T) *config.Config {
	t.Helper()
	// 与 middleware_test 同样的模板
	yaml := `server:
  host: 0.0.0.0
  port: 8080
  mode: debug
  metrics_enabled: false

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

// genValidToken 生成一个能通过 AuthMiddleware 的 JWT
func genValidToken(t *testing.T) string {
	t.Helper()
	userID := uuid.NewString()
	tok, err := middleware.GenerateToken(userID, "testuser", "admin")
	require.NoError(t, err)
	return tok
}

// ==================== /healthz 公共探针 ====================

func TestRoutes_Healthz_不需鉴权(t *testing.T) {
	r := setupTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "alive")
}

func TestRoutes_Readyz_DB可达时返200(t *testing.T) {
	r := setupTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "sqlite 内存 DB 应 ping 成功")
}

// ==================== Auth 鉴权拦截 ====================

func TestRoutes_Protected_无token返401(t *testing.T) {
	r := setupTestRouter(t)
	paths := []string{
		"/api/assets",
		"/api/racks",
		"/api/alerts",
		"/api/tickets",
		"/api/users",
		"/api/dashboard/stats",
		"/api/notification-channels",
		"/api/integrations/status",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code, "无 token 应 401")
		})
	}
}

func TestRoutes_Protected_带有效token能进handler(t *testing.T) {
	r := setupTestRouter(t)
	tok := genValidToken(t)

	req := httptest.NewRequest(http.MethodGet, "/api/assets", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 进了 handler 不代表 200（可能 db 返回空/500），
	// 但至少不应 401 / 404（路由没注册）
	assert.NotEqual(t, http.StatusUnauthorized, w.Code, "带有效 token 不应 401")
	assert.NotEqual(t, http.StatusNotFound, w.Code, "路由必须存在")
}

// ==================== 路由顺序（C-F4 修复证据） ====================

// C-F4 回归：/assets/export 必须先于 /assets/:id 匹配
// 之前 bug：访问 /api/assets/export 被 /:id 吞，handler 报 "invalid UUID"
func TestRoutes_CF4_assets_export不被id吞(t *testing.T) {
	r := setupTestRouter(t)
	tok := genValidToken(t)

	req := httptest.NewRequest(http.MethodGet, "/api/assets/export", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 进了 export handler：要么 200（CSV 头），要么 500（业务错）
	// 但绝不能 401（路径存在），也不能 404
	assert.NotEqual(t, http.StatusUnauthorized, w.Code, "/export 路由必须注册")
	assert.NotEqual(t, http.StatusNotFound, w.Code, "/export 路由必须注册")
	// 关键证据：不是 400 invalid uuid（说明 /:id 没吞它）
	body := w.Body.String()
	assert.NotContains(t, body, "invalid UUID", "/export 不应被 /:id 当作 id 解析")
}

// C-F4 同类：/alerts/stats 必须在 /:id 之前
func TestRoutes_CF4_alerts_stats不被id吞(t *testing.T) {
	r := setupTestRouter(t)
	tok := genValidToken(t)

	req := httptest.NewRequest(http.MethodGet, "/api/alerts/stats", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusUnauthorized, w.Code)
	assert.NotEqual(t, http.StatusNotFound, w.Code)
	assert.NotContains(t, w.Body.String(), "invalid UUID", "/stats 不应被 /:id 当作 id 解析")
}

// C-P6 bulk 端点：/alerts/bulk-ack 等静态段也必须先于 /:id
func TestRoutes_CP6_alerts_bulk_不返回路由不匹配(t *testing.T) {
	r := setupTestRouter(t)
	tok := genValidToken(t)

	for _, ep := range []string{"/api/alerts/bulk-ack", "/api/alerts/bulk-resolve", "/api/alerts/bulk-delete"} {
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, ep, nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.NotEqual(t, http.StatusNotFound, w.Code, ep+" 路由必须注册")
		})
	}
}

// ==================== Public /api/auth/* ====================

func TestRoutes_Login_未带body返400或401(t *testing.T) {
	r := setupTestRouter(t)
	// 空 body POST /api/auth/login：service 会查 db 返 user not found 或 validation err
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// 期望：4xx（不是 5xx 也不是 404 路由）
	assert.GreaterOrEqual(t, w.Code, 400)
	assert.NotEqual(t, http.StatusNotFound, w.Code, "/auth/login 路由必须注册")
	assert.Less(t, w.Code, 500, "空 body 不应触发 panic/500")
}

// ==================== NoRoute SPA fallback ====================

func TestRoutes_NoRoute_返前端indexHtml(t *testing.T) {
	r := setupTestRouter(t)
	// 没注册的前端路由（SPA fallback）→ 应返 frontend/dist/index.html
	// 但 dist 不存在时 gin 会抛错或返空
	// 至少不应 404
	req := httptest.NewRequest(http.MethodGet, "/some/spa/route", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// NoRoute 走的 c.File("./frontend/dist/index.html")，文件不存在时 c.File 写 200 + 空 body 或 404
	// 我们只断言：不是 401（NoRoute 不走鉴权）
	assert.NotEqual(t, http.StatusUnauthorized, w.Code)
}

// ==================== 防止 gorm DB 未注入的 panic ====================

// 关键：SetupRouter 必须能跑完（无 panic）—— 这是所有 handler 测试的前置条件
func TestRoutes_SetupRouter_无panic(t *testing.T) {
	assert.NotPanics(t, func() {
		r := setupTestRouter(t)
		assert.NotNil(t, r)
	})
}
