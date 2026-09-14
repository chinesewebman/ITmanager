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
	"github.com/stretchr/testify/require"
)

// mockAssetService 手写 mock（避免引入 sqlmock / testify/mock）
type mockAssetService struct {
	listFunc       func(ctx context.Context, f service.AssetFilter) ([]models.Asset, int64, error)
	listAllFunc    func(ctx context.Context) ([]models.Asset, error)
	getFunc        func(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error)
	createFunc     func(ctx context.Context, a *models.Asset) error
	updateFunc     func(ctx context.Context, id string, u map[string]interface{}) (*models.Asset, error)
	deleteFunc     func(ctx context.Context, id string) error
	retireFunc     func(ctx context.Context, id string, reason string, userID uuid.UUID) (*models.Asset, []models.AssetNetwork, error)
	bulkRetireFunc func(ctx context.Context, ids []string, reason string, userID uuid.UUID) ([]string, map[string]string, error)
	restoreFunc    func(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error)
}

func (m *mockAssetService) List(ctx context.Context, f service.AssetFilter) ([]models.Asset, int64, error) {
	return m.listFunc(ctx, f)
}
func (m *mockAssetService) ListAll(ctx context.Context) ([]models.Asset, error) {
	return m.listAllFunc(ctx)
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
func (m *mockAssetService) Retire(ctx context.Context, id string, reason string, userID uuid.UUID) (*models.Asset, []models.AssetNetwork, error) {
	return m.retireFunc(ctx, id, reason, userID)
}
func (m *mockAssetService) BulkRetire(ctx context.Context, ids []string, reason string, userID uuid.UUID) ([]string, map[string]string, error) {
	return m.bulkRetireFunc(ctx, ids, reason, userID)
}
func (m *mockAssetService) Restore(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error) {
	return m.restoreFunc(ctx, id)
}

// testUserID newTestRouter 注入的请求身份（AuthMiddleware 的替身）。BulkRetireAssets
// 从 c.GetString("user_id") 取操作者（写进 retired_by），必须有值。
var testUserID = uuid.MustParse("11111111-1111-1111-1111-111111111111")

// newTestRouter 把 handler 挂到 /assets 路由上
func newTestRouter(svc service.AssetService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery()) // T-15: handler svc=nil panic 不穿透 testing.tRunner
	// AuthMiddleware 替身：X-Test-User-ID 可覆盖（测非法 uuid → 401 分支），默认合法用户。
	r.Use(func(c *gin.Context) {
		uid := c.GetHeader("X-Test-User-ID")
		if uid == "" {
			uid = testUserID.String()
		}
		c.Set("user_id", uid)
	})
	h := handlers.NewAssetHandler(svc)
	g := r.Group("/assets")
	g.GET("", h.ListAssets)
	g.GET("/:id", h.GetAsset)
	g.POST("", h.CreateAsset)
	g.PUT("/:id", h.UpdateAsset)
	g.DELETE("/:id", h.DeleteAsset)
	g.GET("/export", h.ExportAssets)
	// M58: 与 routes.go 同序 —— 静态段 /bulk-retire 先于 /:id/retire 注册。
	g.POST("/bulk-retire", h.BulkRetireAssets)
	g.POST("/:id/retire", h.RetireAsset)
	g.POST("/:id/restore", h.RestoreAsset)
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

// ==================== M32 导出保真：错误路径 ====================

// 取数失败时不能产出「带 CSV 头的错误响应」—— 那会让调用方把错误体当附件下载。
func TestM32_ExportAssets_取数失败不带CSV头(t *testing.T) {
	svc := &mockAssetService{
		listAllFunc: func(ctx context.Context) ([]models.Asset, error) {
			return nil, errors.New("db is down")
		},
	}
	// 前提钉子：nil 的 listAllFunc 会 panic，而 gin.Recovery() 的 500 同样「无 CSV 头」，
	// 本用例会因错误的原因通过。先钉住前提，panic 路径另有下面的形状断言兜底。
	require.NotNil(t, svc.listAllFunc, "U6 前提：必须注入 listAllFunc")
	r := newTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/assets/export", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Header().Get("Content-Type"), "text/csv", "500 不应带 CSV 类型")
	assert.Empty(t, w.Header().Get("Content-Disposition"), "500 不应带下载头")
	assert.Empty(t, w.Header().Get("X-Total-Count"), "取数失败时不应声明条数")
	// service 错误路径才打 internal_error；panic 路径（Recovery）body 为空 —— 两者形状不同
	assert.Contains(t, w.Body.String(), "internal_error", "必须是 service 错误路径，而不是 panic")
}

// ==================== M58: BulkRetireAssets ====================

// M58 批量退役：全部成功 → 200 + succeeded 含全部 id
func TestM58_BulkRetireAssets_全部成功(t *testing.T) {
	id1, id2 := uuid.New().String(), uuid.New().String()
	var gotIDs []string
	svc := &mockAssetService{
		bulkRetireFunc: func(ctx context.Context, ids []string, reason string, userID uuid.UUID) ([]string, map[string]string, error) {
			gotIDs = ids
			// reason 直达 service（写进 retired_reason），userID 来自认证上下文
			assert.Equal(t, "批量退役", reason)
			assert.Equal(t, testUserID, userID)
			return ids, map[string]string{}, nil
		},
	}
	r := newTestRouter(svc)

	body, _ := json.Marshal(map[string]any{"ids": []string{id1, id2}, "reason": "批量退役"})
	req := httptest.NewRequest(http.MethodPost, "/assets/bulk-retire", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	assert.Equal(t, []string{id1, id2}, gotIDs)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Succeeded []string          `json:"succeeded"`
			Failed    map[string]string `json:"failed"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	assert.Equal(t, []string{id1, id2}, resp.Data.Succeeded)
	assert.Empty(t, resp.Data.Failed)
}

// M58 批量退役：部分失败仍是 200 —— succeeded / failed 两个字段各自承载结果，
// 不能用 4xx 表达「有一条不存在」（那会让前端丢掉成功的那部分）。
func TestM58_BulkRetireAssets_部分失败_仍返200(t *testing.T) {
	okID, badID := uuid.New().String(), uuid.New().String()
	svc := &mockAssetService{
		bulkRetireFunc: func(ctx context.Context, ids []string, reason string, userID uuid.UUID) ([]string, map[string]string, error) {
			return []string{okID}, map[string]string{badID: "资产不存在"}, nil
		},
	}
	r := newTestRouter(svc)

	body, _ := json.Marshal(map[string]any{"ids": []string{okID, badID}, "reason": ""})
	req := httptest.NewRequest(http.MethodPost, "/assets/bulk-retire", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp struct {
		Data struct {
			Succeeded []string          `json:"succeeded"`
			Failed    map[string]string `json:"failed"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, []string{okID}, resp.Data.Succeeded)
	assert.Equal(t, map[string]string{badID: "资产不存在"}, resp.Data.Failed)
}

// M58 批量退役：ids 缺失 / 空 → 400（不进 service）
func TestM58_BulkRetireAssets_空ids_返400(t *testing.T) {
	called := false
	svc := &mockAssetService{
		bulkRetireFunc: func(ctx context.Context, ids []string, reason string, userID uuid.UUID) ([]string, map[string]string, error) {
			called = true
			return nil, nil, nil
		},
	}
	r := newTestRouter(svc)

	for _, body := range []string{`{}`, `{"ids":[]}`, `{"reason":"x"}`} {
		t.Run(body, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/assets/bulk-retire", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "ids 不能为空")
			assert.False(t, called, "空 ids 不得进 service")
		})
	}
}

