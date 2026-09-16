package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"network-monitor-platform/internal/api/handlers"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/integration"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 注：integration handler 用具体类型 *integration.IntegrationService
// （非 interface），完整 mock 需重构成 interface 或 sqlmock 模拟 DB
// 本文件只测不依赖 svc 的 GetIntegrationStatus 路由

func newIntegrationTestRouter(cfg *config.Config) *gin.Engine {
	return newIntegrationTestRouterWithRole(cfg, "")
}

// newIntegrationTestRouterWithRole 同 newIntegrationTestRouter，但在注册路由前注入指定 role
// （模拟 auth 中间件 c.Set("role", …)）。role 为空则走 fail-safe 只读地板。
// 注：gin 的 Engine.Use 只作用于之后注册的路由，所以必须在这里（g.GET 之前）注入。
func newIntegrationTestRouterWithRole(cfg *config.Config, role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery()) // 把 svc=nil 触发的 panic 转 500（不让它穿透 testing.tRunner）
	if role != "" {
		r.Use(func(c *gin.Context) { c.Set("role", role); c.Next() })
	}
	h := handlers.NewIntegrationHandler(nil, cfg) // nil svc，Sync 路由不能实际调用
	g := r.Group("/integrations")
	g.POST("/sync", h.Sync)
	g.GET("/status", h.GetIntegrationStatus)
	// v2.2: 三个集成的配置 + 连通测试
	g.PUT("/zabbix", h.UpdateZabbix)
	g.POST("/zabbix/test", h.TestZabbix)
	g.PUT("/netbox", h.UpdateNetBox)
	g.POST("/netbox/test", h.TestNetBox)
	g.PUT("/glpi", h.UpdateGLPI)
	g.POST("/glpi/test", h.TestGLPI)
	return r
}

func minimalCfgForTest(netboxURL, zabbixURL, glpiURL string) *config.Config {
	return &config.Config{
		Integrations: config.IntegrationsConfig{
			Netbox: config.NetboxConfig{URL: netboxURL},
			Zabbix: config.ZabbixConfig{URL: zabbixURL},
			GLPI:   config.GLPIConfig{URL: glpiURL},
		},
	}
}

// ==================== GetIntegrationStatus ====================

// TestIntegrationStatus_ThreeIntegrationsReturnURL M86：url 仅 canManage 可见，用 admin 角色断言。
// 收紧前默认 fail-safe 角色（""）即可看到 url；收紧后必须显式用 admin / ops_admin 才能看到。
// M85 的 cmd/set-role 同款「收紧即收测试角色」范本。
func TestIntegrationStatus_ThreeIntegrationsReturnURL(t *testing.T) {
	cfg := minimalCfgForTest("http://netbox.local", "http://zabbix.local", "http://glpi.local")
	r := newIntegrationTestRouterWithRole(cfg, "admin")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"netbox":{`)
	assert.Contains(t, body, `"enabled":true`)
	assert.Contains(t, body, `"url":"http://netbox.local"`)
	assert.Contains(t, body, `"zabbix":{`)
	assert.Contains(t, body, `"url":"http://zabbix.local"`)
	assert.Contains(t, body, `"glpi":{`)
	assert.Contains(t, body, `"url":"http://glpi.local"`)
}

func TestIntegrationStatus_EmptyURL_EnabledFalse(t *testing.T) {
	cfg := minimalCfgForTest("", "", "")
	r := newIntegrationTestRouter(cfg)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"enabled":false`)
}

func TestIntegrationStatus_MixedConfig_PartialEnabled(t *testing.T) {
	cfg := minimalCfgForTest("http://netbox.local", "", "http://glpi.local")
	r := newIntegrationTestRouter(cfg)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	netbox := data["netbox"].(map[string]interface{})
	zabbix := data["zabbix"].(map[string]interface{})
	glpi := data["glpi"].(map[string]interface{})
	assert.Equal(t, true, netbox["enabled"])
	assert.Equal(t, false, zabbix["enabled"])
	assert.Equal(t, true, glpi["enabled"])
}

func TestIntegrationStatus_ResponseCodeZero(t *testing.T) {
	cfg := minimalCfgForTest("", "", "")
	r := newIntegrationTestRouter(cfg)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.EqualValues(t, 0, resp["code"])
}

func TestIntegrationStatus_不泄露Secret字段(t *testing.T) {
	// 即使 config 里有 token / password，status 路由不应返回
	cfg := minimalCfgForTest("http://netbox.local", "http://zabbix.local", "http://glpi.local")
	cfg.Integrations.Netbox.Token = "super-secret-netbox-token-123"
	cfg.Integrations.Zabbix.User = "zabbix-user"
	cfg.Integrations.Zabbix.Password = "zabbix-pwd"
	cfg.Integrations.GLPI.AppToken = "glpi-app-tok"
	cfg.Integrations.GLPI.UserToken = "glpi-user-tok"

	r := newIntegrationTestRouter(cfg)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))

	body := w.Body.String()
	assert.NotContains(t, body, "super-secret-netbox-token")
	assert.NotContains(t, body, "zabbix-pwd")
	assert.NotContains(t, body, "glpi-app-tok")
	assert.NotContains(t, body, "glpi-user-tok")
	// 也不应出现 "token" / "password" 字段
	assert.NotContains(t, body, `"token"`)
	assert.NotContains(t, body, `"password"`)
}

