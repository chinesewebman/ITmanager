package api_test

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"network-monitor-platform/internal/api"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/integration"
	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/migrate"
	"network-monitor-platform/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// integrationTestBaseURL 非空时，loadTestConfigForRoutes 把 netbox/zabbix/glpi
// 三个地址全部指向它。「未拦」用例用它指向本进程的假服务——原用例会真实外连
// localhost:8000/8080/443，开发机/CI 上若恰好跑着 Zabbix、NetBox 或 443 服务，
// 测试会真的发起登录/同步并可能改写对方数据（一致性审计 F6）。
// 同包用例串行执行，配 t.Cleanup 复位即可。
var integrationTestBaseURL string

// integrationTestTrustedProxies 非空时由 loadTestConfigForRoutes 注入
// cfg.Server.TrustedProxies（G-7 用例）。格式校验在 config 包单测覆盖，
// 这里只关心 SetupRouter 如何消费它。同包用例串行，配 t.Cleanup 复位即可。
var integrationTestTrustedProxies []string

// rateLimitProbeSeq 让每次运行（含 `go test -count=N`）拿到不同的限流桶。
// 限流桶是进程级缓存，键 = `ClientIP()|FullPath`（middleware/rate_limit.go）；
// 固定源 IP 会让第二次运行从满桶开始，5×401 变成 429（一致性审计 F7）。
var rateLimitProbeSeq atomic.Int64

// probeIP 返回本次调用专属的源 IP（RFC 5737 文档段，端口不参与桶键）。
func probeIP() string {
	return fmt.Sprintf("203.0.113.%d:5000", int(rateLimitProbeSeq.Add(1))%250+1)
}

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
    url: "%s"
    token: ""
  zabbix:
    url: "%s"
    user: "Admin"
    password: "zabbix"
  glpi:
    url: "%s"
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
	// 三个集成地址由 integrationTestBaseURL 决定：空 = 历史默认（localhost 各端口），
	// 非空 = 指向本进程假服务，避免真实外连（一致性审计 F6）。
	base := integrationTestBaseURL
	if base == "" {
		yaml = fmt.Sprintf(yaml, "http://localhost:8000", "http://localhost:8080", "http://localhost")
	} else {
		yaml = fmt.Sprintf(yaml, base, base, base)
	}
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	// G-7：受信代理由用例注入（见 integrationTestTrustedProxies）
	cfg.Server.TrustedProxies = integrationTestTrustedProxies
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
	// 注：本表用随机 user_id（库里不存在），F-1 守卫会让 handler 返 404 ——
	// 因此这里只证明「identity 门禁放行 admin」，**不**证明「admin 能铸造成功」。
	// 后者的正例（真实 201）由 TestRoutes_APIKey不能管理APIKey 的铸造步骤承担。
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
	"POST /api/auth/skip-password-change": "自助跳过强改密（无待办时幂等，不写状态）",
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

// genTokenForUser 生成指定 user_id + 角色的 JWT（API Key 测试需要 Key 关联到真实用户）
func genTokenForUser(t *testing.T, userID, role string) string {
	t.Helper()
	tok, err := middleware.GenerateToken(userID, "user-"+role, role)
	require.NoError(t, err)
	return tok
}

// doJSONAsRaw 以指定 Authorization 头发一次请求（body=nil 时不带 body）
func doJSONAsRaw(t *testing.T, r *gin.Engine, method, path, authHeader string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doJSONAsRawFrom(t, r, method, path, authHeader, body, "")
}

