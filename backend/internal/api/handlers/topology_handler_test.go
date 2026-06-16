package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTopologyHandlerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, s := range []string{
		`CREATE TABLE assets (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, asset_tag TEXT, sn TEXT,
			asset_type TEXT, brand TEXT, model TEXT,
			site_id TEXT, site_name TEXT, rack_id TEXT, rack_name TEXT, rack_position TEXT,
			purchase_date DATETIME, warranty_end DATETIME, vendor TEXT, vendor_contact TEXT,
			status TEXT DEFAULT 'active', online_time DATETIME, offline_time DATETIME,
			business_unit TEXT, service_name TEXT, tags TEXT, custom_fields TEXT,
			net_box_id INTEGER, source TEXT, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE asset_networks (
			id TEXT PRIMARY KEY, asset_id TEXT, interface_name TEXT, interface_type TEXT,
			mac_address TEXT, ipv4_address TEXT, ipv4_netmask TEXT, ipv_address TEXT,
			speed INTEGER, duplex TEXT, status TEXT, connected_to TEXT, connected_port TEXT,
			purpose TEXT, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE alerts (
			id TEXT PRIMARY KEY, alert_id TEXT, host_id TEXT, host_name TEXT, host_ip TEXT,
			trigger_name TEXT, trigger_id TEXT, severity INTEGER, severity_name TEXT,
			problem TEXT, problem_start DATETIME, problem_end DATETIME, duration INTEGER,
			status TEXT DEFAULT 'problem', ack_time DATETIME, ack_user TEXT,
			resolve_time DATETIME, resolve_user TEXT, ticket_id TEXT, asset_id TEXT,
			source TEXT, repeat_count INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
	} {
		require.NoError(t, db.Exec(s).Error)
	}
	return db
}

func setupTopologyRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := newTopologyHandlerDB(t)
	svc := service.NewTopologyService(db)
	h := NewTopologyHandler(svc)
	r := gin.New()
	r.GET("/api/v1/topology", h.GetTopology)
	return r
}

// ==================== TopologyHandler 测试 ====================

func TestTopologyHandler_GetTopology_空DB返200空图(t *testing.T) {
	r := setupTopologyRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/topology", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code int                  `json:"code"`
		Data models.TopologyGraph `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Data.Stats.TotalNodes)
}

func TestTopologyHandler_GetTopology_默认days30生效(t *testing.T) {
	r := setupTopologyRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/topology", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Data models.TopologyGraph `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 30, resp.Data.Stats.WindowDays)
}

func TestTopologyHandler_GetTopology_days超界被clamp(t *testing.T) {
	r := setupTopologyRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/topology?days=99999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Data models.TopologyGraph `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 365, resp.Data.Stats.WindowDays)
}

func TestTopologyHandler_GetTopology_负数days返400(t *testing.T) {
	r := setupTopologyRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/topology?days=-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTopologyHandler_GetTopology_only_with_alerts解析(t *testing.T) {
	r := setupTopologyRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/topology?only_with_alerts=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTopologyHandler_GetTopology_assetTypes逗号分隔(t *testing.T) {
	r := setupTopologyRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/topology?asset_types=server,switch", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