// P2-1/Pre-1：readonly/auditor/user 看凭据存在性（has_*）被拒；canManage（admin/ops_admin）看凭据存在性。
// M86 扩展：url/user 也按 canManage 分级（同款分级范本）。
// 配置详情（url / user）单独由 TestIntegrationStatus_URL与ZabbixUser仅canManage可见 钉死。
func TestIntegrationStatus_凭据存在性仅canManage可见(t *testing.T) {
	cfg := minimalCfgForTest("http://netbox.local", "http://zabbix.local", "http://glpi.local")
	cfg.Integrations.Netbox.Token = "tok"
	cfg.Integrations.Zabbix.User = "zbx-user"
	cfg.Integrations.Zabbix.Password = "pwd"
	cfg.Integrations.GLPI.AppToken = "app"
	cfg.Integrations.GLPI.UserToken = "user"

	cases := []struct {
		role    string
		wantHas bool
	}{
		{"admin", true},
		{"ops_admin", true},
		{"ops_user", false},
		{"auditor", false},
		{"readonly", false},
		{"user", false},
		{"", false}, // 未知/空角色 → fail-safe 只读地板
	}

	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			r := newIntegrationTestRouterWithRole(cfg, tc.role)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))
			require.Equal(t, http.StatusOK, w.Code)

			var resp map[string]interface{}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			data := resp["data"].(map[string]interface{})
			netbox := data["netbox"].(map[string]interface{})
			zabbix := data["zabbix"].(map[string]interface{})
			glpi := data["glpi"].(map[string]interface{})

			// 凭据存在性（has_*）仅 canManage 可见
			_, nbHas := netbox["has_token"]
			_, zbxHas := zabbix["has_password"]
			_, glpiApp := glpi["has_app_token"]
			_, glpiUser := glpi["has_user_token"]
			assert.Equal(t, tc.wantHas, nbHas, "netbox.has_token")
			assert.Equal(t, tc.wantHas, zbxHas, "zabbix.has_password")
			assert.Equal(t, tc.wantHas, glpiApp, "glpi.has_app_token")
			assert.Equal(t, tc.wantHas, glpiUser, "glpi.has_user_token")
		})
	}
}

