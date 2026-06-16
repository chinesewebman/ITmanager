package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newSuppressionHandlerTestDB 复用 suppression service test 的表结构
func newSuppressionHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE alert_suppressions (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		severity_max INTEGER DEFAULT 3,
		host_pattern TEXT NOT NULL,
		time_window_seconds INTEGER DEFAULT 300,
		ttl_seconds INTEGER DEFAULT 0,
		enabled INTEGER DEFAULT 1,
		description TEXT,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error)
	return db
}

func setupSuppressionRouter(t *testing.T) (*gin.Engine, *service.AlertSuppressionService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := newSuppressionHandlerTestDB(t)
	svc := service.NewAlertSuppressionService(db)
	h := NewAlertSuppressionHandler(svc)
	r := gin.New()
	sup := r.Group("/alert-suppressions")
	sup.GET("", h.ListAlertSuppressions)
	sup.POST("/preview", h.PreviewSuppression)
	sup.POST("", h.CreateAlertSuppression)
	sup.GET("/:id", h.GetAlertSuppression)
	sup.PUT("/:id", h.UpdateAlertSuppression)
	sup.DELETE("/:id", h.DeleteAlertSuppression)
	return r, svc
}

func doJSON(t *testing.T, r *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ==================== AlertSuppressionHandler 测试 ====================

func TestAlertSuppressionHandler_List_空列表返空数组(t *testing.T) {
	r, _ := setupSuppressionRouter(t)
	w := doJSON(t, r, "GET", "/alert-suppressions", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"data":[]`)
}

func TestAlertSuppressionHandler_Create_成功返201(t *testing.T) {
	r, _ := setupSuppressionRouter(t)
	body := models.AlertSuppression{
		Name: "test", HostPattern: "db-*", SeverityMax: 3, TimeWindowSeconds: 60,
	}
	w := doJSON(t, r, "POST", "/alert-suppressions", body)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"name":"test"`)
}

func TestAlertSuppressionHandler_Create_空body返400(t *testing.T) {
	r, _ := setupSuppressionRouter(t)
	w := doJSON(t, r, "POST", "/alert-suppressions", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAlertSuppressionHandler_Create_severity超界返400(t *testing.T) {
	r, _ := setupSuppressionRouter(t)
	body := map[string]interface{}{
		"name": "bad", "host_pattern": "*", "severity_max": 99, "time_window_seconds": 60,
	}
	w := doJSON(t, r, "POST", "/alert-suppressions", body)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "severity_max")
}

func TestAlertSuppressionHandler_Get_存在返200(t *testing.T) {
	r, svc := setupSuppressionRouter(t)
	id := createSuppRule(t, svc, "r1", "db-*", 3, 60)

	w := doJSON(t, r, "GET", "/alert-suppressions/"+id.String(), nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"name":"r1"`)
}

func TestAlertSuppressionHandler_Get_不存在返404(t *testing.T) {
	r, _ := setupSuppressionRouter(t)
	w := doJSON(t, r, "GET", "/alert-suppressions/"+uuid.New().String(), nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAlertSuppressionHandler_Get_无效UUID返400(t *testing.T) {
	r, _ := setupSuppressionRouter(t)
	w := doJSON(t, r, "GET", "/alert-suppressions/not-a-uuid", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAlertSuppressionHandler_Update_修改severity(t *testing.T) {
	r, svc := setupSuppressionRouter(t)
	id := createSuppRule(t, svc, "r1", "db-*", 3, 60)

	w := doJSON(t, r, "PUT", "/alert-suppressions/"+id.String(),
		map[string]interface{}{"severity_max": 5})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"severity_max":5`)
}

func TestAlertSuppressionHandler_Update_severity超界返400(t *testing.T) {
	r, svc := setupSuppressionRouter(t)
	id := createSuppRule(t, svc, "r1", "db-*", 3, 60)

	w := doJSON(t, r, "PUT", "/alert-suppressions/"+id.String(),
		map[string]interface{}{"severity_max": 99})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAlertSuppressionHandler_Delete_成功返204(t *testing.T) {
	r, svc := setupSuppressionRouter(t)
	id := createSuppRule(t, svc, "r1", "db-*", 3, 60)

	w := doJSON(t, r, "DELETE", "/alert-suppressions/"+id.String(), nil)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// 再次 GET 应 404
	w2 := doJSON(t, r, "GET", "/alert-suppressions/"+id.String(), nil)
	assert.Equal(t, http.StatusNotFound, w2.Code)
}

func TestAlertSuppressionHandler_Delete_不存在返404(t *testing.T) {
	r, _ := setupSuppressionRouter(t)
	w := doJSON(t, r, "DELETE", "/alert-suppressions/"+uuid.New().String(), nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAlertSuppressionHandler_Preview_无规则不抑制(t *testing.T) {
	r, _ := setupSuppressionRouter(t)
	body := map[string]interface{}{
		"severity": 3, "host_id": uuid.New().String(), "host_name": "host-1",
	}
	w := doJSON(t, r, "POST", "/alert-suppressions/preview", body)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"suppressed":false`)
}

func TestAlertSuppressionHandler_Preview_severity超界返400(t *testing.T) {
	r, _ := setupSuppressionRouter(t)
	body := map[string]interface{}{
		"severity": 99, "host_id": uuid.New().String(), "host_name": "host-1",
	}
	w := doJSON(t, r, "POST", "/alert-suppressions/preview", body)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAlertSuppressionHandler_Preview_规则匹配不抑制(t *testing.T) {
	r, svc := setupSuppressionRouter(t)
	createSuppRule(t, svc, "r1", "db-*", 3, 60)

	body := map[string]interface{}{
		"severity": 3, "host_id": uuid.New().String(), "host_name": "db-01",
	}
	w := doJSON(t, r, "POST", "/alert-suppressions/preview", body)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"suppressed":false`, "首次评估应放行")
}

func TestAlertSuppressionHandler_Preview_窗口期内抑制(t *testing.T) {
	r, svc := setupSuppressionRouter(t)
	createSuppRule(t, svc, "r1", "db-*", 3, 60)
	hostID := uuid.New()

	body := map[string]interface{}{
		"severity": 3, "host_id": hostID.String(), "host_name": "db-01",
	}
	// 第一次放行
	_ = doJSON(t, r, "POST", "/alert-suppressions/preview", body)
	// 第二次抑制
	w := doJSON(t, r, "POST", "/alert-suppressions/preview", body)
	assert.Contains(t, w.Body.String(), `"suppressed":true`, "窗口内第二次应被抑制")
}

// createSuppRule helper：建 1 条规则并返回 ID
func createSuppRule(t *testing.T, svc *service.AlertSuppressionService, name, pattern string, severityMax, windowSec int) uuid.UUID {
	t.Helper()
	rule := &models.AlertSuppression{
		Name: name, HostPattern: pattern, SeverityMax: severityMax, TimeWindowSeconds: windowSec,
	}
	require.NoError(t, svc.Create(context.Background(), rule))
	return rule.ID
}
