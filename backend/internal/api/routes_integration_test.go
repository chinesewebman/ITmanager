package api_test

import (
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"network-monitor-platform/internal/api"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/integration"
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

	// v2.3: 构造 IntegrationService（routes.SetupRouter 现在需要）
	integSvc := integration.NewIntegrationService(cfg, api.NewIntegrationMetricsAdapter())
	return api.SetupRouter(cfg, integSvc)
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

// ==================== /swagger UI 集成 ====================

func TestRoutes_SwaggerUI_可达(t *testing.T) {
	r := setupTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// gin-swagger 返回 200 + HTML（含 Swagger UI）
	assert.Equal(t, http.StatusOK, w.Code, "/swagger/index.html 必须可达")
	assert.Contains(t, w.Body.String(), "Swagger", "响应 body 应含 Swagger UI HTML")
}

func TestRoutes_OpenAPISpec_可达且合法YAML(t *testing.T) {
	r := setupTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	// 基本 openapi 3.0 标识
	assert.Contains(t, body, "openapi: 3.0")
	assert.Contains(t, body, "paths:")
	assert.Contains(t, body, "components:")
}

// ==================== 资产诊断端点 (P0-1) ====================

func TestRoutes_DiagnosticTimeline_无token返401(t *testing.T) {
	r := setupTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics/assets/"+uuid.New().String()+"/timeline", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRoutes_DiagnosticTimeline_带token可访问(t *testing.T) {
	r := setupTestRouter(t)
	tok := genValidToken(t)

	// 找一个种子资产
	var assetID string
	require.NoError(t, database.GetDB().Raw("SELECT id FROM assets LIMIT 1").Scan(&assetID).Error)
	if assetID == "" {
		t.Skip("setupTestRouter 未 seed 资产，跳过")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics/assets/"+assetID+"/timeline?days=30&limit=50", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusUnauthorized, w.Code)
	assert.NotEqual(t, http.StatusNotFound, w.Code, "路由必须注册且 asset 存在")
	assert.Contains(t, w.Body.String(), "events", "响应应包含 events 字段")
}

func TestRoutes_DiagnosticTimeline_无效UUID返400(t *testing.T) {
	r := setupTestRouter(t)
	tok := genValidToken(t)

	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics/assets/not-a-uuid/timeline", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ==================== 权限矩阵：路由级鉴权（docs/FIX-PLAN-AUTHZ.md §3.3） ====================

// genTokenWithRole 生成指定角色的 JWT（genValidToken 固定 admin）
func genTokenWithRole(t *testing.T, role string) string {
	t.Helper()
	tok, err := middleware.GenerateToken(uuid.NewString(), "role-"+role, role)
	require.NoError(t, err)
	return tok
}

// gatedRoute 受控路由的**声明**：方法 + 路径模板 + 所需能力。
// 三个测试共用这张表：
//   - TestRoutes_能力矩阵_无权限被拒
//   - TestRoutes_能力矩阵_有权限放行
//   - TestRoutes_非GET路由都已分类（反向：新增非 GET 路由不在表里就失败）
//
// 路径用 :id / :shift_id 模板，与 gin 的 r.Routes() 对齐；发请求时再替换成具体值。
var gatedRoutes = []struct {
	cap          middleware.Capability
	method, path string
	// handlerErrOK: handler 会主动外连，或依赖测试 sqlite schema 里没有的表 ——
	// 沙箱里 5xx 属正常，对这类路由只断言「不被 403 拦」。
	handlerErrOK bool
}{
	// identity：用户与凭据（仅 admin）
	{middleware.CapIdentity, http.MethodGet, "/api/users", false},
	{middleware.CapIdentity, http.MethodGet, "/api/users/:id", false},
	{middleware.CapIdentity, http.MethodGet, "/api/auth/api-keys", false},
	{middleware.CapIdentity, http.MethodPost, "/api/auth/api-keys", false},
	{middleware.CapIdentity, http.MethodDelete, "/api/auth/api-keys/:id", false},
	{middleware.CapIdentity, http.MethodPut, "/api/auth/api-keys/:id/revoke", false},

	// audit：审计日志
	{middleware.CapAudit, http.MethodGet, "/api/audit-logs", false},

	// manage：集成凭据 / 通知渠道（含明文凭据）/ 破坏性删除
	{middleware.CapManage, http.MethodPost, "/api/integrations/sync", true},
	{middleware.CapManage, http.MethodPost, "/api/integrations/zabbix/test", true},
	{middleware.CapManage, http.MethodPut, "/api/integrations/zabbix", false},
	{middleware.CapManage, http.MethodPost, "/api/integrations/netbox/test", true},
	{middleware.CapManage, http.MethodPut, "/api/integrations/netbox", false},
	{middleware.CapManage, http.MethodPost, "/api/integrations/glpi/test", true},
	{middleware.CapManage, http.MethodPut, "/api/integrations/glpi", false},
	{middleware.CapManage, http.MethodGet, "/api/notification-channels", false},
	{middleware.CapManage, http.MethodPost, "/api/notification-channels", false},
	{middleware.CapManage, http.MethodPut, "/api/notification-channels/:id", false},
	{middleware.CapManage, http.MethodDelete, "/api/notification-channels/:id", false},
	{middleware.CapManage, http.MethodPut, "/api/notification-channels/:id/test", true},
	{middleware.CapManage, http.MethodDelete, "/api/assets/:id", false},
	{middleware.CapManage, http.MethodPost, "/api/alerts/bulk-delete", false},
	{middleware.CapManage, http.MethodDelete, "/api/alert-rules/:id", false},
	{middleware.CapManage, http.MethodDelete, "/api/alert-suppressions/:id", false},
	{middleware.CapManage, http.MethodDelete, "/api/oncall/schedules/:id", false},
	{middleware.CapManage, http.MethodDelete, "/api/oncall/shifts/:shift_id", false},
	{middleware.CapManage, http.MethodDelete, "/api/oncall/policies/:id", false},
	{middleware.CapManage, http.MethodDelete, "/api/runbooks/:id", false},

	// write：业务写（可逆）
	{middleware.CapWrite, http.MethodPost, "/api/assets", false},
	{middleware.CapWrite, http.MethodPut, "/api/assets/:id", false},
	{middleware.CapWrite, http.MethodPost, "/api/assets/:id/retire", false},
	{middleware.CapWrite, http.MethodPost, "/api/assets/:id/restore", false},
	{middleware.CapWrite, http.MethodPost, "/api/alert-rules", false},
	{middleware.CapWrite, http.MethodPut, "/api/alert-rules/:id", false},
	{middleware.CapWrite, http.MethodPost, "/api/alerts/bulk-ack", false},
	{middleware.CapWrite, http.MethodPost, "/api/alerts/bulk-resolve", false},
	{middleware.CapWrite, http.MethodPut, "/api/alerts/:id/ack", false},
	{middleware.CapWrite, http.MethodPut, "/api/alerts/:id/resolve", false},
	{middleware.CapWrite, http.MethodPost, "/api/alerts/:id/mark-fp", false},
	{middleware.CapWrite, http.MethodPost, "/api/tickets", false},
	{middleware.CapWrite, http.MethodPut, "/api/tickets/:id", false},
	{middleware.CapWrite, http.MethodPost, "/api/alert-suppressions", false},
	{middleware.CapWrite, http.MethodPut, "/api/alert-suppressions/:id", false},
	{middleware.CapWrite, http.MethodPost, "/api/oncall/schedules", false},
	{middleware.CapWrite, http.MethodPost, "/api/oncall/schedules/:id/shifts", false},
	{middleware.CapWrite, http.MethodPost, "/api/oncall/policies", false},
	{middleware.CapWrite, http.MethodPost, "/api/runbooks", false},
	{middleware.CapWrite, http.MethodPut, "/api/runbooks/:id", false},
	{middleware.CapWrite, http.MethodPost, "/api/metric-snapshots", false},
	{middleware.CapWrite, http.MethodGet, "/api/diagnostics/ping", false},
	{middleware.CapWrite, http.MethodGet, "/api/diagnostics/traceroute", false},
}

// matrixRoles 参与矩阵断言的令牌角色：6 个权威角色 + 2 个遗留别名 + 空角色（fail-safe 只读）。
var matrixRoles = []string{
	middleware.RoleAdmin, middleware.RoleOpsAdmin, middleware.RoleOpsUser,
	middleware.RoleAuditor, middleware.RoleReadonly, middleware.RoleUser,
	"operator", "viewer", "",
}

// ungatedRoutes 刻意不挂能力的路由 → 豁免理由。新增路由若既不在 gatedRoutes
// 也不在这里，TestRoutes_所有路由都已分类 会失败（必须显式分类）。
//
// 非 GET：认证 / 自助端点 / 纯计算无副作用。
// GET：读地板（docs/FIX-PLAN-AUTHZ.md §3.2）—— 未被更高能力覆盖的端点默认放行。
var ungatedRoutes = map[string]string{
	// ---- 认证与自助（只需认证，不挂能力，否则 readonly 连自己的密码都改不了）----
	"POST /api/auth/login":                "登录入口（限流 5/min）",
	"POST /api/auth/logout":               "登出（清 cookie）",
	"PUT /api/auth/password":              "改自己的密码",
	"POST /api/auth/skip-password-change": "自助跳过强改密（限流 3/min）",
	"GET /api/auth/me":                    "读自己的身份与能力集",

	// ---- 非 GET 但无副作用 ----
	"POST /api/alert-suppressions/preview": "纯计算：仅 DB 读 + 评估，无写入",

	// ---- 公开探针与静态资源 ----
	"GET /api/health":       "存活探针",
	"GET /healthz":          "存活探针",
	"GET /readyz":           "就绪探针",
	"GET /openapi.yaml":     "API 规格（无凭据内容）",
	"GET /static/*filepath": "前端静态资源",
	"GET /swagger/*any":     "Swagger UI",

	// ---- 读地板：业务读（列表 / 详情 / 统计 / 导出）----
	"GET /api/assets":                          "资产读",
	"GET /api/assets/:id":                      "资产读",
	"GET /api/assets/export":                   "资产导出（限 500 行 + safeCSV）",
	"GET /api/alerts":                          "告警读",
	"GET /api/alerts/:id":                      "告警读",
	"GET /api/alerts/stats":                    "告警统计",
	"GET /api/alerts/false-positives/export":   "误报导出（safeCSV）",
	"GET /api/alert-rules":                     "规则读",
	"GET /api/alert-suppressions":              "抑制读",
	"GET /api/alert-suppressions/:id":          "抑制读",
	"GET /api/tickets":                         "工单读",
	"GET /api/tickets/:id":                     "工单读",
	"GET /api/sites":                           "机房读",
	"GET /api/sites/:id":                       "机房读",
	"GET /api/racks":                           "机柜读",
	"GET /api/racks/:id":                       "机柜读",
	"GET /api/racks/:id/devices":               "机柜设备读",
	"GET /api/topology":                        "拓扑读",
	"GET /api/dashboard/stats":                 "仪表盘统计",
	"GET /api/dashboard/kpis":                  "仪表盘 KPI",
	"GET /api/dashboard/trends":                "仪表盘趋势",
	"GET /api/integrations/status":             "集成连通状态（只回 URL/用户名，不含 token）",
	"GET /api/metric-snapshots":                "指标快照读",
	"GET /api/metric-snapshots/latest":         "最新指标快照",
	"GET /api/oncall/schedules":                "排班读",
	"GET /api/oncall/schedules/:id/shifts":     "班次读",
	"GET /api/oncall/policies":                 "升级策略读",
	"GET /api/oncall/policies/:id":             "升级策略读",
	"GET /api/oncall/current":                  "当前值班人",
	"GET /api/runbooks":                        "Runbook 读",
	"GET /api/runbooks/:id":                    "Runbook 读",
	"GET /api/runbooks/recommend":              "Runbook 推荐（只读）",
	"GET /api/diagnostics/assets/:id/timeline": "资产时间线（只读）",
	"GET /api/postmortem/assets/:id/report":    "复盘报告（只读，文件名 sanitize）",
}

// concretePath 把路径模板里的 :id / :shift_id 换成具体 UUID（发请求用）
func concretePath(p string) string {
	p = strings.ReplaceAll(p, ":shift_id", uuid.NewString())
	return strings.ReplaceAll(p, ":id", uuid.NewString())
}

// requestAs 以指定角色发一次请求
func requestAs(t *testing.T, r *gin.Engine, method, path, role string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+genTokenWithRole(t, role))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestRoutes_能力矩阵_无权限被拒 逐条断言「没有该能力的角色一律 403」。
//
// 注意：本测试用 middleware.Can 决定「谁该被拒」，与被测实现同源 ——
// 矩阵本身改错时这里**不会红**，兜住它的是 middleware/roles_test.go 的
// 独立 capabilityMatrix。本测试的职责是「路由挂载与矩阵一致」。
func TestRoutes_能力矩阵_无权限被拒(t *testing.T) {
	r := setupTestRouter(t)
	for _, c := range gatedRoutes {
		denied := 0
		for _, role := range matrixRoles {
			if middleware.Can(role, c.cap) {
				continue
			}
			denied++
			t.Run(role+"/"+c.method+" "+c.path, func(t *testing.T) {
				w := requestAs(t, r, c.method, concretePath(c.path), role)
				assert.Equal(t, http.StatusForbidden, w.Code,
					"角色 %q 无 %s 能力，应 403", role, c.cap)
			})
		}
		// 每条受控路由至少要有一个「被拒」的角色，否则 matrixRoles 收窄会让本测试静默空转
		require.NotZero(t, denied, "%s %s 没有任何角色被拒：matrixRoles 是否漏了低权限角色？", c.method, c.path)
	}
}

// TestRoutes_能力矩阵_有权限放行 逐条断言「有该能力的角色不被 403/401 拦」。
// 只断言 !=403 会把 500 也放行，所以额外要求 <500（handlerErrOK 的除外）。
func TestRoutes_能力矩阵_有权限放行(t *testing.T) {
	r := setupTestRouter(t)
	for _, c := range gatedRoutes {
		allowed := 0
		for _, role := range matrixRoles {
			if !middleware.Can(role, c.cap) {
				continue
			}
			allowed++
			t.Run(role+"/"+c.method+" "+c.path, func(t *testing.T) {
				w := requestAs(t, r, c.method, concretePath(c.path), role)
				assert.NotEqual(t, http.StatusForbidden, w.Code,
					"角色 %q 有 %s 能力，不应被拒", role, c.cap)
				assert.NotEqual(t, http.StatusUnauthorized, w.Code)
				if !c.handlerErrOK {
					assert.Less(t, w.Code, 500, "不应是服务端错误")
				}
			})
		}
		require.NotZero(t, allowed, "%s %s 没有任何角色被放行：matrixRoles 是否漏了高权限角色？", c.method, c.path)
	}
}

// TestRoutes_所有路由都已分类 反向兜底（Risk#7）：枚举注册的**全部**路由，
// 任何一条既没挂能力、也不在 ungatedRoutes 白名单 → 失败。
//
// 为什么 GET 也要分类：本轮修的就是两个 GET（notification-channels 泄露明文凭据、
// diagnostics/ping 服务端外连），「新增敏感 GET 忘挂门禁」是同一类缺陷。
func TestRoutes_所有路由都已分类(t *testing.T) {
	r := setupTestRouter(t)

	gated := map[string]bool{}
	for _, c := range gatedRoutes {
		gated[c.method+" "+c.path] = true
	}

	for _, rt := range r.Routes() {
		switch rt.Method {
		case http.MethodHead, http.MethodOptions:
			continue
		}
		key := rt.Method + " " + rt.Path
		if gated[key] || ungatedRoutes[key] != "" {
			continue
		}
		t.Errorf("路由 %s 既未挂能力也未列入 ungatedRoutes：新增路由必须分类"+
			"（docs/FIX-PLAN-AUTHZ.md §3.3）", key)
	}
}

// TestRoutes_只读端点未被过度收紧 只读身份必须仍能读非凭据类端点（防过度收紧）。
func TestRoutes_只读端点未被过度收紧(t *testing.T) {
	r := setupTestRouter(t)
	for _, p := range []string{
		"/api/assets", "/api/alerts", "/api/tickets", "/api/racks", "/api/sites",
		"/api/dashboard/stats", "/api/integrations/status",
	} {
		t.Run(p, func(t *testing.T) {
			w := requestAs(t, r, http.MethodGet, p, middleware.RoleReadonly)
			assert.Equal(t, http.StatusOK, w.Code, "只读身份应能读 %s", p)
		})
	}

	// /api/topology 在测试 sqlite schema 下 500（testdata 的 assets 表没有 brand 列），
	// 这里只断言没被权限收紧 —— 状态码 200 属测试夹具缺口，不是鉴权问题。
	t.Run("/api/topology", func(t *testing.T) {
		w := requestAs(t, r, http.MethodGet, "/api/topology", middleware.RoleReadonly)
		assert.NotEqual(t, http.StatusForbidden, w.Code, "只读身份不应被拒")
	})

	// 凭据例外：通知渠道响应体含明文 webhook token / SMTP 密码 → 已收紧到 manage
	w := requestAs(t, r, http.MethodGet, "/api/notification-channels", middleware.RoleReadonly)
	assert.Equal(t, http.StatusForbidden, w.Code,
		"通知渠道含明文凭据，只读身份不应能读（docs/FIX-PLAN-AUTHZ.md §3.2）")
}

// seedUserWithRole 插一个真实用户行（/auth/me 需要 user_id 能查到用户）
func seedUserWithRole(t *testing.T, role string) string {
	t.Helper()
	id := uuid.NewString()
	err := database.GetDB().Exec(
		`INSERT INTO users (id, username, password_hash, role, status) VALUES (?, ?, ?, ?, 'active')`,
		id, "u-"+id[:8], "$2a$10$seed", role).Error
	require.NoError(t, err)
	return id
}

// TestRoutes_AuthMe下发capabilities 下发给前端的能力集必须与执行侧（middleware.Capabilities）
// 逐项一致，否则会出现「按钮隐藏但接口放行」的错位。
//
// 关键设计：DB 里的角色与 JWT 里的角色**故意不同**。若 handler 改成回查 DB
// （docs/FIX-PLAN-AUTHZ.md §4.4 明令禁止的做法），本测试会红 —— 这是「同源」的守护。
func TestRoutes_AuthMe下发capabilities(t *testing.T) {
	r := setupTestRouter(t)

	for _, role := range matrixRoles {
		t.Run("role="+role, func(t *testing.T) {
			decoy := middleware.RoleReadonly
			if role == middleware.RoleReadonly {
				decoy = middleware.RoleAdmin
			}
			userID := seedUserWithRole(t, decoy)
			tok, err := middleware.GenerateToken(userID, "me-"+role, role)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, "响应: %s", w.Body.String())

			var resp struct {
				Data struct {
					Role         string   `json:"role"`
					Capabilities []string `json:"capabilities"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

			want := []string{}
			for _, c := range middleware.Capabilities(role) {
				want = append(want, string(c))
			}
			assert.Equal(t, middleware.CanonicalRole(role), resp.Data.Role)
			assert.Equal(t, want, resp.Data.Capabilities)
		})
	}
}
