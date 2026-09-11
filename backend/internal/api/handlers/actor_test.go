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

// newTicketRouterWithActor 与 newTicketRouter 同构，但按 auth 中间件的口径塞入
// user_id / username —— actorFromContext 的全部输入就是这两个键。
// 空串表示「中间件没写这个键」，用来覆盖非常规路径。
func newTicketRouterWithActor(svc service.TicketService, username, uid string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewTicketHandler(svc)
	r.Use(func(c *gin.Context) {
		if username != "" {
			c.Set("username", username)
		}
		if uid != "" {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	r.PUT("/api/tickets/:id", h.UpdateTicket)
	r.POST("/api/tickets", h.CreateTicket)
	r.POST("/api/alerts/:id/ticket", h.CreateTicketFromAlert)
	return r
}

// 建单也要记「谁经手的」：出生行的 actor 与 Update 走同一个 helper，
// 这里钉住 handler 真的把它传下去了 —— 漏传不会报错，只会让每次建单的出生记录
// 都变成「无名人建的」，而事后无法补。
func TestCreateTicket_经手人从ctx解析并传给service(t *testing.T) {
	const uid = "3f2504e0-4f89-11d3-9a0c-0305e82c3301"
	var got service.Actor
	svc := &mockTicketService{
		createFunc: func(ctx context.Context, tk *models.Ticket, a service.Actor) error {
			got = a
			tk.ID = uuid.MustParse(uid)
			return nil
		},
	}
	r := newTicketRouterWithActor(svc, "alice", uid)

	body, _ := json.Marshal(models.Ticket{Title: "新工单"}) //nolint:exhaustruct
	req := httptest.NewRequest("POST", "/api/tickets", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "alice", got.Name)
	if assert.NotNil(t, got.ID, "合法 user_id 必须解析成 Actor.ID") {
		assert.Equal(t, uid, got.ID.String())
	}
}

// 经手人解析的正路：JWT 中间件写的 username + user_id 都要落进 Actor，
// 且必须原样传给 service —— 漏了 ID 历史就查不到人，传错则是把操作记到别人头上。
func TestUpdateTicket_经手人从ctx解析并传给service(t *testing.T) {
	const uid = "3f2504e0-4f89-11d3-9a0c-0305e82c3301"
	var got service.Actor
	svc := &mockTicketService{
		updateFunc: func(ctx context.Context, id string, u map[string]interface{}, a service.Actor) (*models.Ticket, error) {
			got = a
			return &models.Ticket{Title: "测试", Status: "closed"}, nil
		},
	}
	r := newTicketRouterWithActor(svc, "alice", uid)

	body, _ := json.Marshal(map[string]interface{}{"status": "closed"})
	req := httptest.NewRequest("PUT", "/api/tickets/t-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "alice", got.Name, "username 是历史里的经手人姓名快照")
	if assert.NotNil(t, got.ID, "合法 user_id 必须解析成 Actor.ID") {
		assert.Equal(t, uid, got.ID.String())
	}
}

// user_id 缺失或非法时不阻断写入：宁可有名无 id，也不能把「谁经手」整个丢掉
// （真实来源：测试基座与内部调用路径下中间件可能只写了 username）。
// 反面守住「解析失败就填零值 uuid」——那会让历史里出现一个指向 00000000 的假外键。
func TestUpdateTicket_user_id非法_ID留空但姓名保留(t *testing.T) {
	var got service.Actor
	svc := &mockTicketService{
		updateFunc: func(ctx context.Context, id string, u map[string]interface{}, a service.Actor) (*models.Ticket, error) {
			got = a
			return &models.Ticket{Title: "测试"}, nil
		},
	}
	r := newTicketRouterWithActor(svc, "alice", "not-a-uuid")

	body, _ := json.Marshal(map[string]interface{}{"status": "closed"})
	req := httptest.NewRequest("PUT", "/api/tickets/t-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "alice", got.Name, "user_id 坏掉不该连姓名一起丢")
	assert.Nil(t, got.ID, "非法 user_id 必须是 nil，不能是零值 uuid")
}

// username 也缺失时兜底 "unknown" —— 与改动前 CreateTicketFromAlert 的行为一致。
// 若这里变成空串，历史行会留一个空姓名，事后完全分不清是「没记录」还是「记录为空」。
func TestUpdateTicket_username缺失_兜底unknown(t *testing.T) {
	var got service.Actor
	svc := &mockTicketService{
		updateFunc: func(ctx context.Context, id string, u map[string]interface{}, a service.Actor) (*models.Ticket, error) {
			got = a
			return &models.Ticket{Title: "测试"}, nil
		},
	}
	r := newTicketRouterWithActor(svc, "", "")

	body, _ := json.Marshal(map[string]interface{}{"status": "closed"})
	req := httptest.NewRequest("PUT", "/api/tickets/t-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "unknown", got.Name)
	assert.Nil(t, got.ID)
}

// 从告警建单的 requester 收的是 **username**（旧签名就是字符串），不是 user_id。
// ctx 里同时有两者时钉住这一点：写成 uuid 字符串的话，工单 requester 会变成一串
// 数字，人看了不知道是谁，按姓名检索也查不到。
func TestCreateTicketFromAlert_requester取username而非user_id(t *testing.T) {
	var gotUser string
	svc := &mockTicketService{
		fromAlertFunc: func(ctx context.Context, alertID, userID string) (*models.Ticket, bool, error) {
			gotUser = userID
			return &models.Ticket{Title: "工单"}, true, nil
		},
	}
	r := newTicketRouterWithActor(svc, "alice", "3f2504e0-4f89-11d3-9a0c-0305e82c3301")

	req := httptest.NewRequest("POST", "/api/alerts/a-1/ticket", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "alice", gotUser, "requester 要的是可读姓名，不是 uuid")
}
