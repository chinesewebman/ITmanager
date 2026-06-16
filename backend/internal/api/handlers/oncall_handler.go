package handlers

import (
	"net/http"
	"time"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// timeNow helper：方便测试时 mock
var timeNow = func() time.Time { return time.Now() }

// OncallHandler 值班 + 升级策略 HTTP 处理器（P1-2）
type OncallHandler struct {
	svc *service.OncallService
}

func NewOncallHandler(svc *service.OncallService) *OncallHandler {
	return &OncallHandler{svc: svc}
}

// ==================== Schedules ====================

func (h *OncallHandler) ListSchedules(c *gin.Context) {
	out, err := h.svc.ListSchedules(c.Request.Context())
	if err != nil {
		apierr.Internal(c, "列出值班组失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *OncallHandler) CreateSchedule(c *gin.Context) {
	var sched models.OncallSchedule
	if err := c.ShouldBindJSON(&sched); err != nil {
		apierr.BadRequest(c, "请求体格式错误: "+err.Error())
		return
	}
	sched.ID = uuid.Nil
	if err := h.svc.CreateSchedule(c.Request.Context(), &sched); err != nil {
		apierr.BadRequest(c, "创建失败: "+err.Error())
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": sched})
}

func (h *OncallHandler) DeleteSchedule(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.BadRequest(c, "ID 格式错误")
		return
	}
	if err := h.svc.DeleteSchedule(c.Request.Context(), id); err != nil {
		apierr.NotFound(c, "值班组不存在")
		return
	}
	c.Status(http.StatusNoContent)
}

// ==================== Shifts ====================

func (h *OncallHandler) ListShifts(c *gin.Context) {
	scheduleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.BadRequest(c, "schedule ID 格式错误")
		return
	}
	out, err := h.svc.ListShifts(c.Request.Context(), scheduleID)
	if err != nil {
		apierr.Internal(c, "列出班次失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *OncallHandler) CreateShift(c *gin.Context) {
	var shift models.OncallShift
	if err := c.ShouldBindJSON(&shift); err != nil {
		apierr.BadRequest(c, "请求体格式错误: "+err.Error())
		return
	}
	shift.ID = uuid.Nil
	if err := h.svc.CreateShift(c.Request.Context(), &shift); err != nil {
		apierr.BadRequest(c, "创建失败: "+err.Error())
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": shift})
}

func (h *OncallHandler) DeleteShift(c *gin.Context) {
	id, err := uuid.Parse(c.Param("shift_id"))
	if err != nil {
		apierr.BadRequest(c, "shift ID 格式错误")
		return
	}
	if err := h.svc.DeleteShift(c.Request.Context(), id); err != nil {
		apierr.NotFound(c, "班次不存在")
		return
	}
	c.Status(http.StatusNoContent)
}

// ==================== Current Oncall ====================

// GetCurrentOncall godoc
// @Summary      获取当前在班的 user
// @Description  跨所有 enabled schedule 查询当前在班的 user
// @Tags         值班
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}  models.OncallCurrent
// @Router       /oncall/current [get]
func (h *OncallHandler) GetCurrentOncall(c *gin.Context) {
	out, err := h.svc.GetCurrentOncall(c.Request.Context(), timeNow())
	if err != nil {
		apierr.Internal(c, "查询失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

// ==================== Policies ====================

func (h *OncallHandler) ListPolicies(c *gin.Context) {
	out, err := h.svc.ListPolicies(c.Request.Context())
	if err != nil {
		apierr.Internal(c, "列出升级策略失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *OncallHandler) CreatePolicy(c *gin.Context) {
	var policy models.EscalationPolicy
	if err := c.ShouldBindJSON(&policy); err != nil {
		apierr.BadRequest(c, "请求体格式错误: "+err.Error())
		return
	}
	policy.ID = uuid.Nil
	if err := h.svc.CreatePolicy(c.Request.Context(), &policy); err != nil {
		apierr.BadRequest(c, "创建失败: "+err.Error())
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": policy})
}

func (h *OncallHandler) GetPolicy(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.BadRequest(c, "ID 格式错误")
		return
	}
	out, err := h.svc.GetPolicy(c.Request.Context(), id)
	if err != nil {
		apierr.NotFound(c, "策略不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *OncallHandler) DeletePolicy(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.BadRequest(c, "ID 格式错误")
		return
	}
	if err := h.svc.DeletePolicy(c.Request.Context(), id); err != nil {
		apierr.NotFound(c, "策略不存在")
		return
	}
	c.Status(http.StatusNoContent)
}
