package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"network-monitor-platform/internal/api/handlers"
	"network-monitor-platform/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 注：integration handler 用具体类型 *integration.IntegrationService
// （非 interface），完整 mock 需重构成 interface 或 sqlmock 模拟 DB
// 本文件只测不依赖 svc 的 GetIntegrationStatus 路由

func newIntegrationTestRouter(cfg *config.Config) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery()) // 把 svc=nil 触发的 panic 转 500（不让它穿透 testing.tRunner）
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

func TestIntegrationStatus_ThreeIntegrationsReturnURL(t *testing.T) {
	cfg := minimalCfgForTest("http://netbox.local", "http://zabbix.local", "http://glpi.local")
	r := newIntegrationTestRouter(cfg)

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

// ==================== BUG FIX 回归测试 ====================

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

	for _, ty := range []string{"netbox", "zabbix", "glpi", "all", ""} {
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
	r := newIntegrationTestRouter(cfg)

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
	r := newIntegrationTestRouter(cfg)

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
	r := newIntegrationTestRouter(cfg)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"has_token":true`)
	assert.NotContains(t, body, `"token":"real-token-xyz"`)
}

func TestIntegrationStatus_NetBox_NoToken(t *testing.T) {
	cfg := minimalCfgForTest("http://netbox.local", "", "")
	r := newIntegrationTestRouter(cfg)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integrations/status", nil))
	assert.Contains(t, w.Body.String(), `"has_token":false`)
}

// TestIntegrationStatus_GLPI_HasTokens v2.2: GLPI 双 token 各自独立反映。
func TestIntegrationStatus_GLPI_HasTokens(t *testing.T) {
	cfg := minimalCfgForTest("", "", "http://glpi.local")
	cfg.Integrations.GLPI.AppToken = "app-tok"
	cfg.Integrations.GLPI.UserToken = "" // 只有 app
	r := newIntegrationTestRouter(cfg)
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