// doJSONAsRawFrom 同上，但可指定 RemoteAddr。
// 登录限流的桶键是 `ClientIP()|FullPath`，同包用例共用默认 RemoteAddr 会互相消耗额度；
// 需要独立额度的用例（如 429 断言）用它固定自己的 IP。
//
// 可选 xff 参数（G-7 用例）：设置 X-Forwarded-For 头，用于验证「受信代理」语义。
func doJSONAsRawFrom(t *testing.T, r *gin.Engine, method, path, authHeader string, body any, remoteAddr string, xff ...string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	if len(xff) > 0 && xff[0] != "" {
		req.Header.Set("X-Forwarded-For", xff[0])
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// doJSONAs 以 Bearer token 发一次 JSON 请求
func doJSONAs(t *testing.T, r *gin.Engine, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doJSONAsRaw(t, r, method, path, "Bearer "+token, body)
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

// seedAPIKeyOwner 插一个可铸造 API Key 的 admin 用户，返回 user_id。
//
// must_change_password 必须显式为 0：默认值 TRUE（migration 000012）会让
// CreateAPIKey 提前 403，测不出目标中间件（强改密收窄另有专门用例覆盖）。
func seedAPIKeyOwner(t *testing.T, username string) string {
	t.Helper()
	id := uuid.NewString()
	require.NoError(t, database.GetDB().Exec(`INSERT INTO users
		(id, username, password_hash, role, status, failed_login, must_change_password, created_at, updated_at)
		VALUES (?, ?, 'x', 'admin', 'active', 0, 0, datetime('now'), datetime('now'))`,
		id, username).Error)
	return id
}

// mintWriteKeyViaSession 用登录会话铸一把 write scope Key，返回可直接用作
// Authorization 头的字符串（"X-API-Key <明文>"）。
//
// 铸造这一步本身必须成功——它是所有「Key 越权」用例的攻击前提，失败说明
// 测试环境而非被测中间件有问题，故用 require 而非 assert。
func mintWriteKeyViaSession(t *testing.T, r *gin.Engine, sessionToken, name string) string {
	t.Helper()
	w := doJSONAs(t, r, http.MethodPost, "/api/auth/api-keys", sessionToken, map[string]any{
		"name": name, "permissions": []string{"write"},
	})
	require.Equal(t, http.StatusCreated, w.Code, "会话铸造 Key 应 201: %s", w.Body.String())
	var created struct {
		Data struct {
			APIKey string `json:"api_key"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created.Data.APIKey, "响应必须回传一次性明文 Key")
	return "X-API-Key " + created.Data.APIKey
}

// TestRoutes_APIKey不能管理APIKey — AUTHZ 遗留缺陷 S-2（长期凭据不得自我复制）
//
// 必须用 **write scope** 的 Key 才复现攻击前提：read Key 打 POST/DELETE 会被
// apiKeyAllows 先挡下（403 "API Key 权限不足"），测不出新中间件。
// GET /api/auth/api-keys 是唯一「apiKeyAllows 会放行」的判别用例，必须覆盖。
// PUT /api/auth/password 同理：泄露的 write Key 可直接改掉所属账号的密码。
func TestRoutes_APIKey不能管理APIKey(t *testing.T) {
	r := setupTestRouter(t)

	uid := seedAPIKeyOwner(t, "key-owner")
	keyAuth := mintWriteKeyViaSession(t, r, genTokenForUser(t, uid, "admin"), "leaked")

	for _, c := range []struct{ name, method, path string }{
		{"GET 列举", http.MethodGet, "/api/auth/api-keys"},
		{"POST 铸造", http.MethodPost, "/api/auth/api-keys"},
		{"DELETE 吊销", http.MethodDelete, "/api/auth/api-keys/" + uuid.NewString()},
		{"PUT 吊销", http.MethodPut, "/api/auth/api-keys/" + uuid.NewString() + "/revoke"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var body any
			if c.method == http.MethodPost {
				body = map[string]any{"name": "copy", "permissions": []string{"write"}}
			}
			res := doJSONAsRaw(t, r, c.method, c.path, keyAuth, body)
			assert.Equal(t, http.StatusForbidden, res.Code, "API Key 不得管理 API Key")
			assert.Contains(t, res.Body.String(), "登录会话",
				"应命中新中间件的文案，而不是 apiKeyAllows 的「API Key 权限不足」")
		})
	}

	t.Run("PUT 改密", func(t *testing.T) {
		res := doJSONAsRaw(t, r, http.MethodPut, "/api/auth/password", keyAuth, map[string]string{
			"old_password": "x", "new_password": "newpass123",
		})
		assert.Equal(t, http.StatusForbidden, res.Code, "API Key 不得改密")
		assert.Contains(t, res.Body.String(), "登录会话")
	})
}

// TestRoutes_强改密窗口内不能铸造APIKey — 安全审计 F-1
//
// 攻击路径：seed 的 admin/admin123 未改密（must_change_password=1）→ 用默认口令
// 登录拿会话 → 立刻铸一把不过期的 Key → 即使之后改密，这把 Key 依然有效。
// 因此在路由层断言「强改密态下 POST /api/auth/api-keys 一律 403」。
func TestRoutes_强改密窗口内不能铸造APIKey(t *testing.T) {
	r := setupTestRouter(t)
	db := database.GetDB()

	uid := uuid.NewString()
	require.NoError(t, db.Exec(`INSERT INTO users
		(id, username, password_hash, role, status, failed_login, must_change_password, created_at, updated_at)
		VALUES (?, 'seed-admin', 'x', 'admin', 'active', 0, 1, datetime('now'), datetime('now'))`,
		uid).Error)
	token := genTokenForUser(t, uid, "admin")

	// 会话本身有效（能读自己的 Key 列表）——排除「403 是因为没登录」的假通过
	listRes := doJSONAs(t, r, http.MethodGet, "/api/auth/api-keys", token, nil)
	assert.Equal(t, http.StatusOK, listRes.Code, "会话应有效: %s", listRes.Body.String())

	res := doJSONAs(t, r, http.MethodPost, "/api/auth/api-keys", token, map[string]any{
		"name": "pre-change", "permissions": []string{"write"},
	})
	assert.Equal(t, http.StatusForbidden, res.Code, "强改密态下不得铸造 Key: %s", res.Body.String())
	assert.Contains(t, res.Body.String(), "强制改密")

	var n int64
	require.NoError(t, db.Table("api_keys").Where("user_id = ?", uid).Count(&n).Error)
	assert.Equal(t, int64(0), n, "强改密态下不得写入 api_keys")
}

// TestRoutes_跳过强改密留审计 — 回归保护（审计中-1）
//
// 该路由的**唯一**改动理由就是「挂到 protected 组拿 AuditLog」；若有人把它挪回
// auth 组，行为不变、路由分类测试也不会红，但审计留痕会静默消失。故在路由层断言。
func TestRoutes_跳过强改密留审计(t *testing.T) {
	r := setupTestRouter(t)
	db := database.GetDB()

	uid := uuid.NewString()
	require.NoError(t, db.Exec(`INSERT INTO users
		(id, username, password_hash, role, status, failed_login, must_change_password, created_at, updated_at)
		VALUES (?, 'audit-skip', 'x', 'admin', 'active', 0, 1, datetime('now'), datetime('now'))`,
		uid).Error)
	token := genTokenForUser(t, uid, "admin")

	// flag=true → 400（拒绝绕过），且必须留痕
	res := doJSONAs(t, r, http.MethodPost, "/api/auth/skip-password-change", token, nil)
	require.Equal(t, http.StatusBadRequest, res.Code, "body=%s", res.Body.String())

	var logs []models.AuditLog
	require.NoError(t, db.Where("path = ? AND method = ?",
		"/api/auth/skip-password-change", http.MethodPost).Find(&logs).Error)
	require.Len(t, logs, 1, "强改密绕过尝试必须留审计（AuditLog 中间件）")
	assert.Equal(t, http.StatusBadRequest, logs[0].Status)
}

// TestRoutes_APIKey不能读写信道与集成凭据 — FIX-PLAN-AUTHZ-CLOSURE.md §2 D-C（AV-1/AV-2/AV-3）
//
// 攻击前提：admin 会话铸的 write scope Key 继承所属用户的能力（middleware/auth.go:196），
// 于是这把「长期凭据」可以读走通知渠道里的明文 webhook token / SMTP 密码，
// 或把集成出站地址改指攻击者（F-3 / F-5）。
// 修复方式：这些端点额外挂 RejectAPIKeyAuth（只拒 API Key，不拒会话）。
func TestRoutes_APIKey不能读写信道与集成凭据(t *testing.T) {
	// 集成端点（/test、/sync）必须真的走到 handler 才能证明「没被新中间件拦」，
	// 而 handler 会外连集成地址。指向本进程假服务：既避免真实外连的副作用，
	// 也去掉网络环境带来的耗时抖动（一致性审计 F6）。
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"fake integration"}`))
	}))
	defer fake.Close()
	integrationTestBaseURL = fake.URL
	t.Cleanup(func() { integrationTestBaseURL = "" })

	r := setupTestRouter(t)
	uid := seedAPIKeyOwner(t, "closure-owner")
	keyAuth := mintWriteKeyViaSession(t, r, genTokenForUser(t, uid, "admin"), "leaked-write")

	// F-3：通知渠道整组（读也在内——响应体含明文凭据）
	for _, c := range []struct{ name, method, path string }{
		{"GET 列举渠道", http.MethodGet, "/api/notification-channels"},
		{"POST 建渠道", http.MethodPost, "/api/notification-channels"},
		{"PUT 改渠道", http.MethodPut, "/api/notification-channels/" + uuid.NewString()},
		{"DELETE 删渠道", http.MethodDelete, "/api/notification-channels/" + uuid.NewString()},
		{"PUT 测试渠道", http.MethodPut, "/api/notification-channels/" + uuid.NewString() + "/test"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := doJSONAsRaw(t, r, c.method, c.path, keyAuth, nil)
			assert.Equal(t, http.StatusForbidden, res.Code, "API Key 不得触碰通知渠道")
			assert.Contains(t, res.Body.String(), "登录会话",
				"应命中 RejectAPIKeyAuth 的文案，而不是能力矩阵的「权限不足」")
		})
	}

	// F-5：集成凭据的写端点（PUT 空字段会保留旧值 → 只改 URL 即可外带凭据）
	for _, path := range []string{
		"/api/integrations/zabbix", "/api/integrations/netbox", "/api/integrations/glpi",
	} {
		t.Run("PUT "+path, func(t *testing.T) {
			res := doJSONAsRaw(t, r, http.MethodPut, path, keyAuth,
				map[string]any{"url": "http://attacker.example"})
			assert.Equal(t, http.StatusForbidden, res.Code, "API Key 不得改集成出站地址")
			assert.Contains(t, res.Body.String(), "登录会话")
		})
	}

	// 防过度收紧：/test 与 /sync 不拦——堵住 PUT 后它们只能打管理员配置过的地址，
	// 是自动化该有的能力。不假设外连成败（无真实 Zabbix/NetBox，返回 5xx 也算通过）。
	//
	// 只断言「body 不含『登录会话』」是空转：路径拼错时 404 的 body 同样不含该文案
	// （一致性审计 F2）。故同时钉住状态码——既不是 403（被拦），也不是 404/405
	// （路由不存在/方法不对），保证请求真的走到了 handler。
	for _, path := range []string{
		"/api/integrations/zabbix/test", "/api/integrations/netbox/test",
		"/api/integrations/glpi/test", "/api/integrations/sync",
	} {
		t.Run("未拦 "+path, func(t *testing.T) {
			res := doJSONAsRaw(t, r, http.MethodPost, path, keyAuth, nil)
			assert.NotEqual(t, http.StatusForbidden, res.Code,
				"该端点不在本轮收紧范围，不应被 RejectAPIKeyAuth 拦下: %s", res.Body.String())
			assert.NotEqual(t, http.StatusNotFound, res.Code,
				"路由必须存在，否则「未拦」断言空转: %s", res.Body.String())
			assert.NotEqual(t, http.StatusMethodNotAllowed, res.Code,
				"方法必须匹配，否则「未拦」断言空转: %s", res.Body.String())
			assert.NotContains(t, res.Body.String(), "登录会话")
		})
	}

	// AV-3：会话身份行为不变（同一个 admin，同一批端点，不得被新中间件误伤）。
	// 断言 200 而不是「非 403」：401/404/405 也能通过 NotEqual 403，那样中间件被误挂
	// 到会话路由或路由改名时用例仍绿（一致性审计 F5）。
	token := genTokenForUser(t, uid, "admin")
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/notification-channels"},
		{http.MethodGet, "/api/integrations/status"},
	} {
		res := doJSONAs(t, r, c.method, c.path, token, nil)
		assert.Equal(t, http.StatusOK, res.Code,
			"会话访问 %s %s 应正常放行: %s", c.method, c.path, res.Body.String())
	}
}

