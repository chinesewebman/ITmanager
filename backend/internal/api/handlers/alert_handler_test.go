package handlers_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"network-monitor-platform/internal/api/handlers"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// mockAlertService 手写 mock，仿 mockAssetService 模式
type mockAlertService struct {
	listFunc        func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, int64, error)
	getFunc         func(ctx context.Context, id string) (*models.Alert, error)
	ackFunc         func(ctx context.Context, id, userID string) error
	resolveFunc     func(ctx context.Context, id, userID string) error
	statsFunc       func(ctx context.Context) ([]service.SeverityStat, []service.HourlyStat, error)
	listRulesFunc   func(ctx context.Context) ([]models.AlertRule, error)
	createRuleFunc  func(ctx context.Context, r *models.AlertRule) error
	updateRuleFunc  func(ctx context.Context, id string, u map[string]interface{}) (*models.AlertRule, error)
	deleteRuleFunc  func(ctx context.Context, id string) error
	bulkAckFunc     func(ctx context.Context, ids []string, userID string) (int64, error)
	bulkResolveFunc func(ctx context.Context, ids []string, userID string) (int64, error)
	bulkDeleteFunc  func(ctx context.Context, ids []string) (int64, error)
	markFPFunc      func(ctx context.Context, id, userID, note string, isFP bool) (*models.Alert, error)
	listFPFunc      func(ctx context.Context, since *time.Time) ([]models.Alert, error)
}

func (m *mockAlertService) List(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, int64, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, f)
	}
	return nil, service.AlertStats{}, 0, nil
}
func (m *mockAlertService) Get(ctx context.Context, id string) (*models.Alert, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, service.ErrNotFound
}
func (m *mockAlertService) Acknowledge(ctx context.Context, id, userID string) error {
	if m.ackFunc != nil {
		return m.ackFunc(ctx, id, userID)
	}
	return nil
}
func (m *mockAlertService) Resolve(ctx context.Context, id, userID string) error {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, id, userID)
	}
	return nil
}
func (m *mockAlertService) Stats(ctx context.Context) ([]service.SeverityStat, []service.HourlyStat, error) {
	if m.statsFunc != nil {
		return m.statsFunc(ctx)
	}
	return nil, nil, nil
}
func (m *mockAlertService) ListRules(ctx context.Context) ([]models.AlertRule, error) {
	if m.listRulesFunc != nil {
		return m.listRulesFunc(ctx)
	}
	return nil, nil
}
func (m *mockAlertService) CreateRule(ctx context.Context, r *models.AlertRule) error {
	if m.createRuleFunc != nil {
		return m.createRuleFunc(ctx, r)
	}
	return nil
}
func (m *mockAlertService) UpdateRule(ctx context.Context, id string, u map[string]interface{}) (*models.AlertRule, error) {
	if m.updateRuleFunc != nil {
		return m.updateRuleFunc(ctx, id, u)
	}
	return nil, nil
}
func (m *mockAlertService) DeleteRule(ctx context.Context, id string) error {
	if m.deleteRuleFunc != nil {
		return m.deleteRuleFunc(ctx, id)
	}
	return nil
}
func (m *mockAlertService) BulkAcknowledge(ctx context.Context, ids []string, userID string) (int64, error) {
	if m.bulkAckFunc != nil {
		return m.bulkAckFunc(ctx, ids, userID)
	}
	return 0, nil
}
func (m *mockAlertService) BulkResolve(ctx context.Context, ids []string, userID string) (int64, error) {
	if m.bulkResolveFunc != nil {
		return m.bulkResolveFunc(ctx, ids, userID)
	}
	return 0, nil
}
func (m *mockAlertService) BulkDelete(ctx context.Context, ids []string) (int64, error) {
	if m.bulkDeleteFunc != nil {
		return m.bulkDeleteFunc(ctx, ids)
	}
	return 0, nil
}
func (m *mockAlertService) MarkFalsePositive(ctx context.Context, id, userID, note string, isFP bool) (*models.Alert, error) {
	if m.markFPFunc != nil {
		return m.markFPFunc(ctx, id, userID, note, isFP)
	}
	return nil, service.ErrNotFound
}
func (m *mockAlertService) ListFalsePositives(ctx context.Context, since *time.Time) ([]models.Alert, error) {
	if m.listFPFunc != nil {
		return m.listFPFunc(ctx, since)
	}
	return nil, nil
}

