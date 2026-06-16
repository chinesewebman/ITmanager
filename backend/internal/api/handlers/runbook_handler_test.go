package handlers

import (
	"bytes"
	"context"
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

func newRunbookHandlerDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.New().String())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	schema, err := os.ReadFile("../../api/testdata/runbook/init.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return db
}

func setupRunbookRouter(t *testing.T) (*gin.Engine, *service.RunbookService, *gorm.DB) {
	gin.SetMode(gin.TestMode)
	db := newRunbookHandlerDB(t)
	svc := service.NewRunbookService(db)
	h := NewRunbookHandler(svc)
	r := gin.New()
	r.POST("/api/runbooks", h.Create)
	r.GET("/api/runbooks", h.List)
	r.GET("/api/runbooks/recommend", h.Recommend)
	r.GET("/api/runbooks/:id", h.Get)
	r.PUT("/api/runbooks/:id", h.Update)
	r.DELETE("/api/runbooks/:id", h.Delete)
	return r, svc, db
}

func doRunbookJSON(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
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

func seedRunbookHandler(t *testing.T, db *gorm.DB, rb models.Runbook) models.Runbook {
	t.Helper()
	if rb.ID == uuid.Nil {
		rb.ID = uuid.New()
	}
	now := time.Now()
	if rb.CreatedAt.IsZero() {
		rb.CreatedAt = now
	}
	if rb.UpdatedAt.IsZero() {
		rb.UpdatedAt = now
	}
	err := db.Table("runbooks").Create(map[string]any{
		"id": rb.ID.String(), "title": rb.Title, "asset_type": rb.AssetType,
		"summary": rb.Summary, "content_md": rb.ContentMD, "steps": rb.Steps,
		"tags": rb.Tags, "severity": rb.Severity, "enabled": rb.Enabled,
		"created_at": rb.CreatedAt, "updated_at": rb.UpdatedAt,
	}).Error
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return rb
}

// ==================== Create ====================

func TestRunbookHandler_Create_正常返201(t *testing.T) {
	r, _, _ := setupRunbookRouter(t)
	body := models.Runbook{Title: "DB 慢查询", AssetType: "server", Severity: 4, Enabled: true}
	w := doRunbookJSON(t, r, "POST", "/api/runbooks", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201 got %d: %s", w.Code, w.Body.String())
	}
}

func TestRunbookHandler_Create_空title返400(t *testing.T) {
	r, _, _ := setupRunbookRouter(t)
	w := doRunbookJSON(t, r, "POST", "/api/runbooks", models.Runbook{Title: "", AssetType: "server"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestRunbookHandler_Create_非法JSON返400(t *testing.T) {
	r, _, _ := setupRunbookRouter(t)
	req := httptest.NewRequest("POST", "/api/runbooks", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

// ==================== Get ====================

func TestRunbookHandler_Get_存在返200(t *testing.T) {
	r, _, db := setupRunbookRouter(t)
	rb := seedRunbookHandler(t, db, models.Runbook{Title: "x", AssetType: "server"})

	w := doRunbookJSON(t, r, "GET", "/api/runbooks/"+rb.ID.String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
}

func TestRunbookHandler_Get_无效UUID返400(t *testing.T) {
	r, _, _ := setupRunbookRouter(t)
	w := doRunbookJSON(t, r, "GET", "/api/runbooks/not-a-uuid", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestRunbookHandler_Get_不存在返404(t *testing.T) {
	r, _, _ := setupRunbookRouter(t)
	w := doRunbookJSON(t, r, "GET", "/api/runbooks/"+uuid.New().String(), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d", w.Code)
	}
}

// ==================== Update ====================

func TestRunbookHandler_Update_正常返200(t *testing.T) {
	r, svc, _ := setupRunbookRouter(t)
	rb := &models.Runbook{Title: "old", AssetType: "server", Severity: 3}
	if err := svc.Create(context.Background(), rb); err != nil {
		t.Fatalf("create: %v", err)
	}
	rb.Title = "new"
	rb.Severity = 5
	w := doRunbookJSON(t, r, "PUT", "/api/runbooks/"+rb.ID.String(), rb)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
}

func TestRunbookHandler_Update_空title返400(t *testing.T) {
	r, svc, _ := setupRunbookRouter(t)
	rb := &models.Runbook{Title: "x", AssetType: "server"}
	_ = svc.Create(context.Background(), rb)
	rb.Title = ""
	w := doRunbookJSON(t, r, "PUT", "/api/runbooks/"+rb.ID.String(), rb)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

// ==================== Delete ====================

func TestRunbookHandler_Delete_存在返204(t *testing.T) {
	r, svc, _ := setupRunbookRouter(t)
	rb := &models.Runbook{Title: "x", AssetType: "server"}
	_ = svc.Create(context.Background(), rb)
	w := doRunbookJSON(t, r, "DELETE", "/api/runbooks/"+rb.ID.String(), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204 got %d", w.Code)
	}
}

func TestRunbookHandler_Delete_不存在返404(t *testing.T) {
	r, _, _ := setupRunbookRouter(t)
	w := doRunbookJSON(t, r, "DELETE", "/api/runbooks/"+uuid.New().String(), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d", w.Code)
	}
}

// ==================== List ====================

func TestRunbookHandler_List_空返200(t *testing.T) {
	r, _, _ := setupRunbookRouter(t)
	w := doRunbookJSON(t, r, "GET", "/api/runbooks", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
}

func TestRunbookHandler_List_过滤生效(t *testing.T) {
	r, _, db := setupRunbookRouter(t)
	seedRunbookHandler(t, db, models.Runbook{Title: "DB", AssetType: "server", Severity: 4, Enabled: true})
	seedRunbookHandler(t, db, models.Runbook{Title: "Net", AssetType: "switch", Severity: 5, Enabled: true})

	w := doRunbookJSON(t, r, "GET", "/api/runbooks?asset_type=server", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Items []models.Runbook
			Total int
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.Total != 1 {
		t.Fatalf("filter want 1 got %d", resp.Data.Total)
	}
}

func TestRunbookHandler_List_非法severity返400(t *testing.T) {
	r, _, _ := setupRunbookRouter(t)
	w := doRunbookJSON(t, r, "GET", "/api/runbooks?severity=abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

// ==================== Recommend ====================

func TestRunbookHandler_Recommend_空assetType返200(t *testing.T) {
	r, _, _ := setupRunbookRouter(t)
	w := doRunbookJSON(t, r, "GET", "/api/runbooks/recommend", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
}

func TestRunbookHandler_Recommend_按assetType推荐(t *testing.T) {
	r, _, db := setupRunbookRouter(t)
	seedRunbookHandler(t, db, models.Runbook{Title: "DB", AssetType: "server", Severity: 4, Enabled: true})
	seedRunbookHandler(t, db, models.Runbook{Title: "Net", AssetType: "switch", Severity: 4, Enabled: true})

	w := doRunbookJSON(t, r, "GET", "/api/runbooks/recommend?asset_type=server&severity=4", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	var resp struct {
		Code int              `json:"code"`
		Data []models.Runbook `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data) != 1 || resp.Data[0].AssetType != "server" {
		t.Fatalf("recommend want 1 server, got %d: %+v", len(resp.Data), resp.Data)
	}
}