// TestRoutes_登录尝试留审计 — FIX-PLAN-AUTHZ-CLOSURE.md §2 D-E（AV-7）
//
// 登录路由此前完全无留痕：既分不清爆破还是忘密码，也看不出谁在制造账户锁定。
// AuditLog 在未认证路由上只能拿到 IP，故 Login handler 把**尝试的用户名**放进 context。
// 同时断言密码绝不进审计——审计写的是元数据，任何形式的请求体都不落库。
func TestRoutes_登录尝试留审计(t *testing.T) {
	r := setupTestRouter(t)
	db := database.GetDB()

	const (
		username      = "audit-login"
		correctPasswd = "correct-pass-123"
		wrongPasswd   = "wrong-pass-456"
	)
	hash, err := bcrypt.GenerateFromPassword([]byte(correctPasswd), bcrypt.MinCost)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO users
		(id, username, password_hash, role, status, failed_login, must_change_password, created_at, updated_at)
		VALUES (?, ?, ?, 'admin', 'active', 0, 0, datetime('now'), datetime('now'))`,
		uuid.NewString(), username, string(hash)).Error)

	// 失败在前：AuditLog 默认同步写（AuditConfig.Async=false），响应返回时行已落库。
	// 源 IP 每次运行独立，避免 -count=N 时第二次已无登录额度（一致性审计 F7）。
	srcIP := probeIP()
	fail := doJSONAsRawFrom(t, r, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": username, "password": wrongPasswd,
	}, srcIP)
	require.Equal(t, http.StatusUnauthorized, fail.Code, "body=%s", fail.Body.String())

	ok := doJSONAsRawFrom(t, r, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": username, "password": correctPasswd,
	}, srcIP)
	require.Equal(t, http.StatusOK, ok.Code, "body=%s", ok.Body.String())

	// 按 username 过滤：同包其他用例也会打 /api/auth/login，只认本用例的两条
	var logs []models.AuditLog
	require.NoError(t, db.Where("path = ? AND method = ? AND username = ?",
		"/api/auth/login", http.MethodPost, username).Order("created_at").Find(&logs).Error)
	require.Len(t, logs, 2, "登录成功与失败必须各留一条审计")

	assert.Equal(t, http.StatusUnauthorized, logs[0].Status, "第一条是失败尝试")
	assert.Equal(t, http.StatusOK, logs[1].Status, "第二条是成功登录")
	for i, l := range logs {
		assert.Equal(t, username, l.Username, "第 %d 条审计必须带尝试的用户名", i)
		// 扫描**整行**而不是 ErrorMsg：`error_msg` 全仓没有写入者，只查它等于恒真
		// （安全审计 F4）。审计模型里根本没有 body 字段，这是「请求体不落库」的机制保证。
		row := fmt.Sprintf("%+v", l)
		assert.NotContains(t, row, wrongPasswd, "密码不得进审计")
		assert.NotContains(t, row, correctPasswd, "密码不得进审计")
	}
}

// TestRoutes_限流的登录请求不写审计 — 安全审计 F1
//
// AuditLog 在 c.Next() 之后**无条件**写库，若排在 RateLimit 之前，被 429 拒掉的
// 请求照样 INSERT：未认证请求即可无限写库（实测 10 次请求 → 5×401 + 5×429，
// 但审计 10 行）。这里断言「第 6 次起不再落库」——顺序换回去即红。
func TestRoutes_限流的登录请求不写审计(t *testing.T) {
	r := setupTestRouter(t)
	db := database.GetDB()

	attackerIP := probeIP() // 每次运行独立的桶，避免 -count=N 复用满桶
	before := auditLoginCount(t, db)

	codes := map[int]int{}
	for i := 0; i < 7; i++ {
		w := doJSONAsRawFrom(t, r, http.MethodPost, "/api/auth/login", "", map[string]string{
			"username": "rate-limit-probe", "password": "x",
		}, attackerIP)
		codes[w.Code]++
	}
	require.Equal(t, 5, codes[http.StatusUnauthorized], "前 5 次应被处理（401）")
	require.Equal(t, 2, codes[http.StatusTooManyRequests], "第 6、7 次应被限流")

	after := auditLoginCount(t, db)
	assert.Equal(t, 5, after-before,
		"只有被处理的 5 次该落库；429 也落库 = 未认证请求可无限写库（审计 F1）")
}

// auditLoginCount 统计登录路径的审计行数（用整表计数做增量，避免依赖 username）
func auditLoginCount(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&models.AuditLog{}).
		Where("path = ? AND method = ?", "/api/auth/login", http.MethodPost).Count(&n).Error)
	return int(n)
}

// TestRoutes_登录审计用户名被净化 — 安全审计 F3
//
// 审计行是行式消费的（SIEM / 导出 CSV），请求体里的换行/控制字符能把一行伪造成
// 多条记录。这里用真实请求走完整链路，断言落库值不含控制字符、且超长值被按 rune 截断。
func TestRoutes_登录审计用户名被净化(t *testing.T) {
	r := setupTestRouter(t)
	db := database.GetDB()

	injected := "evil\n[OK] LOGIN SUCCESS user=root\x00" + strings.Repeat("A", 300)
	srcIP := probeIP()
	w := doJSONAsRawFrom(t, r, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": injected, "password": "x",
	}, srcIP)
	require.Equal(t, http.StatusUnauthorized, w.Code, "body=%s", w.Body.String())

	var log models.AuditLog
	require.NoError(t, db.Where("path = ? AND ip = ?", "/api/auth/login", strings.Split(srcIP, ":")[0]).
		First(&log).Error, "该请求必须留痕（否则本用例空转）")

	assert.NotContains(t, log.Username, "\n", "换行不得入库（可伪造多条记录）")
	assert.NotContains(t, log.Username, "\x00", "NUL 不得入库")
	for _, r := range log.Username {
		require.GreaterOrEqual(t, r, rune(0x20), "不得含控制字符: %q", log.Username)
	}
	assert.LessOrEqual(t, len(log.Username), 96, "须在 audit.go 的 100 字节截断之前收敛")
	assert.True(t, utf8.ValidString(log.Username), "不得切出非法 UTF-8: %q", log.Username)
	assert.Contains(t, log.Username, "[OK] LOGIN SUCCESS", "可打印内容应保留，只剥控制字符")
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

// ==================== G-7：受信代理（X-Forwarded-For 可信边界） ====================
//
// 修复前：gin 默认信任 0.0.0.0/0，ClientIP() 取 XFF 最左值 —— 攻击者每请求换一个
// XFF 即可绕过登录限流、伪造审计 IP、绕过 API Key 的 IP 白名单（安全审计 F1①）。
// 见 docs/FIX-PLAN-TRUSTED-PROXY.md。

// mintWriteKeyWithWhitelist 铸一把带 IP 白名单的 write scope Key（G-7 V-8 用）。
func mintWriteKeyWithWhitelist(t *testing.T, r *gin.Engine, sessionToken, name string, whitelist []string) string {
	t.Helper()
	w := doJSONAs(t, r, http.MethodPost, "/api/auth/api-keys", sessionToken, map[string]any{
		"name": name, "permissions": []string{"write"}, "ip_whitelist": whitelist,
	})
	require.Equal(t, http.StatusCreated, w.Code, "会话铸造带白名单的 Key 应 201: %s", w.Body.String())
	var created struct {
		Data struct {
			APIKey string `json:"api_key"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created.Data.APIKey, "响应必须回传一次性明文 Key")
	return "X-API-Key " + created.Data.APIKey
}