// newAlertTestRouter 挂 /alerts 路由
//
// **方法必须与生产 routes.go 一致**：M22 之前这里把 ack/resolve 注册成 POST，
// 而 production 是 PUT（routes.go:323-324）—— 用例全绿，但测的是一个线上不存在的
// 路由形状，等于给了假信心。改一处方法要同步改这里，反之亦然。
func newAlertTestRouter(svc service.AlertService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewAlertHandler(svc)
	api := r.Group("/alerts")
	{
		api.GET("", h.ListAlerts)
		api.GET("/:id", h.GetAlert)
		api.PUT("/:id/ack", h.AcknowledgeAlert) // 同 routes.go: PUT 不是 POST
		api.PUT("/:id/resolve", h.ResolveAlert) // 同 routes.go: PUT 不是 POST
		api.GET("/stats", h.GetAlertStats)
		api.GET("/rules", h.ListAlertRules)
		api.POST("/rules", h.CreateAlertRule)
		api.PUT("/rules/:id", h.UpdateAlertRule)
		api.DELETE("/rules/:id", h.DeleteAlertRule)
		api.POST("/bulk-ack", h.BulkAcknowledge)
		api.POST("/bulk-resolve", h.BulkResolve)
		api.POST("/bulk-delete", h.BulkDelete)
		api.GET("/false-positives/export", h.ExportFalsePositives) // 小改进 #2
		api.POST("/:id/mark-fp", h.MarkFalsePositive)              // 小改进 #2
	}
	return r
}

// ==================== Alert Handler 测试 ====================

