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
	"github.com/stretchr/testify/assert"
)

// mockRackService 演示 service 接口的可测试性
type mockRackService struct {
	listRacksFunc  func(ctx context.Context, siteID string) ([]service.RackDTO, error)
	getRackFunc    func(ctx context.Context, id string) (*service.RackDTO, error)
	getDevicesFunc func(ctx context.Context, rackID string) ([]service.RackDevice, error)
	listSitesFunc  func(ctx context.Context) ([]models.Site, error)
	getSiteFunc    func(ctx context.Context, id string) (*service.SiteDetail, error)
}

func (m *mockRackService) ListRacks(ctx context.Context, siteID string) ([]service.RackDTO, error) {
	return m.listRacksFunc(ctx, siteID)
}
func (m *mockRackService) GetRack(ctx context.Context, id string) (*service.RackDTO, error) {
	return m.getRackFunc(ctx, id)
}
func (m *mockRackService) GetRackDevices(ctx context.Context, id string) ([]service.RackDevice, error) {
	return m.getDevicesFunc(ctx, id)
}
func (m *mockRackService) ListSites(ctx context.Context) ([]models.Site, error) {
	return m.listSitesFunc(ctx)
}
func (m *mockRackService) GetSite(ctx context.Context, id string) (*service.SiteDetail, error) {
	return m.getSiteFunc(ctx, id)
}

func newRackRouter(svc service.RackService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewRackHandler(svc)
	g := r.Group("/api")
	g.GET("/racks", h.ListRacks)
	g.GET("/racks/:id", h.GetRack)
	g.GET("/racks/:id/devices", h.GetRackDevices)
	g.GET("/sites", h.ListSites)
	g.GET("/sites/:id", h.GetSite)
	return r
}

func TestRackList_带site_id筛选_透传到service(t *testing.T) {
	var capturedSiteID string
	svc := &mockRackService{
		listRacksFunc: func(ctx context.Context, siteID string) ([]service.RackDTO, error) {
			capturedSiteID = siteID
			return []service.RackDTO{{Name: "Rack-A01"}}, nil
		},
	}
	r := newRackRouter(svc)

	req := httptest.NewRequest("GET", "/api/racks?site_id=dc-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "dc-1", capturedSiteID)
}

func TestRackGet_不泄露DB错误(t *testing.T) {
	svc := &mockRackService{
		getRackFunc: func(ctx context.Context, id string) (*service.RackDTO, error) {
			return nil, errors.New("pq: column 'foo' does not exist (SQLSTATE 42703)")
		},
	}
	r := newRackRouter(svc)

	req := httptest.NewRequest("GET", "/api/racks/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	body := w.Body.String()
	// 关键安全断言：原始 SQL 错误不能漏到客户端
	assert.NotContains(t, body, "pq:")
	assert.NotContains(t, body, "SQLSTATE")
	assert.NotContains(t, body, "column")
}

func TestRackGet_不存在_统一404结构(t *testing.T) {
	svc := &mockRackService{
		getRackFunc: func(ctx context.Context, id string) (*service.RackDTO, error) {
			return nil, service.ErrNotFound
		},
	}
	r := newRackRouter(svc)

	req := httptest.NewRequest("GET", "/api/racks/missing", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	var resp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "not_found", resp.Code)
}

// ---- Ticket handler 测试 ----

type mockTicketService struct {
	listFunc   func(ctx context.Context, f service.TicketFilter) ([]models.Ticket, int64, error)
	getFunc    func(ctx context.Context, id string) (*models.Ticket, error)
	createFunc func(ctx context.Context, t *models.Ticket) error
	updateFunc func(ctx context.Context, id string, u map[string]interface{}) (*models.Ticket, error)
}

func (m *mockTicketService) List(ctx context.Context, f service.TicketFilter) ([]models.Ticket, int64, error) {
	return m.listFunc(ctx, f)
}
func (m *mockTicketService) Get(ctx context.Context, id string) (*models.Ticket, error) {
	return m.getFunc(ctx, id)
}
func (m *mockTicketService) Create(ctx context.Context, t *models.Ticket) error {
	return m.createFunc(ctx, t)
}
func (m *mockTicketService) Update(ctx context.Context, id string, u map[string]interface{}) (*models.Ticket, error) {
	return m.updateFunc(ctx, id, u)
}

func newTicketRouter(svc service.TicketService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewTicketHandler(svc)
	g := r.Group("/api/tickets")
	g.GET("", h.ListTickets)
	g.GET("/:id", h.GetTicket)
	g.POST("", h.CreateTicket)
	g.PUT("/:id", h.UpdateTicket)
	return r
}

func TestTicketCreate_空标题_返回400(t *testing.T) {
	svc := &mockTicketService{
		createFunc: func(ctx context.Context, t *models.Ticket) error {
			return service.ErrInvalidInput // service 层校验标题
		},
	}
	r := newTicketRouter(svc)

	bad := models.Ticket{Title: ""}
	body, _ := json.Marshal(bad)
	req := httptest.NewRequest("POST", "/api/tickets", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "bad_request")
}

func TestTicketUpdate_关闭工单_updates透传给service(t *testing.T) {
	var capturedUpdates map[string]interface{}
	svc := &mockTicketService{
		updateFunc: func(ctx context.Context, id string, u map[string]interface{}) (*models.Ticket, error) {
			capturedUpdates = u
			return &models.Ticket{Title: "测试", Status: "closed"}, nil
		},
	}
	r := newTicketRouter(svc)

	updates := map[string]interface{}{"status": "closed"}
	body, _ := json.Marshal(updates)
	req := httptest.NewRequest("PUT", "/api/tickets/t-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// 验证 handler 把 status=closed 完整透传给 service
	assert.Equal(t, "closed", capturedUpdates["status"])
	// 注："关闭工单自动写入 closed_at"是 service 层的职责，应在 service 单元测试中验证
}