// TestRoutes_未配受信代理时忽略XFF — G-7 V-1
//
// trusted_proxies 为空 = 不信任任何来源，ClientIP() 取直连对端。
// 断言方式：同一 RemoteAddr + 每次不同的 XFF，打满 5 次登录额度后第 6 次必须 429。
// 若 XFF 被采信（修复前的 gin 默认行为），每个 XFF 各自成桶，第 6 次不会 429。
func TestRoutes_未配受信代理时忽略XFF(t *testing.T) {
	integrationTestTrustedProxies = nil
	t.Cleanup(func() { integrationTestTrustedProxies = nil })
	r := setupTestRouter(t)

	srcIP := probeIP() // 直连对端；每次运行独立，避免 -count=N 复用满桶
	for i := 0; i < 5; i++ {
		w := doJSONAsRawFrom(t, r, http.MethodPost, "/api/auth/login", "", map[string]string{
			"username": "xff-probe", "password": "wrong",
		}, srcIP, fmt.Sprintf("198.51.100.%d", i))
		require.NotEqual(t, http.StatusTooManyRequests, w.Code,
			"第 %d 次应在额度内（XFF 必须被忽略）", i+1)
	}
	w := doJSONAsRawFrom(t, r, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": "xff-probe", "password": "wrong",
	}, srcIP, "198.51.100.99")
	assert.Equal(t, http.StatusTooManyRequests, w.Code,
		"第 6 次必须 429：桶键用的是直连对端，不是攻击者可控的 XFF")
}

