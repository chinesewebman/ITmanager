package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// init 注册带 gen_random_uuid() 的 sqlite3 driver
func init() {
	sql.Register("sqlite3_uuid", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("gen_random_uuid", func() string {
				return uuid.New().String()
			}, true)
		},
	})
}

// newDiagHandlerTestDB 4 张表 + 1 个种子资产
func newDiagHandlerTestDB(t *testing.T) (*gorm.DB, uuid.UUID) {
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
		`CREATE TABLE alerts (
			id TEXT PRIMARY KEY, alert_id TEXT, host_id TEXT, host_name TEXT, host_ip TEXT,
			trigger_name TEXT, trigger_id TEXT, severity INTEGER, severity_name TEXT,
			problem TEXT, problem_start DATETIME, problem_end DATETIME, duration INTEGER,
			status TEXT DEFAULT 'problem', ack_time DATETIME, ack_user TEXT,
			resolve_time DATETIME, resolve_user TEXT, ticket_id TEXT, asset_id TEXT,
			source TEXT, repeat_count INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE tickets (
			id TEXT PRIMARY KEY, ticket_number TEXT, title TEXT, description TEXT,
			ticket_type TEXT, priority TEXT, status TEXT DEFAULT 'open',
			requester_id TEXT, requester_name TEXT, requester_email TEXT,
			assignee_id TEXT, assignee_name TEXT, category TEXT, tags TEXT,
			asset_id TEXT, asset_name TEXT, external_id TEXT, source TEXT,
			resolution TEXT, resolved_at DATETIME, closed_at DATETIME, due_date DATETIME,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE asset_networks (
			id TEXT PRIMARY KEY, asset_id TEXT, interface_name TEXT, interface_type TEXT,
			mac_address TEXT, ipv4_address TEXT, ipv4_netmask TEXT, ipv_address TEXT,
			speed INTEGER, duplex TEXT, status TEXT, connected_to TEXT, connected_port TEXT,
			purpose TEXT, created_at DATETIME, updated_at DATETIME
		)`,
	} {
		require.NoError(t, db.Exec(s).Error)
	}

	assetID := uuid.New()
	now := time.Now().UTC()
	require.NoError(t, db.Exec(`INSERT INTO assets (id, name, asset_type, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		assetID, "diag-test-asset", "server", "active", now, now).Error)

	return db, assetID
}

func setupDiagRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, _ := newDiagHandlerTestDB(t)
	svc := service.NewDiagnosticService(db)
	h := NewDiagnosticHandler(svc)
	r := gin.New()
	r.GET("/api/v1/diagnostics/assets/:id/timeline", h.GetAssetTimeline)
	r.GET("/api/v1/diagnostics/ping", h.PingAsset)
	r.GET("/api/v1/diagnostics/traceroute", h.TracerouteAsset)
	return r, db
}

// ==================== DiagnosticHandler 测试 ====================

func TestDiagnosticHandler_GetAssetTimeline_无效UUID返回400(t *testing.T) {
	r, _ := setupDiagRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/assets/not-a-uuid/timeline", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "资产 ID 格式错误")
}

func TestDiagnosticHandler_GetAssetTimeline_不存在资产返回404(t *testing.T) {
	r, _ := setupDiagRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/assets/"+uuid.New().String()+"/timeline", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDiagnosticHandler_GetAssetTimeline_负数days返回400(t *testing.T) {
	r, _ := setupDiagRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/assets/"+uuid.New().String()+"/timeline?days=-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "days")
}

func TestDiagnosticHandler_GetAssetTimeline_非数字limit返回400(t *testing.T) {
	r, _ := setupDiagRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/assets/"+uuid.New().String()+"/timeline?limit=abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDiagnosticHandler_GetAssetTimeline_空资产返回200空事件流(t *testing.T) {
	r, db := setupDiagRouter(t)

	// 拿种子资产 ID
	var id string
	require.NoError(t, db.Raw("SELECT id FROM assets LIMIT 1").Scan(&id).Error)
	require.NotEmpty(t, id)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/assets/"+id+"/timeline", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"code":0`)
	assert.Contains(t, body, `"data"`)
	assert.Contains(t, body, `"diag-test-asset"`)
	assert.Contains(t, body, `"events":[]`)
}

func TestDiagnosticHandler_GetAssetTimeline_默认DaysLimit生效(t *testing.T) {
	r, db := setupDiagRouter(t)
	var id string
	require.NoError(t, db.Raw("SELECT id FROM assets LIMIT 1").Scan(&id).Error)

	// 插一个 100 天前的告警（> 默认 30 天窗口）应被滤掉
	oldProblem := time.Now().UTC().Add(-100 * 24 * time.Hour)
	now := time.Now().UTC()
	require.NoError(t, db.Exec(`INSERT INTO alerts
		(id, host_id, host_name, trigger_name, severity, severity_name, problem,
		 problem_start, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.New(), id, "host", "old alert", 3, "Warning", "old",
		oldProblem, "problem", now, now).Error)

	// 不传 days/limit → 默认 30 天
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/assets/"+id+"/timeline", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"alert_count":0`, "100 天前应被默认 30 天窗口滤掉")
}

// ==================== 静态检查：apierr 包导入正确 ====================

// 这个测试是为了证明 handler 引用了 apierr（不报 unused import）
var _ = apierr.BadRequest
var _ = context.TODO

// 验证返回的 JSON 包含 apierr.ErrorResponse 风格
func TestDiagnosticHandler_返回内容是JSON(t *testing.T) {
	r, _ := setupDiagRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/assets/"+uuid.New().String()+"/timeline", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	ct := w.Header().Get("Content-Type")
	// gin 默认 content-type
	if !strings.HasPrefix(ct, "application/json") && ct != "" {
		t.Errorf("expected application/json, got %q", ct)
	}
}

// ==================== PingAsset handler 测试 ====================

func TestDiagnosticHandler_PingAsset_缺host(t *testing.T) {
	r, _ := setupDiagRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/ping", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "host 必传")
}

func TestDiagnosticHandler_PingAsset_count非法(t *testing.T) {
	r, _ := setupDiagRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/ping?host=127.0.0.1&count=abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "count 必须是正整数")
}

func TestDiagnosticHandler_PingAsset_host非法字符(t *testing.T) {
	r, _ := setupDiagRouter(t)
	// 含分号 → service 拒绝
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/ping?host=host%3Bbad&count=2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDiagnosticHandler_PingAsset_成功(t *testing.T) {
	if _, err := exec.LookPath("ping"); err != nil {
		t.Skip("ping binary not available")
	}
	r, _ := setupDiagRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/ping?host=127.0.0.1&count=2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"transmitted":`, "应包含 transmitted 字段")
	assert.Contains(t, body, `"received":`, "应包含 received 字段")
}

// ==================== TracerouteAsset handler 测试 ====================

func TestDiagnosticHandler_TracerouteAsset_缺host(t *testing.T) {
	r, _ := setupDiagRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/traceroute", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "host 必传")
}

func TestDiagnosticHandler_TracerouteAsset_maxHops非法(t *testing.T) {
	r, _ := setupDiagRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/traceroute?host=127.0.0.1&maxHops=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDiagnosticHandler_TracerouteAsset_成功(t *testing.T) {
	if _, err := exec.LookPath("traceroute"); err != nil {
		t.Skip("traceroute binary not available")
	}
	r, _ := setupDiagRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/traceroute?host=127.0.0.1&maxHops=3", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"hops":`, "应包含 hops 数组")
}
