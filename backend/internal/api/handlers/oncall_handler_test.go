package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newOncallHandlerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, s := range []string{
		`CREATE TABLE oncall_schedules (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT,
			timezone TEXT DEFAULT 'Asia/Shanghai', enabled INTEGER DEFAULT 1,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE oncall_shifts (
			id TEXT PRIMARY KEY, schedule_id TEXT NOT NULL, user_id TEXT NOT NULL,
			user_name TEXT, starts_at DATETIME NOT NULL, ends_at DATETIME NOT NULL,
			created_at DATETIME
		)`,
		`CREATE TABLE escalation_policies (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, enabled INTEGER DEFAULT 1,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE escalation_levels (
			id TEXT PRIMARY KEY, policy_id TEXT NOT NULL, level INTEGER NOT NULL,
			target_type TEXT, target_id TEXT, wait_minutes INTEGER DEFAULT 5,
			notify_methods TEXT
		)`,
	} {
		require.NoError(t, db.Exec(s).Error)
	}
	return db
}

func setupOncallRouter(t *testing.T) (*gin.Engine, *service.OncallService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := newOncallHandlerDB(t)
	svc := service.NewOncallService(db)
	h := NewOncallHandler(svc)
	r := gin.New()
	g := r.Group("/oncall")
	g.GET("/current", h.GetCurrentOncall)
	g.GET("/schedules", h.ListSchedules)
	g.POST("/schedules", h.CreateSchedule)
	g.DELETE("/schedules/:id", h.DeleteSchedule)
	g.GET("/schedules/:id/shifts", h.ListShifts)
	g.POST("/schedules/:id/shifts", h.CreateShift)
	g.DELETE("/shifts/:shift_id", h.DeleteShift)
	g.GET("/policies", h.ListPolicies)
	g.POST("/policies", h.CreatePolicy)
	g.GET("/policies/:id", h.GetPolicy)
	g.DELETE("/policies/:id", h.DeletePolicy)
	return r, svc
}

func doJSONOncall(t *testing.T, r *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ==================== Schedule Handler ====================

func TestOncallHandler_ListSchedules_空返空数组(t *testing.T) {
	r, _ := setupOncallRouter(t)
	w := doJSONOncall(t, r, "GET", "/oncall/schedules", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"data":[]`)
}

func TestOncallHandler_CreateSchedule_成功返201(t *testing.T) {
	r, _ := setupOncallRouter(t)
	w := doJSONOncall(t, r, "POST", "/oncall/schedules", models.OncallSchedule{Name: "team-a"})
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"name":"team-a"`)
}

func TestOncallHandler_CreateSchedule_空name返400(t *testing.T) {
	r, _ := setupOncallRouter(t)
	w := doJSONOncall(t, r, "POST", "/oncall/schedules", models.OncallSchedule{})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestOncallHandler_DeleteSchedule_成功返204(t *testing.T) {
	r, svc := setupOncallRouter(t)
	sched := &models.OncallSchedule{Name: "x"}
	require.NoError(t, svc.CreateSchedule(context.Background(), sched))
	w := doJSONOncall(t, r, "DELETE", "/oncall/schedules/"+sched.ID.String(), nil)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestOncallHandler_DeleteSchedule_不存在返404(t *testing.T) {
	r, _ := setupOncallRouter(t)
	w := doJSONOncall(t, r, "DELETE", "/oncall/schedules/"+uuid.New().String(), nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ==================== Shift Handler ====================

func TestOncallHandler_ListShifts_无效UUID返400(t *testing.T) {
	r, _ := setupOncallRouter(t)
	w := doJSONOncall(t, r, "GET", "/oncall/schedules/not-a-uuid/shifts", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestOncallHandler_CreateShift_成功返201(t *testing.T) {
	r, svc := setupOncallRouter(t)
	sched := &models.OncallSchedule{Name: "x"}
	require.NoError(t, svc.CreateSchedule(context.Background(), sched))

	now := timeNow()
	shift := models.OncallShift{
		ScheduleID: sched.ID, UserID: uuid.New(),
		StartsAt: now, EndsAt: now.Add(time.Hour),
	}
	w := doJSONOncall(t, r, "POST", "/oncall/schedules/"+sched.ID.String()+"/shifts", shift)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestOncallHandler_DeleteShift_成功返204(t *testing.T) {
	r, svc := setupOncallRouter(t)
	sched := &models.OncallSchedule{Name: "x"}
	require.NoError(t, svc.CreateSchedule(context.Background(), sched))
	now := timeNow()
	shift := &models.OncallShift{
		ScheduleID: sched.ID, UserID: uuid.New(),
		StartsAt: now, EndsAt: now.Add(time.Hour),
	}
	require.NoError(t, svc.CreateShift(context.Background(), shift))

	w := doJSONOncall(t, r, "DELETE", "/oncall/shifts/"+shift.ID.String(), nil)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

// ==================== Current Oncall ====================

func TestOncallHandler_GetCurrentOncall_空返200(t *testing.T) {
	r, _ := setupOncallRouter(t)
	w := doJSONOncall(t, r, "GET", "/oncall/current", nil)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ==================== Policy Handler ====================

func TestOncallHandler_ListPolicies_空返空(t *testing.T) {
	r, _ := setupOncallRouter(t)
	w := doJSONOncall(t, r, "GET", "/oncall/policies", nil)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestOncallHandler_CreatePolicy_成功返201(t *testing.T) {
	r, _ := setupOncallRouter(t)
	policy := models.EscalationPolicy{
		Name: "p1",
		Levels: []models.EscalationLevel{
			{Level: 1, TargetType: "user", TargetID: "u1"},
		},
	}
	w := doJSONOncall(t, r, "POST", "/oncall/policies", policy)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestOncallHandler_GetPolicy_不存在返404(t *testing.T) {
	r, _ := setupOncallRouter(t)
	w := doJSONOncall(t, r, "GET", "/oncall/policies/"+uuid.New().String(), nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestOncallHandler_GetPolicy_无效UUID返400(t *testing.T) {
	r, _ := setupOncallRouter(t)
	w := doJSONOncall(t, r, "GET", "/oncall/policies/not-a-uuid", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestOncallHandler_DeletePolicy_不存在返404(t *testing.T) {
	r, _ := setupOncallRouter(t)
	w := doJSONOncall(t, r, "DELETE", "/oncall/policies/"+uuid.New().String(), nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