// M86：url / user 与 P2-1 has_* 同款按 canManage 分级。
// 7 角色 × 3 字段 (netbox.url / zabbix.user / glpi.url)：
// - admin / ops_admin (canManage=true) → 字段可见
// - ops_user / auditor / readonly / user / 空 (canManage=false) → 字段不可见 (非 canManage 只看 enabled)
//
// mutation M1 反证：临时把 url/user 移出 `if canManage` 块 → 5 角色断言失败 (字段"被看见") + admin/ops_admin 仍绿。
func TestIntegrationStatus_URL与ZabbixUser仅canManage可见(t *testing.T) {
	cfg := minimalCfgForTest("http://netbox.local", "http://zabbix.local", "http://glpi.local")
	cfg.Integrations.Zabbix.User = "zbx-admin"

	cases := []struct {
		role     string
		canManage bool
	}{
		{"admin", true},
		{"ops_admin", true},
		{"ops_user", false},
		{"auditor", false},
		{"readonly", false},
		{"user", false},
		{"", false}, // 未知/空角色 → fail-safe 只读地板
	}

	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			r := newIntegrationTestRouterWithRole(cfg, tc.role)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))
			require.Equal(t, http.StatusOK, w.Code)

			var resp map[string]interface{}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			data := resp["data"].(map[string]interface{})
			netbox := data["netbox"].(map[string]interface{})
			zabbix := data["zabbix"].(map[string]interface{})
			glpi := data["glpi"].(map[string]interface{})

			// enabled 永远可见（读地板，不含任何凭据/拓扑信息）
			assert.Contains(t, netbox, "enabled")
			assert.Contains(t, zabbix, "enabled")
			assert.Contains(t, glpi, "enabled")

			// url / user 按 canManage 分级
			_, nbURL := netbox["url"]
			_, zbxURL := zabbix["url"]
			_, zbxUser := zabbix["user"]
			_, glpiURL := glpi["url"]
			assert.Equal(t, tc.canManage, nbURL, "netbox.url")
			assert.Equal(t, tc.canManage, zbxURL, "zabbix.url")
			assert.Equal(t, tc.canManage, zbxUser, "zabbix.user")
			assert.Equal(t, tc.canManage, glpiURL, "glpi.url")
		})
	}
}

// M86：URL 字面不暴露。即使 key 不在 map 里，也要确认响应 body 不含 URL 字符串本身
// （防 G-28 同款脱敏绕过：字段名改 / URL 出现在 debug log / 错误回显等非响应键路径）。
// readonly 角色配置 netbox.url="http://secret-netbox:8000"，断言响应不含该字符串。
func TestIntegrationStatus_URL与User不可见_不暴露配置拓扑(t *testing.T) {
	cfg := minimalCfgForTest("http://secret-netbox:8000", "http://secret-zabbix:8080", "http://secret-glpi:80")
	cfg.Integrations.Zabbix.User = "secret-zbx-user"
	r := newIntegrationTestRouterWithRole(cfg, "readonly")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))
	require.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	// readonly 响应里不应含任何 URL 字面或 Zabbix 用户名字面
	assert.NotContains(t, body, "secret-netbox:8000")
	assert.NotContains(t, body, "secret-zabbix:8080")
	assert.NotContains(t, body, "secret-glpi")
	assert.NotContains(t, body, "secret-zbx-user")
	// 也不应出现 "url" / "user" 键（canManage=false 时不暴露）
	assert.NotContains(t, body, `"url"`)
	assert.NotContains(t, body, `"user"`)
}

// TestSync_非法Type_返400 — BUG#7
//
//	之前 type="garbage" 静默走 default 分支（=SyncAll），用户不知道输错了
//	修复：严格 switch，非法 type 直接 400
func TestSync_非法Type_返400(t *testing.T) {
	cfg := minimalCfgForTest("", "", "")
	r := newIntegrationTestRouter(cfg)

	tests := []string{"garbage", "Netbox", "NETBOX", "unknown", "syncc"}
	for _, ty := range tests {
		t.Run("type="+ty, func(t *testing.T) {
			body := []byte(`{"type":"` + ty + `"}`)
			req := httptest.NewRequest("POST", "/integrations/sync", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, "非法 type=%q 必须 400", ty)
		})
	}
}