// TestRoutes_受信代理下审计IP取最右不可信跳 — G-7 V-2 / V-5
//
// 受信代理语义（gin validateHeader）：从 XFF 最右往左跳过受信代理，取第一个不可信 IP。
// 攻击者伪造最左值 + nginx 追加真实 $remote_addr → 必须落到真实客户端。
func TestRoutes_受信代理下审计IP取最右不可信跳(t *testing.T) {
	integrationTestTrustedProxies = []string{"203.0.113.0/24"}
	t.Cleanup(func() { integrationTestTrustedProxies = nil })
	r := setupTestRouter(t)
	db := database.GetDB()

	// 用户名/IP 每次运行独立，避免 -count=N 时查询命中上一轮的审计行
	seq := int(rateLimitProbeSeq.Add(1))
	username := fmt.Sprintf("xff-audit-%d", seq)
	realClient := fmt.Sprintf("198.51.100.%d", seq%250+1) // 不在受信网段内
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-pass-123"), bcrypt.MinCost)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO users
		(id, username, password_hash, role, status, failed_login, must_change_password, created_at, updated_at)
		VALUES (?, ?, ?, 'admin', 'active', 0, 0, datetime('now'), datetime('now'))`,
		uuid.NewString(), username, string(hash)).Error)

	w := doJSONAsRawFrom(t, r, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": username, "password": "wrong-pass",
	}, "203.0.113.7:5000", "1.2.3.4, "+realClient) // 直连对端 = 受信代理；左值由攻击者伪造
	require.Equal(t, http.StatusUnauthorized, w.Code, "body=%s", w.Body.String())

	var entry models.AuditLog
	require.NoError(t, db.Where("path = ? AND username = ?", "/api/auth/login", username).First(&entry).Error)
	assert.Equal(t, realClient, entry.IP,
		"审计 IP 必须是 XFF 最右不可信跳（真实客户端），不是最左伪造值，也不是代理地址")
}

// TestRoutes_受信代理配置非法时failClosed — G-7 R-2
//
// SetTrustedProxies 出错时 gin 仍会把**已解析成功的部分** CIDR 写进 engine
// （/tmp 探针实测：["203.0.113.0/24","bogus"] → err 非 nil，但 203.0.113.0/24 已生效，
// ClientIP() 采信 XFF）。不 fail-closed 的话，这份「半截信任表」会让该网段的对端
// 可以伪造 XFF —— 比完全不配更危险。断言：配了非法条目时行为必须与空配置一致。
func TestRoutes_受信代理配置非法时failClosed(t *testing.T) {
	// 每次运行用不同网段/对端，避免 -count=N 复用上一轮已打满的限流桶
	seq := int(rateLimitProbeSeq.Add(1)) % 200
	proxyNet := fmt.Sprintf("203.0.%d.0/24", seq+50)
	srcIP := fmt.Sprintf("203.0.%d.7:5000", seq+50)
	integrationTestTrustedProxies = []string{proxyNet, "bogus"}
	t.Cleanup(func() { integrationTestTrustedProxies = nil })
	r := setupTestRouter(t)

	for i := 0; i < 5; i++ {
		w := doJSONAsRawFrom(t, r, http.MethodPost, "/api/auth/login", "", map[string]string{
			"username": "xff-failclosed", "password": "wrong",
		}, srcIP, fmt.Sprintf("198.51.100.%d", i))
		require.NotEqual(t, http.StatusTooManyRequests, w.Code,
			"第 %d 次应在额度内（配置非法 → 降级为不信任任何来源，XFF 必须被忽略）", i+1)
	}
	w := doJSONAsRawFrom(t, r, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": "xff-failclosed", "password": "wrong",
	}, srcIP, "198.51.100.99")
	assert.Equal(t, http.StatusTooManyRequests, w.Code,
		"第 6 次必须 429：半截信任表不得生效（否则该网段对端可伪造 XFF 逐请求换桶）")
}

// TestRoutes_APIKey白名单按真实客户端IP判定 — G-7 V-8
//
// IP 白名单是安全控制而非仅日志字段：修复前伪造 XFF 即可从任意 IP 使用持白名单的 Key。
// 配好受信代理后，白名单必须按**真实客户端**判定。
func TestRoutes_APIKey白名单按真实客户端IP判定(t *testing.T) {
	integrationTestTrustedProxies = []string{"203.0.113.0/24"}
	t.Cleanup(func() { integrationTestTrustedProxies = nil })
	r := setupTestRouter(t)

	uid := seedAPIKeyOwner(t, "xff-key-owner")
	session := genTokenForUser(t, uid, "admin")
	realClient := fmt.Sprintf("198.51.100.%d", int(rateLimitProbeSeq.Add(1))%250+1)
	const proxyAddr = "203.0.113.7:5000" // 受信代理
	// 攻击者伪造最左值 + nginx 追加真实 $remote_addr（frontend/nginx.conf:19-20）。
	// 带伪造值才能区分「取最左」与「取最右不可信跳」——只发真实 IP 时两者恰好相同。
	spoofedXFF := "1.2.3.4, " + realClient

	// 白名单 = 真实客户端 → 放行（证明 ClientIP 采信了 XFF 最右不可信跳）
	keyForClient := mintWriteKeyWithWhitelist(t, r, session, "wl-client", []string{realClient})
	w := doJSONAsRawFrom(t, r, http.MethodGet, "/api/assets", keyForClient, nil, proxyAddr, spoofedXFF)
	assert.Equal(t, http.StatusOK, w.Code,
		"白名单命中真实客户端 IP 应放行，实际: %s", w.Body.String())

	// 白名单 = 代理地址 → 403（证明 ClientIP 不是代理地址，白名单没被架空）
	keyForProxy := mintWriteKeyWithWhitelist(t, r, session, "wl-proxy", []string{"203.0.113.7"})
	w = doJSONAsRawFrom(t, r, http.MethodGet, "/api/assets", keyForProxy, nil, proxyAddr, spoofedXFF)
	assert.Equal(t, http.StatusForbidden, w.Code,
		"ClientIP 应是真实客户端而非代理地址，白名单只含代理时必须 403")
}
