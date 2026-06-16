package handlers_test

import (
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

// ==================== 已知 Issue 文档化 ====================
//
// 审查发现 issue #7：handler.Sync 在 type 不识别时静默 fallback 到 "all"
// 修复建议：
//   default: apierr.BadRequest(c, "不支持的 type: "+req.Type)
//
// 当前生产代码（integration_handler.go:36-39）行为：
//   if err := c.ShouldBindJSON(&req); err != nil {
//       req.Type = "all" // 这里！如果 JSON parse 失败也走 all
//   }
// 应区分：JSON parse fail → 400, type unrecognize → 400
// 留作后续修复，TODO 跟踪。