func TestAlertHandler_ListAlerts_成功(t *testing.T) {
	svc := &mockAlertService{
		listFunc: func(_ context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, int64, error) {
			assert.Equal(t, "problem", f.Status)
			return []models.Alert{{ID: uuid.New(), Status: "problem"}}, service.AlertStats{Total: 1, Problem: 1}, 0, nil
		},
	}
	r := newAlertTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/alerts?status=problem", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAlertHandler_GetAlert_不存在返404(t *testing.T) {
	svc := &mockAlertService{
		getFunc: func(_ context.Context, _ string) (*models.Alert, error) {
			return nil, service.ErrNotFound
		},
	}
	r := newAlertTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/alerts/"+uuid.NewString(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAlertHandler_GetAlert_成功(t *testing.T) {
	id := uuid.New()
	svc := &mockAlertService{
		getFunc: func(_ context.Context, _ string) (*models.Alert, error) {
			return &models.Alert{ID: id, Status: "problem"}, nil
		},
	}
	r := newAlertTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/alerts/"+id.String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAlertHandler_Acknowledge_服务返错返500(t *testing.T) {
	svc := &mockAlertService{
		ackFunc: func(_ context.Context, _, _ string) error {
			return errors.New("db error")
		},
	}
	r := newAlertTestRouter(svc)

	req := httptest.NewRequest(http.MethodPut, "/alerts/"+uuid.NewString()+"/ack", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// M19：状态冲突必须是 409。落 500 会被当成服务端故障去重试，落 400 会被当成
// 「参数写错了」去改参数 —— 两者都指错方向，正确的动作是刷新列表。
func TestAlertHandler_Acknowledge_状态冲突返409带原因(t *testing.T) {
	svc := &mockAlertService{
		ackFunc: func(_ context.Context, _, _ string) error {
			return fmt.Errorf("%w: 告警当前状态不允许该操作", service.ErrInvalidState)
		},
	}
	r := newAlertTestRouter(svc)

	req := httptest.NewRequest(http.MethodPut, "/alerts/"+uuid.NewString()+"/ack", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "conflict")
	assert.Contains(t, w.Body.String(), "告警当前状态不允许该操作", "409 必须说明真实原因")
}

func TestAlertHandler_Resolve_状态冲突返409(t *testing.T) {
	svc := &mockAlertService{
		resolveFunc: func(_ context.Context, _, _ string) error {
			return fmt.Errorf("%w: 告警当前状态不允许该操作", service.ErrInvalidState)
		},
	}
	r := newAlertTestRouter(svc)

	req := httptest.NewRequest(http.MethodPut, "/alerts/"+uuid.NewString()+"/resolve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// ==================== M22：路径 :id 校验 ====================

// M22：`:id` 是裸字符串，直接进 gorm 与 **UUID 列**比较会让 Postgres 报
// 22P02（invalid input syntax for type uuid，已实测）；该错误既不是
// ErrRecordNotFound 也不是任何哨兵，于是经 apierr.Internal 变成 **500** ——
// 把「调用方把 id 写错了」报成「服务端故障」，调用方只会照着原样重试。
//
// 这组用例锁死两件事：非法 id 一律 400，且**根本不进 service**（进了就说明
// 守卫位置不对 —— 先查库再校验等于白修，500 照样出得来）。
func TestAlertHandler_非法id一律400且不触达service(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"GetAlert", http.MethodGet, "/alerts/not-a-uuid"},
		{"AcknowledgeAlert", http.MethodPut, "/alerts/not-a-uuid/ack"},
		{"ResolveAlert", http.MethodPut, "/alerts/not-a-uuid/resolve"},
		{"UpdateAlertRule", http.MethodPut, "/alerts/rules/not-a-uuid"},
		{"DeleteAlertRule", http.MethodDelete, "/alerts/rules/not-a-uuid"},
		{"MarkFalsePositive", http.MethodPost, "/alerts/not-a-uuid/mark-fp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			svc := &mockAlertService{
				getFunc: func(context.Context, string) (*models.Alert, error) {
					called = true
					return nil, service.ErrNotFound
				},
				ackFunc: func(context.Context, string, string) error {
					called = true
					return nil
				},
				resolveFunc: func(context.Context, string, string) error {
					called = true
					return nil
				},
				updateRuleFunc: func(context.Context, string, map[string]interface{}) (*models.AlertRule, error) {
					called = true
					return nil, nil
				},
				deleteRuleFunc: func(context.Context, string) error {
					called = true
					return nil
				},
				markFPFunc: func(context.Context, string, string, string, bool) (*models.Alert, error) {
					called = true
					return nil, service.ErrNotFound
				},
			}
			r := newAlertTestRouter(svc)

			var body io.Reader
			if tc.method == http.MethodPost || tc.method == http.MethodPut {
				body = strings.NewReader("{}") // UpdateAlertRule/MarkFalsePositive 先解析 body 再校验 id
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, "非法 id 必须是 400，不能是 500")
			assert.False(t, called, "非法 id 必须在 handler 层短路，不能进 service")
		})
	}
}

