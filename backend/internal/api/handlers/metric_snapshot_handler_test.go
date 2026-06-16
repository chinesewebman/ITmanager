package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"
)

func newMetricHandlerDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.New().String())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	schema, err := os.ReadFile("../../api/testdata/metric_snapshot/init.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return db
}

func setupMetricRouter(t *testing.T) (*gin.Engine, *service.MetricSnapshotService, *gorm.DB) {
	gin.SetMode(gin.TestMode)
	db := newMetricHandlerDB(t)
	svc := service.NewMetricSnapshotService(db)
	h := NewMetricSnapshotHandler(svc)
	r := gin.New()
	r.POST("/api/metric-snapshots", h.BulkInsert)
	r.GET("/api/metric-snapshots", h.Query)
	r.GET("/api/metric-snapshots/latest", h.Latest)
	return r, svc, db
}

func doMetricJSON(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMetricHandler_BulkInsert_正常返201(t *testing.T) {
	r, _, _ := setupMetricRouter(t)
	body := []models.MetricSnapshot{
		{AssetID: uuid.New(), Key: "cpu.user", Value: 45.0, TS: time.Now()},
		{AssetID: uuid.New(), Key: "mem.used", Value: 70.0, TS: time.Now()},
	}
	w := doMetricJSON(t, r, "POST", "/api/metric-snapshots", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201 got %d: %s", w.Code, w.Body.String())
	}
}

func TestMetricHandler_BulkInsert_空数组返400(t *testing.T) {
	r, _, _ := setupMetricRouter(t)
	w := doMetricJSON(t, r, "POST", "/api/metric-snapshots", []models.MetricSnapshot{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestMetricHandler_BulkInsert_非法JSON返400(t *testing.T) {
	r, _, _ := setupMetricRouter(t)
	req := httptest.NewRequest("POST", "/api/metric-snapshots", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestMetricHandler_Query_空返200(t *testing.T) {
	r, _, _ := setupMetricRouter(t)
	w := doMetricJSON(t, r, "GET", "/api/metric-snapshots", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
}

func TestMetricHandler_Query_非法from返400(t *testing.T) {
	r, _, _ := setupMetricRouter(t)
	w := doMetricJSON(t, r, "GET", "/api/metric-snapshots?from=not-a-date", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestMetricHandler_Query_非法limit返400(t *testing.T) {
	r, _, _ := setupMetricRouter(t)
	w := doMetricJSON(t, r, "GET", "/api/metric-snapshots?limit=abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestMetricHandler_Latest_缺assetId返400(t *testing.T) {
	r, _, _ := setupMetricRouter(t)
	w := doMetricJSON(t, r, "GET", "/api/metric-snapshots/latest?key=cpu", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestMetricHandler_Latest_缺key返400(t *testing.T) {
	r, _, _ := setupMetricRouter(t)
	w := doMetricJSON(t, r, "GET", "/api/metric-snapshots/latest?asset_id="+uuid.New().String(), nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestMetricHandler_Latest_正常返200(t *testing.T) {
	r, _, db := setupMetricRouter(t)
	assetID := uuid.New()
	now := time.Now().UTC()
	db.Table("metric_snapshots").Create([]map[string]any{
		{"id": uuid.New().String(), "asset_id": assetID.String(), "key": "cpu", "value": 50.0, "ts": now, "created_at": now},
	})

	w := doMetricJSON(t, r, "GET", "/api/metric-snapshots/latest?asset_id="+assetID.String()+"&key=cpu", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
}
