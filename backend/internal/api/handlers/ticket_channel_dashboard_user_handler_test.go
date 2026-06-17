package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
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

// ==================== Channel Handler 测试 ====================

type mockChannelService struct {
	listFunc   func(ctx context.Context) ([]models.NotificationChannel, error)
	getFunc    func(ctx context.Context, id string) (*models.NotificationChannel, error)
	createFunc func(ctx context.Context, ch *models.NotificationChannel) error
	updateFunc func(ctx context.Context, id string, u map[string]interface{}) (*models.NotificationChannel, error)
	deleteFunc func(ctx context.Context, id string) error
	testFunc   func(ctx context.Context, id string) error
}

func (m *mockChannelService) List(ctx context.Context) ([]models.NotificationChannel, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return nil, nil
}
func (m *mockChannelService) Get(ctx context.Context, id string) (*models.NotificationChannel, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, service.ErrNotFound
}
func (m *mockChannelService) Create(ctx context.Context, ch *models.NotificationChannel) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, ch)
	}
	return nil
}
func (m *mockChannelService) Update(ctx context.Context, id string, u map[string]interface{}) (*models.NotificationChannel, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, id, u)
	}
	return nil, nil
}
func (m *mockChannelService) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}
func (m *mockChannelService) Test(ctx context.Context, id string) error {
	if m.testFunc != nil {
		return m.testFunc(ctx, id)
	}
	return nil
}

func newChannelTestRouter(svc service.ChannelService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewChannelHandler(svc)
	api := r.Group("/channels")
	{
		api.GET("", h.ListChannels)
		api.POST("", h.CreateChannel)
		api.PUT("/:id", h.UpdateChannel)
		api.DELETE("/:id", h.DeleteChannel)
		api.POST("/:id/test", h.TestChannel)
	}
	return r
}

func TestChannelHandler_ListChannels_空返200(t *testing.T) {
	svc := &mockChannelService{
		listFunc: func(_ context.Context) ([]models.NotificationChannel, error) {
			return []models.NotificationChannel{}, nil
		},
	}
	r := newChannelTestRouter(svc)
	req := httptest.NewRequest(http.MethodGet, "/channels", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestChannelHandler_CreateChannel_空name返400(t *testing.T) {
	svc := &mockChannelService{
		createFunc: func(_ context.Context, _ *models.NotificationChannel) error {
			return service.ErrInvalidInput
		},
	}
	r := newChannelTestRouter(svc)

	body, _ := json.Marshal(map[string]interface{}{"name": ""})
	req := httptest.NewRequest(http.MethodPost, "/channels", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestChannelHandler_DeleteChannel_成功(t *testing.T) {
	svc := &mockChannelService{
		deleteFunc: func(_ context.Context, _ string) error { return nil },
	}
	r := newChannelTestRouter(svc)
	req := httptest.NewRequest(http.MethodDelete, "/channels/"+uuid.NewString(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestChannelHandler_TestChannel_存在返200(t *testing.T) {
	svc := &mockChannelService{
		testFunc: func(_ context.Context, _ string) error { return nil },
	}
	r := newChannelTestRouter(svc)
	req := httptest.NewRequest(http.MethodPost, "/channels/"+uuid.NewString()+"/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ==================== Dashboard Handler 测试 ====================

type mockDashboardService struct {
	statsFunc       func(ctx context.Context) (*service.DashboardStats, error)
	alertTrendsFunc func(ctx context.Context, days int) ([]service.TrendPoint, error)
	kpisFunc        func(ctx context.Context, days int) (*service.KPI, error)
}

func (m *mockDashboardService) Stats(ctx context.Context) (*service.DashboardStats, error) {
	if m.statsFunc != nil {
		return m.statsFunc(ctx)
	}
	return &service.DashboardStats{}, nil
}
func (m *mockDashboardService) AlertTrends(ctx context.Context, days int) ([]service.TrendPoint, error) {
	if m.alertTrendsFunc != nil {
		return m.alertTrendsFunc(ctx, days)
	}
	return nil, nil
}
func (m *mockDashboardService) KPIs(ctx context.Context, days int) (*service.KPI, error) {
	if m.kpisFunc != nil {
		return m.kpisFunc(ctx, days)
	}
	return &service.KPI{WindowDays: days}, nil
}

func newDashboardTestRouter(svc service.DashboardService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewDashboardHandler(svc)
	r.GET("/dashboard/stats", h.GetDashboardStats)
	r.GET("/dashboard/trends", h.GetDashboardTrends)
	return r
}

func TestDashboardHandler_Stats_成功(t *testing.T) {
	svc := &mockDashboardService{
		statsFunc: func(_ context.Context) (*service.DashboardStats, error) {
			return &service.DashboardStats{Assets: 100, Alerts: 5, Tickets: 20}, nil
		},
	}
	r := newDashboardTestRouter(svc)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDashboardHandler_Trends_默认7天(t *testing.T) {
	svc := &mockDashboardService{
		alertTrendsFunc: func(_ context.Context, days int) ([]service.TrendPoint, error) {
			assert.Equal(t, 7, days)
			return []service.TrendPoint{{Date: "2026-06-15", Count: 3}}, nil
		},
	}
	r := newDashboardTestRouter(svc)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/trends", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ==================== User Handler 测试 ====================

type mockUserService struct {
	listFunc func(ctx context.Context, page, pageSize int) ([]models.User, int64, error)
	getFunc  func(ctx context.Context, id string) (*models.User, error)
}

func (m *mockUserService) List(ctx context.Context, page, pageSize int) ([]models.User, int64, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, page, pageSize)
	}
	return nil, 0, nil
}
func (m *mockUserService) Get(ctx context.Context, id string) (*models.User, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, service.ErrNotFound
}

func newUserTestRouter(svc service.UserService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewUserHandler(svc)
	r.GET("/users", h.ListUsers)
	r.GET("/users/:id", h.GetUser)
	return r
}

func TestUserHandler_ListUsers_成功(t *testing.T) {
	svc := &mockUserService{
		listFunc: func(_ context.Context, page, pageSize int) ([]models.User, int64, error) {
			assert.Equal(t, 1, page)
			assert.Equal(t, 20, pageSize)
			return []models.User{{Username: "admin"}}, 1, nil
		},
	}
	r := newUserTestRouter(svc)
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUserHandler_GetUser_不存在返404(t *testing.T) {
	svc := &mockUserService{
		getFunc: func(_ context.Context, _ string) (*models.User, error) {
			return nil, service.ErrNotFound
		},
	}
	r := newUserTestRouter(svc)
	req := httptest.NewRequest(http.MethodGet, "/users/"+uuid.NewString(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
