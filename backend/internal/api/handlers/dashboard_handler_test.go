package handlers_test

import (
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDashboardServiceOnlyHandlerTest 命名避免与 ticket_channel_dashboard_user_handler_test 重名
type mockDashboardServiceOnlyHandlerTest struct {
	statsFunc  func(ctx context.Context) (*service.DashboardStats, error)
	trendsFunc func(ctx context.Context, days int) ([]service.TrendPoint, error)
	kpisFunc   func(ctx context.Context, days int) (*service.KPI, error)
}

func (m *mockDashboardServiceOnlyHandlerTest) Stats(ctx context.Context) (*service.DashboardStats, error) {
	return m.statsFunc(ctx)
}
func (m *mockDashboardServiceOnlyHandlerTest) AlertTrends(ctx context.Context, days int) ([]service.TrendPoint, error) {
	return m.trendsFunc(ctx, days)
}
func (m *mockDashboardServiceOnlyHandlerTest) KPIs(ctx context.Context, days int) (*service.KPI, error) {
	if m.kpisFunc == nil {
		return &service.KPI{WindowDays: days}, nil
	}
	return m.kpisFunc(ctx, days)
}

func newDashboardTestRouterOnlyHandlerTest(svc service.DashboardService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewDashboardHandler(svc)
	g := r.Group("/dashboard")
	g.GET("/stats", h.GetDashboardStats)
	g.GET("/trends", h.GetDashboardTrends)
	g.GET("/kpis", h.GetKPIs)
	return r
}

// ==================== GetDashboardStats ====================

func TestDashboardStats_ReturnsStatsOK(t *testing.T) {
	svc := &mockDashboardServiceOnlyHandlerTest{
		statsFunc: func(ctx context.Context) (*service.DashboardStats, error) {
			return &service.DashboardStats{
				Assets:  100,
				Alerts:  5,
				Tickets: 12,
			}, nil
		},
	}
	r := newDashboardTestRouterOnlyHandlerTest(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard/stats", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"code":0`)
	assert.Contains(t, body, `"assets":100`)
	assert.Contains(t, body, `"alerts":5`)
	assert.Contains(t, body, `"tickets":12`)
}

func TestDashboardStats_ServiceError_Returns500_NoLeak(t *testing.T) {
	svc := &mockDashboardServiceOnlyHandlerTest{
		statsFunc: func(ctx context.Context) (*service.DashboardStats, error) {
			return nil, errors.New("pq: relation 'assets' does not exist")
		},
	}
	r := newDashboardTestRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard/stats", nil))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"code":"internal_error"`)
	// 关键：SQL 错误不暴露给客户端
	assert.NotContains(t, body, "pq:")
	assert.NotContains(t, body, "relation")
}

// ==================== GetDashboardTrends ====================

func TestDashboardTrends_Default7Days(t *testing.T) {
	var capturedDays int
	svc := &mockDashboardServiceOnlyHandlerTest{
		trendsFunc: func(ctx context.Context, days int) ([]service.TrendPoint, error) {
			capturedDays = days
			return []service.TrendPoint{
				{Date: "2026-06-15", Count: 3},
				{Date: "2026-06-16", Count: 5},
			}, nil
		},
	}
	r := newDashboardTestRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard/trends", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 7, capturedDays, "无 days 参数应默认 7")
	body := w.Body.String()
	assert.Contains(t, body, `"alert_trends"`)
}

func TestDashboardTrends_CustomDays(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected int
	}{
		{"1天", "?days=1", 1},
		{"30天", "?days=30", 30},
		{"NonNumeric_Fallback0_ServiceHandles", "?days=abc", 0},
		{"Negative_PassesThrough", "?days=-5", -5}, // service 自己负责校验
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured int
			svc := &mockDashboardServiceOnlyHandlerTest{
				trendsFunc: func(ctx context.Context, days int) ([]service.TrendPoint, error) {
					captured = days
					return nil, nil
				},
			}
			r := newDashboardTestRouter(svc)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard/trends"+tt.query, nil))
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tt.expected, captured)
		})
	}
}

