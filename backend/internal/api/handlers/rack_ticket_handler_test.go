package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	listFunc      func(ctx context.Context, f service.TicketFilter) ([]models.Ticket, int64, error)
	getFunc       func(ctx context.Context, id string) (*models.Ticket, error)
	createFunc    func(ctx context.Context, t *models.Ticket, a service.Actor) error
	updateFunc    func(ctx context.Context, id string, u map[string]interface{}, a service.Actor) (*models.Ticket, error)
	fromAlertFunc func(ctx context.Context, alertID, userID string) (*models.Ticket, bool, error)
}

func (m *mockTicketService) List(ctx context.Context, f service.TicketFilter) ([]models.Ticket, int64, error) {
	return m.listFunc(ctx, f)
}
func (m *mockTicketService) Get(ctx context.Context, id string) (*models.Ticket, error) {
	return m.getFunc(ctx, id)
}
func (m *mockTicketService) Create(ctx context.Context, t *models.Ticket, a service.Actor) error {
	return m.createFunc(ctx, t, a)
}
func (m *mockTicketService) Update(ctx context.Context, id string, u map[string]interface{}, a service.Actor) (*models.Ticket, error) {
	return m.updateFunc(ctx, id, u, a)
}
func (m *mockTicketService) CreateFromAlert(ctx context.Context, alertID, userID string) (*models.Ticket, bool, error) {
	return m.fromAlertFunc(ctx, alertID, userID)
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

// newAlertTicketRouter 按 routes.go 的挂法建一个最小路由：D-3 的端点虽然路径归 /alerts，
// handler 却在 TicketHandler 上（工单号生成与撞号处理都在 TicketService）。
// 中间件塞 username，与 JWT 中间件写入的键一致。
func newAlertTicketRouter(svc service.TicketService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewTicketHandler(svc)
	r.Use(func(c *gin.Context) {
		c.Set("username", "yanru")
		c.Next()
	})
	r.POST("/api/alerts/:id/ticket", h.CreateTicketFromAlert)
	return r
}

func TestTicketCreateFromAlert_新建返回201(t *testing.T) {
	var gotAlertID, gotUser string
	svc := &mockTicketService{
		fromAlertFunc: func(ctx context.Context, alertID, userID string) (*models.Ticket, bool, error) {
			gotAlertID, gotUser = alertID, userID
			return &models.Ticket{Title: "core-sw-01 CPU 高", Source: "alert", Priority: "normal"}, true, nil
		},
	}
	r := newAlertTicketRouter(svc)

	req := httptest.NewRequest("POST", "/api/alerts/a-1/ticket", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "a-1", gotAlertID, "路径参数应透传")
	assert.Equal(t, "yanru", gotUser, "requester 取 JWT 里的 username")

	var resp struct {
		Data struct {
			Ticket  models.Ticket `json:"ticket"`
			Created bool          `json:"created"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Data.Created)
	assert.Equal(t, "alert", resp.Data.Ticket.Source)
}

// 已关联时幂等返回既有票 —— 200 而非 201，前端据此决定要不要提示「已建单」。
func TestTicketCreateFromAlert_已关联返回200(t *testing.T) {
	svc := &mockTicketService{
		fromAlertFunc: func(ctx context.Context, alertID, userID string) (*models.Ticket, bool, error) {
			return &models.Ticket{Title: "既有工单", TicketNumber: "TICKET-20260910-A"}, false, nil
		},
	}
	r := newAlertTicketRouter(svc)

	req := httptest.NewRequest("POST", "/api/alerts/a-1/ticket", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "TICKET-20260910-A")
	assert.Contains(t, w.Body.String(), `"created":false`)
}

func TestTicketCreateFromAlert_告警不存在返回404(t *testing.T) {
	svc := &mockTicketService{
		fromAlertFunc: func(ctx context.Context, alertID, userID string) (*models.Ticket, bool, error) {
			return nil, false, service.ErrNotFound
		},
	}
	r := newAlertTicketRouter(svc)

	req := httptest.NewRequest("POST", "/api/alerts/missing/ticket", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not_found")
}

func TestTicketCreateFromAlert_DB错误不泄露(t *testing.T) {
	svc := &mockTicketService{
		fromAlertFunc: func(ctx context.Context, alertID, userID string) (*models.Ticket, bool, error) {
			return nil, false, errors.New("pq: duplicate key value violates unique constraint \"tickets_ticket_number_key\"")
		},
	}
	r := newAlertTicketRouter(svc)

	req := httptest.NewRequest("POST", "/api/alerts/a-1/ticket", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	body := w.Body.String()
	// 与 rack handler 同一条安全红线：原始 SQL 错误不得漏到客户端
	assert.NotContains(t, body, "pq:")
	assert.NotContains(t, body, "constraint")
	assert.NotContains(t, body, "tickets_ticket_number_key")
}

func TestTicketCreate_空标题_返回400(t *testing.T) {
	svc := &mockTicketService{
		createFunc: func(ctx context.Context, t *models.Ticket, a service.Actor) error {
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

// M18：`ErrInvalidInput` 这个分支现在承载两种原因（空标题 / 枚举列取值越界），
// 原先把文案写死成「工单标题不能为空」——枚举越界会被报成标题问题，调用方照着改标题
// 永远改不好。这条钉住 400 body 必须带得出**真实原因**。
func TestTicketCreate_枚举越界_返回400带原因(t *testing.T) {
	svc := &mockTicketService{
		createFunc: func(ctx context.Context, tk *models.Ticket, a service.Actor) error {
			return fmt.Errorf("%w: priority 取值超出契约词表", service.ErrInvalidInput)
		},
	}
	r := newTicketRouter(svc)

	body, _ := json.Marshal(models.Ticket{Title: "工单", Priority: "urgent"}) //nolint:exhaustruct
	req := httptest.NewRequest("POST", "/api/tickets", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "bad_request")
	assert.Contains(t, w.Body.String(), "priority 取值超出契约词表", "400 必须说明真实原因")
	assert.NotContains(t, w.Body.String(), "标题", "不得再套用写死的标题文案")
}

func TestTicketUpdate_关闭工单_updates透传给service(t *testing.T) {
	var capturedUpdates map[string]interface{}
	svc := &mockTicketService{
		updateFunc: func(ctx context.Context, id string, u map[string]interface{}, a service.Actor) (*models.Ticket, error) {
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

// 请求体带了 id/ticket_number/created_at 等系统维护列时，service 返回 ErrInvalidInput；
// handler 必须落 400 —— 漏了这个分支会变成 500，把「调用方写错字段」报成服务端故障，
// 运维看到 500 只会重试同样的请求。
func TestTicketUpdate_不可变字段返回400(t *testing.T) {
	svc := &mockTicketService{
		updateFunc: func(ctx context.Context, id string, u map[string]interface{}, a service.Actor) (*models.Ticket, error) {
			return nil, service.ErrInvalidInput
		},
	}
	r := newTicketRouter(svc)

	body, _ := json.Marshal(map[string]interface{}{"id": "11111111-1111-1111-1111-111111111111"})
	req := httptest.NewRequest("PUT", "/api/tickets/t-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "bad_request")
}