// TestSync_合法Type_通过校验 — BUG#7 正向
//
//	合法值 netbox/zabbix/glpi/all/"" 必须过校验（虽然 svc=nil 会 panic，
//	但 400 校验在 panic 前发生，所以应该看到 panic 而非 400）
func TestSync_合法Type_通过校验(t *testing.T) {
	// 这个 test 只验证 type 校验通过；svc=nil 会 panic
	// 用 defer recover 抓 panic 来确认"过校验 + 走 svc 调用"
	cfg := minimalCfgForTest("", "", "")
	r := newIntegrationTestRouter(cfg)

	for _, ty := range []string{"netbox", "zabbix", "glpi", "zabbix_metrics", "all", ""} {
		t.Run("type="+ty, func(t *testing.T) {
			defer func() {
				_ = recover() // svc=nil 必然 panic，过校验即可
			}()
			body := []byte(`{"type":"` + ty + `"}`)
			req := httptest.NewRequest("POST", "/integrations/sync", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			// 不应返 400
			assert.NotEqual(t, http.StatusBadRequest, w.Code, "合法 type=%q 不该 400", ty)
		})
	}
}

// ==================== v2.2: Zabbix Status 新字段 ====================

// TestIntegrationStatus_Zabbix_HasUserAndHasPassword v2.2: status 返回 user + has_password（不返明文 password）。
func TestIntegrationStatus_Zabbix_HasUserAndHasPassword(t *testing.T) {
	cfg := minimalCfgForTest("", "http://zabbix.local", "")
	cfg.Integrations.Zabbix.User = "Admin"
	cfg.Integrations.Zabbix.Password = "zabbix"
	r := newIntegrationTestRouterWithRole(cfg, "admin")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"user":"Admin"`)
	assert.Contains(t, body, `"has_password":true`)
	// 不应回显明文 password
	assert.NotContains(t, body, `"password":"zabbix"`)
}

// TestIntegrationStatus_Zabbix_NoPassword v2.2: 未配置密码时 has_password=false。
func TestIntegrationStatus_Zabbix_NoPassword(t *testing.T) {
	cfg := minimalCfgForTest("", "http://zabbix.local", "")
	cfg.Integrations.Zabbix.User = "Admin"
	cfg.Integrations.Zabbix.Password = ""
	r := newIntegrationTestRouterWithRole(cfg, "admin")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"has_password":false`)
}

// ==================== v2.2: UpdateZabbix ====================