func TestDashboardTrends_Empty_ReturnsEmptyArrayNotNull(t *testing.T) {
	svc := &mockDashboardServiceOnlyHandlerTest{
		trendsFunc: func(ctx context.Context, days int) ([]service.TrendPoint, error) {
			return []service.TrendPoint{}, nil // 空 slice
		},
	}
	r := newDashboardTestRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard/trends", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	// 关键：空 slice 应序列化为 [] 而非 null（前端 .map 不挂）
	assert.Contains(t, body, `"alert_trends":[]`)
	assert.NotContains(t, body, `null`)
}

func TestDashboardTrends_Service报错返500(t *testing.T) {
	svc := &mockDashboardServiceOnlyHandlerTest{
		trendsFunc: func(ctx context.Context, days int) ([]service.TrendPoint, error) {
			return nil, errors.New("context deadline exceeded")
		},
	}
	r := newDashboardTestRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard/trends?days=7", nil))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"code":"internal_error"`)
	assert.NotContains(t, body, "deadline")
}

// ==================== JSON 响应格式 ====================

func TestDashboard_ResponseCodeZeroUnified(t *testing.T) {
	svc := &mockDashboardServiceOnlyHandlerTest{
		statsFunc: func(ctx context.Context) (*service.DashboardStats, error) {
			return &service.DashboardStats{}, nil
		},
		trendsFunc: func(ctx context.Context, days int) ([]service.TrendPoint, error) {
			return []service.TrendPoint{}, nil
		},
	}
	r := newDashboardTestRouter(svc)
	for _, path := range []string{"/dashboard/stats", "/dashboard/trends"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.EqualValues(t, 0, resp["code"], "%s 响应 code 应为 0", path)
		assert.NotNil(t, resp["data"], "%s 响应 data 字段不能 nil", path)
	}
}

// ==================== models fixture ====================

// mock models import 防未用报错（避免删 import 后又加）
var _ = models.Asset{}

// ==================== KPI handler 测试 ====================

func ptrInt64(v int64) *int64     { return &v }
func ptrFloat(v float64) *float64 { return &v }

func TestDashboardHandler_GetKPIs_默认7天(t *testing.T) {
	mock := &mockDashboardServiceOnlyHandlerTest{
		kpisFunc: func(ctx context.Context, days int) (*service.KPI, error) {
			return &service.KPI{
				MTTRSeconds:    ptrInt64(3600), // 1h
				MTTDSeconds:    ptrInt64(300),  // 5min
				AlertDensity:   2.5,
				SLAClosedRate:  ptrFloat(0.95),
				WindowDays:     days,
				ResolvedAlerts: 5,
				AckedAlerts:    8,
				ClosedTickets:  20,
				OnTimeTickets:  19,
			}, nil
		},
	}
	r := newDashboardTestRouterOnlyHandlerTest(mock)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/kpis", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"mttr_seconds":3600`, "应返回 MTTR=3600")
	assert.Contains(t, body, `"mttd_seconds":300`, "应返回 MTTD=300")
	assert.Contains(t, body, `"alert_density":2.5`)
	assert.Contains(t, body, `"sla_closed_rate":0.95`)
}

func TestDashboardHandler_GetKPIs_自定义days(t *testing.T) {
	var gotDays int
	mock := &mockDashboardServiceOnlyHandlerTest{
		kpisFunc: func(ctx context.Context, days int) (*service.KPI, error) {
			gotDays = days
			return &service.KPI{WindowDays: days}, nil
		},
	}
	r := newDashboardTestRouterOnlyHandlerTest(mock)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/kpis?days=30", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 30, gotDays, "应透传 days=30")
}

func TestDashboardHandler_GetKPIs_days非法(t *testing.T) {
	r := newDashboardTestRouterOnlyHandlerTest(&mockDashboardServiceOnlyHandlerTest{})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/kpis?days=abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDashboardHandler_GetKPIs_无数据字段为null(t *testing.T) {
	mock := &mockDashboardServiceOnlyHandlerTest{
		kpisFunc: func(ctx context.Context, days int) (*service.KPI, error) {
			// 所有可空字段为 nil
			return &service.KPI{WindowDays: days}, nil
		},
	}
	r := newDashboardTestRouterOnlyHandlerTest(mock)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/kpis", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"mttr_seconds":null`, "MTTR 应为 null")
	assert.Contains(t, body, `"mttd_seconds":null`, "MTTD 应为 null")
	assert.Contains(t, body, `"sla_closed_rate":null`, "SLA 应为 null")
}
