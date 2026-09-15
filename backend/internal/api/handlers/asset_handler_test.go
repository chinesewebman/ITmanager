package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"network-monitor-platform/internal/api/handlers"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// mockAssetService 手写 mock（避免引入 sqlmock / testify/mock）
type mockAssetService struct {
	listFunc       func(ctx context.Context, f service.AssetFilter) ([]models.Asset, int64, error)
	listAllFunc    func(ctx context.Context) ([]models.Asset, error)
	getFunc        func(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error)
	createFunc     func(ctx context.Context, a *models.Asset, ip *string) error
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
func (m *mockAssetService) Create(ctx context.Context, a *models.Asset, ip *string) error {
	return m.createFunc(ctx, a, ip)
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
		createFunc: func(ctx context.Context, a *models.Asset, ip *string) error { return nil },
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
		createFunc: func(ctx context.Context, a *models.Asset, ip *string) error {
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

// ==================== M63: 资产 IP 投影 (G-UI-AssetIpPersistence) ====================

// strPtr 取地址。Go 1.26 的 `new(值)` 要 1.26+，本模块 go.mod 仍是 1.25.0。
func strPtr(s string) *string { return &s }

// M63: 列表项上的 ip_address 必须出现在 HTTP 响应里 —— 字段名是前端契约
// （AssetTable 列 + Ping/Traceroute 的 disabled 判据都读 `record.ip_address`）。
// 只断言「items 里多了一个字段」不够：字段名拼错（如 ipAddress）在图上看不出来。
func TestM63_ListAssets_投影ip_address(t *testing.T) {
	withIP, withV6, noIP := uuid.New(), uuid.New(), uuid.New()
	svc := &mockAssetService{
		listFunc: func(ctx context.Context, f service.AssetFilter) ([]models.Asset, int64, error) {
			return []models.Asset{
				{ID: withIP, Name: "web-01", IpAddress: strPtr("10.0.0.1")},
				{ID: withV6, Name: "web-02", IpAddress: strPtr("fe80::1")},
				{ID: noIP, Name: "web-03"}, // 无网卡 → nil
			}, 3, nil
		},
	}
	r := newTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/assets", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Data.Items, 3)

	assert.Equal(t, "10.0.0.1", resp.Data.Items[0]["ip_address"], "v4 优先")
	assert.Equal(t, "fe80::1", resp.Data.Items[1]["ip_address"], "无 v4 时用 v6")
	assert.Nil(t, resp.Data.Items[2]["ip_address"], "无网卡 → null（前端据此禁用 Ping/Traceroute）")
}

// M63: 详情响应的 asset.ip_address 同样注入（详情页头部也显示 IP）。
func TestM63_GetAsset_投影ip_address(t *testing.T) {
	id := uuid.New()
	svc := &mockAssetService{
		getFunc: func(ctx context.Context, got string) (*models.Asset, []models.AssetNetwork, error) {
			return &models.Asset{ID: id, Name: "web-01", IpAddress: strPtr("10.0.0.1")},
				[]models.AssetNetwork{{ID: uuid.New(), AssetID: id, IPv4Address: "10.0.0.1"}}, nil
		},
	}
	r := newTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/assets/"+id.String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp struct {
		Data struct {
			Asset struct {
				IPAddress *string `json:"ip_address"`
			} `json:"asset"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Data.Asset.IPAddress)
	assert.Equal(t, "10.0.0.1", *resp.Data.Asset.IPAddress)
}

// M63 (T-76): handler 入口必须把 `ip_address` 从 updates 里**摘掉**再交给 service。
// 断言的观测点是 service 收到的 map（唯一能区分「剥了」与「没剥但库恰好没报错」的地方）。
func TestM63_UpdateAsset_剥掉ip_address键(t *testing.T) {
	var got map[string]interface{}
	svc := &mockAssetService{
		updateFunc: func(ctx context.Context, id string, u map[string]interface{}) (*models.Asset, error) {
			got = u
			return &models.Asset{ID: uuid.New()}, nil
		},
	}
	r := newTestRouter(svc)

	body, _ := json.Marshal(map[string]any{"name": "web-02", "ip_address": "10.0.0.1"})
	req := httptest.NewRequest(http.MethodPut, "/assets/"+uuid.New().String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.NotNil(t, got)
	assert.Equal(t, "web-02", got["name"], "其它字段必须原样透传")
	_, present := got["ip_address"]
	assert.False(t, present, "ip_address 必须在进 service 前被剥掉（否则 PUT 撞 42703 → 500）")
}

// M63 (T-76) 路由级证据：真 service + 真 GORM 语句生成 + sqlmock 驱动，
// 走完 `PUT /assets/:id` 的完整链路，断言**驱动实际收到的 SQL 里没有 ip_address**。
//
// 为什么不能只靠上面那条 mock 用例：那条钉的是「handler 剥了键」，这条钉的是
// 「剥了之后整条链路真的不产生这一列」。两者缺一：只留前者，service 换实现（比如
// 自己拼 map）后漂移不会被发现；只留后者，剥键被删掉时驱动的实际 SQL 会带上
// `"ip_address"` → 本用例红（实测：去掉 delete 后此处 FAIL，见 M63-graph-analysis.md）。
func TestM63_UpdateAsset_带ip_address不产生该列的SQL_返200(t *testing.T) {
	gormDB, mock, captured := newSQLCapturingDB(t)

	id := uuid.New()
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id.String(), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "status", "asset_type", "created_at", "updated_at"}).
			AddRow(id.String(), "web-01", "active", "server", time.Now(), time.Now()))
	mock.ExpectBegin()
	mock.ExpectExec(`.*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	r := newTestRouter(service.NewAssetService(gormDB))
	body, _ := json.Marshal(map[string]any{"name": "web-02", "ip_address": "1.2.3.4"})
	req := httptest.NewRequest(http.MethodPut, "/assets/"+id.String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s（42703 会走到这里变 500）", w.Body.String())
	assert.Contains(t, captured.joined(), `UPDATE "assets" SET`, "必须真的发出 UPDATE")
	assert.NotContains(t, captured.joined(), "ip_address",
		"UPDATE 不得引用 assets 上不存在的列（PG 42703）")
}

// M64: POST /assets 带 ip_address —— 它**不进 assets 的 INSERT**（`assets` 表没有这一列，
// `gorm:"-"` 让虚拟字段在 schema 解析阶段就被排除），但**会**产生一条 asset_networks 的 INSERT。
//
// M63 时这条用例断言的是「不落库」—— 那时确实是：值绑进虚拟字段后被 GORM 静默丢掉，
// 表单里的 IP 写完没影（正是 M64 要修的东西）。现在两件事分开看：
//   - 虚拟字段仍然不是写入路径（第一段断言，防 `assets` 列清单漂移 / T-76 那类 42703）；
//   - 真写入走的是独立入参 → asset_networks 行（第二段断言，M64 的实质）。
//   - 第二段的 IP 用**规范形式**比对：实现走 net.ParseIP().String()，1.2.3.4 恰好不变。
func TestM64_CreateAsset_带ip_address_assets无此列但网卡有行(t *testing.T) {
	gormDB, mock, captured := newSQLCapturingDB(t)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "assets"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectQuery(`INSERT INTO "asset_networks"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	r := newTestRouter(service.NewAssetService(gormDB))
	body, _ := json.Marshal(map[string]any{"name": "web-01", "asset_type": "server", "ip_address": "1.2.3.4"})
	req := httptest.NewRequest(http.MethodPost, "/assets", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())
	joined := captured.joined()
	assert.Contains(t, joined, `INSERT INTO "assets"`)
	assert.Contains(t, joined, `INSERT INTO "asset_networks"`, "表单里的 IP 必须真的写进网卡表")
	// 「写下去的到底是表单里那个地址」不在这一层断言：PreferSimpleProtocol 把值走成占位符
	// （$5）而非字面量，SQL 文本里看不到它 —— 值保真由真 sqlite 的端到端用例断言
	// （TestM64_CreateAsset_带ip_address_落成网卡行 读回 ipv4_address）。
	// `assets` 的列清单里不得出现 ip_address —— 真 PG 上是 42703。
	// 逐条语句看：网卡表的 INSERT 本来就有 ipv4_address 这样的列名。
	for _, stmt := range captured.stmts {
		if strings.Contains(stmt, `INSERT INTO "assets"`) {
			assert.NotContains(t, stmt, "ip_address", "虚拟字段不得出现在 assets 的列清单里：%s", stmt)
		}
	}
}

// sqlCapture 记录驱动**实际收到**的语句。
//
// 为什么需要它：sqlmock 的期望只能断言「某条 SQL 被发过」，无法断言「某列**没**被写」——
// 而 M63 要证明的恰恰是后者（`assets` 没有 `ip_address` 列）。这里把期望放宽成 `.*`，
// 把 actual SQL 抄下来供用例直接读。
type sqlCapture struct{ stmts []string }

func (c *sqlCapture) Match(_, actualSQL string) error {
	c.stmts = append(c.stmts, actualSQL)
	return nil
}

// joined 把捕获到的语句拼成一串，供 Contains / NotContains 直接断言。
func (c *sqlCapture) joined() string { return strings.Join(c.stmts, "\n") }

// newSQLCapturingDB 给 handler 测试一把「真 service + 真 gorm + sqlmock 驱动」的 DB。
// 与 service 层 newMockDB 同一形态（postgres dialect，保证 SQL 语法和列名解析行为一致），
// 差别只在 QueryMatcher 被换成记录器。
func newSQLCapturingDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sqlCapture) {
	t.Helper()
	cap := &sqlCapture{}
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(cap))
	require.NoError(t, err)

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB, mock, cap
}

// ==================== M64: CreateAsset 写 AssetNetwork (G-Asset-NetworksPersist) ====================

// newAssetSQLiteHandlerDB 真 sqlite（手写 DDL，列口径对齐 models.Asset / models.AssetNetwork）。
//
// 这里用真库而不是 mock：本组要证的是**端到端**接线 —— 表单 JSON 里的 `ip_address`
// 一路变成 `asset_networks` 里的一行。mock 只能证明「handler 把某个值递给了 service」，
// 递的是不是 JSON 里那个值、service 有没有把它写下去，只有真库能一起看到。
// （不用 AutoMigrate：models.Asset.ID 带 `default:gen_random_uuid()`，sqlite 上没这个函数。）
func newAssetSQLiteHandlerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	stmts := []string{
		`CREATE TABLE assets (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			asset_tag TEXT,
			sn TEXT,
			asset_type TEXT,
			brand TEXT,
			model TEXT,
			site_id TEXT,
			site_name TEXT,
			rack_id TEXT,
			rack_name TEXT,
			rack_position TEXT,
			purchase_date DATETIME,
			warranty_end DATETIME,
			vendor TEXT,
			vendor_contact TEXT,
			status TEXT DEFAULT 'active',
			online_time DATETIME,
			offline_time DATETIME,
			last_known_ip4 TEXT,
			last_known_ip6 TEXT,
			retired_at DATETIME,
			retired_reason TEXT,
			retired_by TEXT,
			business_unit TEXT,
			service_name TEXT,
			tags TEXT,
			custom_fields TEXT,
			net_box_id INTEGER,
			source TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE asset_networks (
			id TEXT PRIMARY KEY,
			asset_id TEXT NOT NULL,
			interface_name TEXT NOT NULL,
			interface_type TEXT,
			mac_address TEXT,
			ipv4_address TEXT,
			ipv4_netmask TEXT,
			ipv6_address TEXT,
			speed INTEGER,
			duplex TEXT,
			status TEXT,
			connected_to TEXT,
			connected_port TEXT,
			purpose TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
	}
	for _, s := range stmts {
		require.NoError(t, db.Exec(s).Error)
	}
	return db
}

// postAsset 发一条 POST /assets，返回响应。
func postAsset(t *testing.T, r *gin.Engine, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/assets", bytes.NewBuffer(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// M64 主证据（端到端）：POST /assets 带 ip_address → 201，且 `asset_networks` 里有**恰好一行**，
// 值落在 ipv4_address、并挂在刚建的这张资产下。
//
// 「M63 之前这里是 0 行」是本轮存在的理由：表单一直在送这个字段，后端一直静默丢掉。
func TestM64_CreateAsset_带ip_address_落成网卡行(t *testing.T) {
	db := newAssetSQLiteHandlerDB(t)
	r := newTestRouter(service.NewAssetService(db))

	w := postAsset(t, r, map[string]any{"name": "web-01", "asset_type": "server", "ip_address": "10.0.0.5"})
	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())

	var nets []models.AssetNetwork
	require.NoError(t, db.Find(&nets).Error)
	require.Len(t, nets, 1, "表单里的 IP 必须真的落成一张网卡行")
	assert.Equal(t, "10.0.0.5", nets[0].IPv4Address)
	assert.Equal(t, "eth0", nets[0].InterfaceName)

	// 网卡是**这张**资产的，不是孤儿行：比对响应体里回传的 asset.id。
	var resp struct {
		Data models.Asset `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, resp.Data.ID, nets[0].AssetID, "网卡必须挂在响应里那张资产下")
}

// 不带 ip_address：201 且网卡表为空 —— 建资产不该凭空长出一个网口。
// 与上一条成对的负控：没有它，上一条的「恰好一行」在实现改成「总是建一行」时仍会绿。
func TestM64_CreateAsset_无ip_address_不建网卡(t *testing.T) {
	db := newAssetSQLiteHandlerDB(t)
	r := newTestRouter(service.NewAssetService(db))

	w := postAsset(t, r, map[string]any{"name": "web-01", "asset_type": "server"})
	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())

	var n int64
	require.NoError(t, db.Model(&models.AssetNetwork{}).Count(&n).Error)
	assert.Zero(t, n, "没给 IP 就不该建网卡")
}

// 非法 IP → 422 + `validation_failed`，且**不进 service**。
//
// 为什么是 422 而不是 400：JSON 合法、字段名也对，只有取值不对 —— 前端要按字段高亮而不是
// 当成请求坏了。为什么必须挡在入口：service 也会拒（兜底），但那时请求已经过了参数校验这道
// 语义闸，错误文案只能笼统说「名称不能为空」（ErrInvalidInput 的既有映射），指错字段。
func TestM64_CreateAsset_非法ip_address_返回422(t *testing.T) {
	called := false
	svc := &mockAssetService{
		createFunc: func(ctx context.Context, a *models.Asset, ip *string) error {
			called = true
			return nil
		},
	}
	r := newTestRouter(svc)

	w := postAsset(t, r, map[string]any{"name": "web-01", "asset_type": "server", "ip_address": "not-an-ip"})

	require.Equal(t, http.StatusUnprocessableEntity, w.Code, "body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "validation_failed")
	assert.False(t, called, "非法 IP 挡在入口，不回调 service")
}

// 空串 / 纯空白不算非法（也**不是** 201 时凭空建的网卡）：API 直连方与「清空 IP」都走这条路。
// 前端表单的 required 规则会先挡住空值，但这不该让后端把空串判成格式错误。
func TestM64_CreateAsset_空ip_address不算非法(t *testing.T) {
	for _, ip := range []string{"", "   "} {
		t.Run(ip, func(t *testing.T) {
			var got *string
			svc := &mockAssetService{
				createFunc: func(ctx context.Context, a *models.Asset, ip *string) error {
					got = ip
					return nil
				},
			}
			r := newTestRouter(svc)

			w := postAsset(t, r, map[string]any{"name": "web-01", "asset_type": "server", "ip_address": ip})
			require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())
			require.NotNil(t, got, "字段存在时应原样透传给 service（由它决定空值 = 不建网卡）")
			assert.Equal(t, ip, *got)
		})
	}
}

// ip_address 的值必须落进 **Asset** 之外的通道：`models.Asset.IpAddress` 是 `gorm:"-"` 的投影字段，
// 若 handler 继续只绑 models.Asset（M63 之前的写法），这个键会被 JSON 展平塞进虚拟字段再被 GORM
// 丢掉 —— 接口返 201，库里什么都没有。本用例直接钉那条通道：service 收到的 Asset 上虚拟字段为 nil，
// 值走的是独立入参。
func TestM64_CreateAsset_ip_address走独立入参不变虚拟字段(t *testing.T) {
	var gotAsset *models.Asset
	var gotIP *string
	svc := &mockAssetService{
		createFunc: func(ctx context.Context, a *models.Asset, ip *string) error {
			gotAsset, gotIP = a, ip
			return nil
		},
	}
	r := newTestRouter(svc)

	w := postAsset(t, r, map[string]any{"name": "web-01", "asset_type": "server", "ip_address": "10.0.0.5"})
	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())

	require.NotNil(t, gotIP)
	assert.Equal(t, "10.0.0.5", *gotIP)
	require.NotNil(t, gotAsset)
	assert.Nil(t, gotAsset.IpAddress, "虚拟字段是只读投影，值只能走显式入参（否则就是静默丢弃）")
}