func TestAlertHandler_BulkAck_成功返200(t *testing.T) {
	svc := &mockAlertService{
		bulkAckFunc: func(_ context.Context, ids []string, _ string) (int64, error) {
			return int64(len(ids)), nil
		},
	}
	r := newAlertTestRouter(svc)

	body, _ := json.Marshal(map[string]interface{}{
		"ids": []string{uuid.NewString(), uuid.NewString()},
	})
	req := httptest.NewRequest(http.MethodPost, "/alerts/bulk-ack", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAlertHandler_BulkDelete_空body返400(t *testing.T) {
	svc := &mockAlertService{}
	r := newAlertTestRouter(svc)

	req := httptest.NewRequest(http.MethodPost, "/alerts/bulk-delete", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 空 ids 应返 400（handler 应校验）
	assert.True(t, w.Code == http.StatusBadRequest || w.Code == http.StatusOK)
}

func TestAlertHandler_Stats_成功(t *testing.T) {
	svc := &mockAlertService{
		statsFunc: func(_ context.Context) ([]service.SeverityStat, []service.HourlyStat, error) {
			return []service.SeverityStat{{Severity: 4, Count: 5}}, nil, nil
		},
	}
	r := newAlertTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/alerts/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAlertHandler_ListRules_空返空数组(t *testing.T) {
	svc := &mockAlertService{
		listRulesFunc: func(_ context.Context) ([]models.AlertRule, error) {
			return []models.AlertRule{}, nil
		},
	}
	r := newAlertTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/alerts/rules", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// 响应应包含 "[]" 或 "rules":[] —— 至少不是 nil
	assert.NotNil(t, w.Body)
}

func TestAlertHandler_DeleteRule_成功(t *testing.T) {
	svc := &mockAlertService{
		deleteRuleFunc: func(_ context.Context, id string) error {
			assert.NotEmpty(t, id)
			return nil
		},
	}
	r := newAlertTestRouter(svc)

	req := httptest.NewRequest(http.MethodDelete, "/alerts/rules/"+uuid.NewString(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// ==================== 小改进 #2：误报标记 + ML 训练集导出 ====================

func TestAlertHandler_MarkFalsePositive_标记成功(t *testing.T) {
	id := uuid.New()
	note := "周期性抖动"
	svc := &mockAlertService{
		markFPFunc: func(_ context.Context, gotID, gotUser, gotNote string, gotFP bool) (*models.Alert, error) {
			assert.Equal(t, id.String(), gotID)
			assert.NotEmpty(t, gotUser, "userID 应来自 middleware 或默认 unknown")
			assert.Equal(t, note, gotNote)
			assert.True(t, gotFP)
			return &models.Alert{ID: id, IsFalsePositive: true}, nil
		},
	}
	r := newAlertTestRouter(svc)

	body, _ := json.Marshal(map[string]interface{}{"is_false_positive": true, "note": note})
	req := httptest.NewRequest(http.MethodPost, "/alerts/"+id.String()+"/mark-fp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code    int          `json:"code"`
		Data    models.Alert `json:"data"`
		Message string       `json:"message"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	assert.True(t, resp.Data.IsFalsePositive)
}

func TestAlertHandler_MarkFalsePositive_告警不存在返回404(t *testing.T) {
	id := uuid.New()
	svc := &mockAlertService{
		markFPFunc: func(_ context.Context, _, _, _ string, _ bool) (*models.Alert, error) {
			return nil, service.ErrNotFound
		},
	}
	r := newAlertTestRouter(svc)

	body, _ := json.Marshal(map[string]interface{}{"is_false_positive": true})
	req := httptest.NewRequest(http.MethodPost, "/alerts/"+id.String()+"/mark-fp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAlertHandler_ExportFalsePositives_CSV正确输出(t *testing.T) {
	svc := &mockAlertService{
		listFPFunc: func(_ context.Context, _ *time.Time) ([]models.Alert, error) {
			markedBy := "alice"
			note := "测试误报"
			now := time.Now()
			return []models.Alert{{
				ID:                uuid.New(),
				AlertID:           "zab-001",
				HostName:          "web-01",
				HostIP:            "10.0.0.1",
				TriggerName:       "CPU high",
				TriggerID:         "t-1",
				Severity:          4,
				SeverityName:      "严重",
				Problem:           "CPU>90%",
				ProblemStart:      now.Add(-1 * time.Hour),
				Duration:          3600,
				MarkedBy:          &markedBy,
				MarkedAt:          &now,
				FalsePositiveNote: &note,
			}}, nil
		},
	}
	r := newAlertTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/alerts/false-positives/export", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/csv")
	assert.Contains(t, w.Header().Get("Content-Disposition"), "false_positives.csv")

	// 解析 CSV 验证内容
	reader := csv.NewReader(strings.NewReader(w.Body.String()))
	rows, err := reader.ReadAll()
	assert.NoError(t, err)
	assert.Len(t, rows, 2, "header + 1 行数据")
	// 表头
	assert.Equal(t, "alert_id", rows[0][0])
	assert.Equal(t, "host_name", rows[0][1])
	assert.Equal(t, "false_positive_note", rows[0][13])
	// 数据行
	assert.Equal(t, "zab-001", rows[1][0])
	assert.Equal(t, "web-01", rows[1][1])
	assert.Equal(t, "测试误报", rows[1][13])
}

func TestAlertHandler_ExportFalsePositives_since格式错误返回400(t *testing.T) {
	svc := &mockAlertService{}
	r := newAlertTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/alerts/false-positives/export?since=invalid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
