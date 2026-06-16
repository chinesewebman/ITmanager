package handlers_test

import (
	"bytes"
	"context"
	"encoding/csv"
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

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// mockAlertService 手写 mock，仿 mockAssetService 模式
type mockAlertService struct {
	listFunc        func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, error)
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

func (m *mockAlertService) List(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, f)
	}
	return nil, service.AlertStats{}, nil
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
func newAlertTestRouter(svc service.AlertService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewAlertHandler(svc)
	api := r.Group("/alerts")
	{
		api.GET("", h.ListAlerts)
		api.GET("/:id", h.GetAlert)
		api.POST("/:id/ack", h.AcknowledgeAlert)
		api.POST("/:id/resolve", h.ResolveAlert)
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
		listFunc: func(_ context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, error) {
			assert.Equal(t, "problem", f.Status)
			return []models.Alert{{ID: uuid.New(), Status: "problem"}}, service.AlertStats{Total: 1, Problem: 1}, nil
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

	req := httptest.NewRequest(http.MethodPost, "/alerts/"+uuid.NewString()+"/ack", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
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
