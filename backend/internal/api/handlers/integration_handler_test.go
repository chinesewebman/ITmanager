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
	h := handlers.NewIntegrationHandler(nil, cfg) // nil svc，Sync 路由不能实际调用
	g := r.Group("/integrations")
	g.POST("/sync", h.Sync)
	g.GET("/status", h.GetIntegrationStatus)
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