// TestUpdateZabbix_缺URL_返400 v2.2: 缺 url 必返 400。
func TestUpdateZabbix_缺URL_返400(t *testing.T) {
	cfg := minimalCfgForTest("", "", "")
	r := newIntegrationTestRouter(cfg)

	body := []byte(`{"user":"Admin","password":"newpass"}`)
	req := httptest.NewRequest(http.MethodPut, "/integrations/zabbix", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestUpdateZabbix_缺User_返400 v2.2: 缺 user 必返 400。
func TestUpdateZabbix_缺User_返400(t *testing.T) {
	cfg := minimalCfgForTest("", "", "")
	r := newIntegrationTestRouter(cfg)

	body := []byte(`{"url":"http://x","password":"newpass"}`)
	req := httptest.NewRequest(http.MethodPut, "/integrations/zabbix", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestUpdateZabbix_OK_内存Cfg已更新 v2.2: 合法请求 200 且 cfg.Integrations.Zabbix 同步更新。
// 注：svc=nil 会让 ReloadZabbix panic，但 cfg 写入发生在 Reload 之前。
// 验证方法：用 httptest server + 直接跳过 panic（gin.Recovery 会把 panic 转 500）。
func TestUpdateZabbix_OK_内存Cfg已更新(t *testing.T) {
	cfg := minimalCfgForTest("", "http://old", "")
	cfg.Integrations.Zabbix.User = "old"
	cfg.Integrations.Zabbix.Password = "oldpass"
	r := newIntegrationTestRouter(cfg)

	body := []byte(`{"url":"http://new-zabbix:8080","user":"new-admin","password":"newpass"}`)
	req := httptest.NewRequest(http.MethodPut, "/integrations/zabbix", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// 不期待 200：svc=nil 会 panic → gin.Recovery 转 500。但 cfg 在 panic 前已改写。
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, "svc=nil 应 panic → gin 转 500")
	// 关键断言：panic 前 cfg 已被改写（handler 顺序：cfg 写入在 ReloadZabbix 之前）
	assert.Equal(t, "http://new-zabbix:8080", cfg.Integrations.Zabbix.URL)
	assert.Equal(t, "new-admin", cfg.Integrations.Zabbix.User)
	assert.Equal(t, "newpass", cfg.Integrations.Zabbix.Password)
}

// TestUpdateZabbix_空Password_保留旧值 v2.2: password 字段为空时保留 cfg 旧值（避免 UI 误清空）。
func TestUpdateZabbix_空Password_保留旧值(t *testing.T) {
	cfg := minimalCfgForTest("", "http://old", "")
	cfg.Integrations.Zabbix.User = "old"
	cfg.Integrations.Zabbix.Password = "oldpass-preserved"
	r := newIntegrationTestRouter(cfg)

	body := []byte(`{"url":"http://new","user":"new","password":""}`)
	req := httptest.NewRequest(http.MethodPut, "/integrations/zabbix", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// password="" → handler 应保留 "oldpass-preserved"，所以 cfg 写入也是新 URL+新 user+旧 password
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, "svc=nil 应 panic → gin 转 500")
	assert.Equal(t, "http://new", cfg.Integrations.Zabbix.URL)
	assert.Equal(t, "new", cfg.Integrations.Zabbix.User)
	assert.Equal(t, "oldpass-preserved", cfg.Integrations.Zabbix.Password, "空 password 必须保留旧值")
}

// ==================== v2.2: TestZabbix 连通测试 ====================

// TestTestZabbix_svcNil_返500 v2.2: svc=nil 时连通测试必 panic → 500（保证调用 svc 不会假成功）。
func TestTestZabbix_svcNil_返500(t *testing.T) {
	cfg := minimalCfgForTest("", "", "")
	r := newIntegrationTestRouter(cfg)

	req := httptest.NewRequest(http.MethodPost, "/integrations/zabbix/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// svc.TestZabbixConnection → svc.zabbix.Login → nil pointer panic → gin.Recovery → 500
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ==================== v2.2: NetBox/GLPI Status 字段 ====================

// TestIntegrationStatus_NetBox_HasToken v2.2: netbox.has_token 正确反映。
func TestIntegrationStatus_NetBox_HasToken(t *testing.T) {
	cfg := minimalCfgForTest("http://netbox.local", "", "")
	cfg.Integrations.Netbox.Token = "real-token-xyz"
	r := newIntegrationTestRouterWithRole(cfg, "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"has_token":true`)
	assert.NotContains(t, body, `"token":"real-token-xyz"`)
}

func TestIntegrationStatus_NetBox_NoToken(t *testing.T) {
	cfg := minimalCfgForTest("http://netbox.local", "", "")
	r := newIntegrationTestRouterWithRole(cfg, "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))
	assert.Contains(t, w.Body.String(), `"has_token":false`)
}

// TestIntegrationStatus_GLPI_HasTokens v2.2: GLPI 双 token 各自独立反映。
func TestIntegrationStatus_GLPI_HasTokens(t *testing.T) {
	cfg := minimalCfgForTest("", "", "http://glpi.local")
	cfg.Integrations.GLPI.AppToken = "app-tok"
	cfg.Integrations.GLPI.UserToken = "" // 只有 app
	r := newIntegrationTestRouterWithRole(cfg, "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))
	body := w.Body.String()
	assert.Contains(t, body, `"has_app_token":true`)
	assert.Contains(t, body, `"has_user_token":false`)
	assert.NotContains(t, body, `"app_token":"app-tok"`)
}

// ==================== v2.2: NetBox Update ====================

func TestUpdateNetBox_缺URL_返400(t *testing.T) {
	cfg := minimalCfgForTest("", "", "")
	r := newIntegrationTestRouter(cfg)
	body := []byte(`{"token":"abc"}`)
	req := httptest.NewRequest(http.MethodPut, "/integrations/netbox", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateNetBox_OK_内存Cfg已更新(t *testing.T) {
	cfg := minimalCfgForTest("http://old", "", "")
	cfg.Integrations.Netbox.Token = "old-token"
	r := newIntegrationTestRouter(cfg)
	body := []byte(`{"url":"http://new-netbox:8000","token":"new-token"}`)
	req := httptest.NewRequest(http.MethodPut, "/integrations/netbox", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, "svc=nil 应 panic → gin 转 500")
	assert.Equal(t, "http://new-netbox:8000", cfg.Integrations.Netbox.URL)
	assert.Equal(t, "new-token", cfg.Integrations.Netbox.Token)
}

func TestUpdateNetBox_空Token_保留旧值(t *testing.T) {
	cfg := minimalCfgForTest("http://old", "", "")
	cfg.Integrations.Netbox.Token = "preserved-token"
	r := newIntegrationTestRouter(cfg)
	body := []byte(`{"url":"http://new","token":""}`)
	req := httptest.NewRequest(http.MethodPut, "/integrations/netbox", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "preserved-token", cfg.Integrations.Netbox.Token, "空 token 必须保留旧值")
}

// ==================== v2.2: GLPI Update ====================

func TestUpdateGLPI_缺URL_返400(t *testing.T) {
	cfg := minimalCfgForTest("", "", "")
	r := newIntegrationTestRouter(cfg)
	body := []byte(`{"app_token":"a","user_token":"b"}`)
	req := httptest.NewRequest(http.MethodPut, "/integrations/glpi", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateGLPI_OK_内存Cfg已更新(t *testing.T) {
	cfg := minimalCfgForTest("", "", "http://old")
	cfg.Integrations.GLPI.AppToken = "old-app"
	cfg.Integrations.GLPI.UserToken = "old-user"
	r := newIntegrationTestRouter(cfg)
	body := []byte(`{"url":"http://new-glpi:80","app_token":"new-app","user_token":"new-user"}`)
	req := httptest.NewRequest(http.MethodPut, "/integrations/glpi", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "http://new-glpi:80", cfg.Integrations.GLPI.URL)
	assert.Equal(t, "new-app", cfg.Integrations.GLPI.AppToken)
	assert.Equal(t, "new-user", cfg.Integrations.GLPI.UserToken)
}

func TestUpdateGLPI_空Token_各自保留旧值(t *testing.T) {
	cfg := minimalCfgForTest("", "", "http://old")
	cfg.Integrations.GLPI.AppToken = "preserved-app"
	cfg.Integrations.GLPI.UserToken = "preserved-user"
	r := newIntegrationTestRouter(cfg)
	// 只改 URL + app_token，user_token 留空 → 保留
	body := []byte(`{"url":"http://new","app_token":"new-app","user_token":""}`)
	req := httptest.NewRequest(http.MethodPut, "/integrations/glpi", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "preserved-user", cfg.Integrations.GLPI.UserToken, "空 user_token 必须保留旧值")
}

// ==================== M59 + M60: 集成 URL 的 scheme 校验 ====================

// TestUpdateIntegrations_非URL_返400 M59 引入、M60 换成自定义校验后仍成立：
// scheme-less 的值（`not-a-url`）不进 handler 逻辑，直接 400。
func TestUpdateIntegrations_非URL_返400(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{"zabbix", "/integrations/zabbix", `{"url":"not-a-url","user":"Admin"}`},
		{"netbox", "/integrations/netbox", `{"url":"not-a-url","token":"t"}`},
		{"glpi", "/integrations/glpi", `{"url":"not-a-url","app_token":"a","user_token":"b"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalCfgForTest("", "", "")
			r := newIntegrationTestRouter(cfg)
			req := httptest.NewRequest(http.MethodPut, tc.path, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

// TestUpdateIntegrations_内网URL_不被url标签拒 M59 引入、M60 起由 isHTTPURL 承接同一语义：
// scheme 是 http 但 **host 无 TLD**（`http://netbox:8000`）的自定义校验必须放行 —— 集成目标
// 常是内网主机名。若哪天换成「必须有 TLD」的校验（直接 `new URL()` 强校验 / RFC 3986 附录 B
// 正则），合法配置会被挡在门外（M59 brief 点名的风险），这条钉住它。
func TestUpdateIntegrations_内网URL_不被url标签拒(t *testing.T) {
	cfg := minimalCfgForTest("", "", "")
	r := newIntegrationTestRouter(cfg)
	// svc=nil：binding 通过后 handler 写 cfg 再 ReloadNetBox → panic → gin.Recovery 转 500。
	// 断言「不是 400」即证明 `url` tag 放行了内网无 TLD 地址。
	body := []byte(`{"url":"http://netbox:8000"}`)
	req := httptest.NewRequest(http.MethodPut, "/integrations/netbox", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "http://netbox:8000", cfg.Integrations.Netbox.URL)
}

// TestUpdateIntegrations_非HTTPScheme_返400 M60/T-56：三家集成的 URL 必须是 http(s)。
//
// M59 用的 `binding:"url"` 只要求「有 scheme + host」—— `ftp://example.com` 照样通过，
// 于是 API 直连 / 脚本调用能给集成配置写进非 http(s) 的地址（前端 URL_PATTERN 拦不住绕过的调用）。
// 这条按 3 端点 × 3 非 http(s) scheme 展开，钉住「handler 里那道 isHTTPURL」：
// 断言 400 **且** cfg 未被写 —— 证明拒绝发生在写内存配置之前，不是「先写坏再报错」。
func TestUpdateIntegrations_非HTTPScheme_返400(t *testing.T) {
	for _, ep := range []struct {
		name  string
		path  string
		body  func(url string) string
		urlOf func(*config.Config) string
	}{
		{
			name:  "zabbix",
			path:  "/integrations/zabbix",
			body:  func(u string) string { return `{"url":"` + u + `","user":"Admin"}` },
			urlOf: func(c *config.Config) string { return c.Integrations.Zabbix.URL },
		},
		{
			name:  "netbox",
			path:  "/integrations/netbox",
			body:  func(u string) string { return `{"url":"` + u + `","token":"t"}` },
			urlOf: func(c *config.Config) string { return c.Integrations.Netbox.URL },
		},
		{
			name:  "glpi",
			path:  "/integrations/glpi",
			body:  func(u string) string { return `{"url":"` + u + `","app_token":"a","user_token":"b"}` },
			urlOf: func(c *config.Config) string { return c.Integrations.GLPI.URL },
		},
	} {
		// file:/// 是「有 scheme + 有 host」的合法 URI，正是 `url` tag 放行而白名单必须挡的形态。
		for _, scheme := range []string{"ftp://example.com", "file:///etc/passwd", "ssh://root@10.0.0.1"} {
			t.Run(ep.name+"/"+scheme, func(t *testing.T) {
				cfg := minimalCfgForTest("", "", "")
				r := newIntegrationTestRouter(cfg)
				req := httptest.NewRequest(http.MethodPut, ep.path, bytes.NewReader([]byte(ep.body(scheme))))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				assert.Equal(t, http.StatusBadRequest, w.Code)
				assert.Empty(t, ep.urlOf(cfg), "非 http(s) 的值不得写进内存 cfg")
			})
		}
	}
}

// ==================== v2.2: TestNetBox/TestGLPI 连通 ====================

func TestTestNetBox_svcNil_返500(t *testing.T) {
	cfg := minimalCfgForTest("", "", "")
	r := newIntegrationTestRouter(cfg)
	req := httptest.NewRequest(http.MethodPost, "/integrations/netbox/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestTestGLPI_svcNil_返500(t *testing.T) {
	cfg := minimalCfgForTest("", "", "")
	r := newIntegrationTestRouter(cfg)
	req := httptest.NewRequest(http.MethodPost, "/integrations/glpi/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ==================== G-28：连通测试失败回显不泄漏凭据 ====================

// newIntegrationRouterWithRealSvc 三个连通测试都需要非 nil svc（nil 会 panic）。
func newIntegrationRouterWithRealSvc(cfg *config.Config) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewIntegrationHandler(integration.NewIntegrationService(cfg, nil), cfg)
	r.POST("/integrations/zabbix/test", h.TestZabbix)
	r.POST("/integrations/netbox/test", h.TestNetBox)
	r.POST("/integrations/glpi/test", h.TestGLPI)
	return r
}

// integrationTestMessage 发一次连通测试并解出 message 字段。
// 注意：HTTP body 里的 < > 会被 JSON 编码成 < / >，不能直接断言原文。
func integrationTestMessage(t *testing.T, r *gin.Engine, path string) (int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "响应必须是合法 JSON")
	return w.Code, body.Message
}

// 端口 1 无人监听 → 立刻 refuse（不联网、不等超时）。query 里的 token 是 stdlib
// stripPassword 不管的（它只掩 userinfo），必须靠 redact 兜住。
func TestTestZabbix_失败回显不泄漏URL凭据(t *testing.T) {
	cfg := minimalCfgForTest("", "http://127.0.0.1:1/api_jsonrpc.php?auth=SUPERSECRET", "")
	code, msg := integrationTestMessage(t, newIntegrationRouterWithRealSvc(cfg), "/integrations/zabbix/test")

	require.Equal(t, http.StatusBadRequest, code)
	require.Contains(t, msg, "Zabbix 连通失败")
	require.Contains(t, msg, "http://127.0.0.1:1", "URL 必须塌缩成 scheme://host")
	assert.NotContains(t, msg, "SUPERSECRET")
	assert.NotContains(t, msg, "auth=")
}

// URL 解析失败这条路径没有 http.Client 的 stripPassword，url.Error 原样带 userinfo。
func TestTestZabbix_非法URL不泄漏userinfo(t *testing.T) {
	cfg := minimalCfgForTest("", "http://admin:SUPERSECRET@[::1/api_jsonrpc.php", "")
	code, msg := integrationTestMessage(t, newIntegrationRouterWithRealSvc(cfg), "/integrations/zabbix/test")

	require.Equal(t, http.StatusBadRequest, code)
	require.Contains(t, msg, "missing ']' in host", "保留 parse 原因，便于排障")
	assert.NotContains(t, msg, "SUPERSECRET")
	assert.NotContains(t, msg, "admin:")
}

// 审计 HIGH-2：NetBox/GLPI 的 400 回显脱敏零覆盖（只有 Zabbix 被上面两个用例钉住），
// 去掉 redact.Text 的变异不被捕获。两处与 Zabbix 同型，各镜像一条。
func TestTestNetBox_失败回显不泄漏URL凭据(t *testing.T) {
	cfg := minimalCfgForTest("http://127.0.0.1:1/api/dcim/devices/?token=SUPERSECRET", "", "")
	code, msg := integrationTestMessage(t, newIntegrationRouterWithRealSvc(cfg), "/integrations/netbox/test")

	require.Equal(t, http.StatusBadRequest, code)
	require.Contains(t, msg, "NetBox 连通失败")
	require.Contains(t, msg, "http://127.0.0.1:1", "URL 必须塌缩成 scheme://host")
	assert.NotContains(t, msg, "SUPERSECRET")
	assert.NotContains(t, msg, "token=")
}

func TestTestGLPI_失败回显不泄漏URL凭据(t *testing.T) {
	cfg := minimalCfgForTest("", "", "http://127.0.0.1:1/apirest.php?user_token=SUPERSECRET")
	code, msg := integrationTestMessage(t, newIntegrationRouterWithRealSvc(cfg), "/integrations/glpi/test")

	require.Equal(t, http.StatusBadRequest, code)
	require.Contains(t, msg, "GLPI 连通失败")
	require.Contains(t, msg, "http://127.0.0.1:1", "URL 必须塌缩成 scheme://host")
	assert.NotContains(t, msg, "SUPERSECRET")
	assert.NotContains(t, msg, "user_token=")
}