// M58 批量退役：ids 超 1000 → 400（防 DoS，对齐 /alerts/bulk-*）
func TestM58_BulkRetireAssets_超过1000条_返400(t *testing.T) {
	ids := make([]string, 1001)
	for i := range ids {
		ids[i] = uuid.New().String()
	}
	svc := &mockAssetService{
		bulkRetireFunc: func(ctx context.Context, list []string, reason string, userID uuid.UUID) ([]string, map[string]string, error) {
			t.Error("超限请求不得进 service")
			return nil, nil, nil
		},
	}
	r := newTestRouter(svc)

	body, _ := json.Marshal(map[string]any{"ids": ids})
	req := httptest.NewRequest(http.MethodPost, "/assets/bulk-retire", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "最多 1000 条")
}

// M58 批量退役：user_id 非 uuid → 401（retired_by 必须是合法操作者）
func TestM58_BulkRetireAssets_非法userID_返401(t *testing.T) {
	svc := &mockAssetService{
		bulkRetireFunc: func(ctx context.Context, ids []string, reason string, userID uuid.UUID) ([]string, map[string]string, error) {
			t.Error("非法凭证不得进 service")
			return nil, nil, nil
		},
	}
	r := newTestRouter(svc)

	body, _ := json.Marshal(map[string]any{"ids": []string{uuid.New().String()}})
	req := httptest.NewRequest(http.MethodPost, "/assets/bulk-retire", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User-ID", "not-a-uuid")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// M58 路由顺序：POST /assets/bulk-retire 不被 /assets/:id/retire 当成 id="bulk-retire"。
//
// 判别必须落在**调用面**上：两条路径对空 body 都返 400，只看状态码分不出来。
// 这里给合法 body，若被 /:id/retire 收走 → retireFunc 被调（用例失败）；被 bulk handler
// 收走 → bulkRetireFunc 收到 ids。
func TestM58_批量退役路由_不被id_retire吞(t *testing.T) {
	id := uuid.New().String()
	retireCalled := false
	bulkCalled := false
	svc := &mockAssetService{
		retireFunc: func(ctx context.Context, got string, reason string, userID uuid.UUID) (*models.Asset, []models.AssetNetwork, error) {
			retireCalled = true
			return &models.Asset{ID: testUserID}, nil, nil
		},
		bulkRetireFunc: func(ctx context.Context, ids []string, reason string, userID uuid.UUID) ([]string, map[string]string, error) {
			bulkCalled = true
			return ids, map[string]string{}, nil
		},
	}
	r := newTestRouter(svc)

	body, _ := json.Marshal(map[string]any{"ids": []string{id}, "reason": ""})
	req := httptest.NewRequest(http.MethodPost, "/assets/bulk-retire", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	assert.True(t, bulkCalled, "bulk-retire 必须落到 BulkRetireAssets")
	assert.False(t, retireCalled, "bulk-retire 不得被 /:id/retire 吞掉（id=bulk-retire）")
}
