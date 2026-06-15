package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"network-monitor-platform/internal/api/handlers"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// mockAssetService 手写 mock（避免引入 sqlmock / testify/mock）
type mockAssetService struct {
	listFunc   func(ctx context.Context, f service.AssetFilter) ([]models.Asset, int64, error)
	getFunc    func(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error)
	createFunc func(ctx context.Context, a *models.Asset) error
	updateFunc func(ctx context.Context, id string, u map[string]interface{}) (*models.Asset, error)
	deleteFunc func(ctx context.Context, id string) error
}

func (m *mockAssetService) List(ctx context.Context, f service.AssetFilter) ([]models.Asset, int64, error) {
	return m.listFunc(ctx, f)
}
func (m *mockAssetService) Get(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error) {
	return m.getFunc(ctx, id)
}
func (m *mockAssetService) Create(ctx context.Context, a *models.Asset) error {
	return m.createFunc(ctx, a)
}
func (m *mockAssetService) Update(ctx context.Context, id string, u map[string]interface{}) (*models.Asset, error) {
	return m.updateFunc(ctx, id, u)
}
func (m *mockAssetService) Delete(ctx context.Context, id string) error {
	return m.deleteFunc(ctx, id)
}

// newTestRouter 把 handler 挂到 /assets 路由上
func newTestRouter(svc service.AssetService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewAssetHandler(svc)
	g := r.Group("/assets")
	g.GET("", h.ListAssets)
	g.GET("/:id", h.GetAsset)
	g.POST("", h.CreateAsset)
	g.PUT("/:id", h.UpdateAsset)
	g.DELETE("/:id", h.DeleteAsset)
	g.GET("/export", h.ExportAssets)
	return r
}

func TestListAssets_正常返回_带分页参数(t *testing.T) {
	uid := uuid.New()
	svc := &mockAssetService{
		listFunc: func(ctx context.Context, f service.AssetFilter) ([]models.Asset, int64, error) {
			// 验证 handler 把 query 解析正确
			assert.Equal(t, "router", f.Keyword)
			assert.Equal(t, "active", f.Status)
			assert.Equal(t, "switch", f.AssetType)
			assert.Equal(t, 2, f.Page)
			assert.Equal(t, 50, f.PageSize)
			return []models.Asset{{ID: uid, Name: "switch-01"}}, 1, nil
		},
	}
	r := newTestRouter(svc)

	req := httptest.NewRequest("GET", "/assets?keyword=router&status=active&type=switch&page=2&page_size=50", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Items []models.Asset `json:"items"`
			Total int64          `json:"total"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	assert.Equal(t, int64(1), resp.Data.Total)
	assert.Equal(t, "switch-01", resp.Data.Items[0].Name)
}

func TestGetAsset_不存在_返回404_统一错误结构(t *testing.T) {
	svc := &mockAssetService{
		getFunc: func(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error) {
			return nil, nil, service.ErrNotFound
		},
	}
	r := newTestRouter(svc)

	req := httptest.NewRequest("GET", "/assets/00000000-0000-0000-0000-000000000000", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	var resp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "not_found", resp.Code)
	assert.NotEmpty(t, resp.Message)
	// 关键：不再泄露 err.Error() 内部细节
	assert.NotContains(t, w.Body.String(), "gorm")
	assert.NotContains(t, w.Body.String(), "sql")
}

func TestGetAsset_DB错误_返回500_不泄露内部错误(t *testing.T) {
	svc := &mockAssetService{
		getFunc: func(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error) {
			// 模拟 DB 异常（含敏感字符串）
			return nil, nil, errors.New("pq: connection terminated (SQLSTATE broken pipe)")
		},
	}
	r := newTestRouter(svc)

	req := httptest.NewRequest("GET", "/assets/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	body := w.Body.String()
	// 关键安全检查：原始错误信息不能出现在响应里
	assert.NotContains(t, body, "pq:")
	assert.NotContains(t, body, "SQLSTATE")
	assert.NotContains(t, body, "broken pipe")
	// 对外只暴露通用文案
	assert.Contains(t, body, "code")
}

func TestCreateAsset_参数错误_返回400(t *testing.T) {
	svc := &mockAssetService{
		createFunc: func(ctx context.Context, a *models.Asset) error { return nil },
	}
	r := newTestRouter(svc)

	// 非法 JSON
	req := httptest.NewRequest("POST", "/assets", bytes.NewBufferString("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateAsset_名称为空_返回400(t *testing.T) {
	svc := &mockAssetService{
		createFunc: func(ctx context.Context, a *models.Asset) error {
			// service 层应拒绝空名
			return service.ErrInvalidInput
		},
	}
	r := newTestRouter(svc)

	asset := models.Asset{Name: ""} // 空名
	body, _ := json.Marshal(asset)
	req := httptest.NewRequest("POST", "/assets", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "bad_request")
}

func TestDeleteAsset_成功_返回200(t *testing.T) {
	deletedID := ""
	svc := &mockAssetService{
		deleteFunc: func(ctx context.Context, id string) error {
			deletedID = id
			return nil
		},
	}
	r := newTestRouter(svc)

	req := httptest.NewRequest("DELETE", "/assets/asset-123", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "asset-123", deletedID)
}

func TestUpdateAsset_不存在_返回404(t *testing.T) {
	svc := &mockAssetService{
		updateFunc: func(ctx context.Context, id string, u map[string]interface{}) (*models.Asset, error) {
			return nil, service.ErrNotFound
		},
	}
	r := newTestRouter(svc)

	updates := map[string]interface{}{"status": "inactive"}
	body, _ := json.Marshal(updates)
	req := httptest.NewRequest("PUT", "/assets/missing", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
