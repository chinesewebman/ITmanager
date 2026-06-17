package handlers

import (
	"net/http"
	"strconv"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
)

// DashboardHandler 仪表盘 HTTP handler
type DashboardHandler struct {
	svc service.DashboardService
}

func NewDashboardHandler(svc service.DashboardService) *DashboardHandler {
	return &DashboardHandler{svc: svc}
}

func (h *DashboardHandler) GetDashboardStats(c *gin.Context) {
	stats, err := h.svc.Stats(c.Request.Context())
	if err != nil {
		apierr.Internal(c, "获取仪表盘统计失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": stats})
}

func (h *DashboardHandler) GetDashboardTrends(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	points, err := h.svc.AlertTrends(c.Request.Context(), days)
	if err != nil {
		apierr.Internal(c, "获取告警趋势失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"alert_trends": points},
	})
}

// GetKPIs godoc
// @Summary      关键 KPI 指标
// @Description  返回 MTTR / MTTD / 告警密度 / SLA 达成率
// @Description  days 默认 7，范围 1-90
// @Tags         dashboard
// @Produce      json
// @Security     BearerAuth
// @Param        days  query     int  false  "窗口天数，默认 7，范围 1-90"
// @Success      200   {object}  service.KPI
// @Failure      401   {object}  apierr.ErrorResponse
// @Router       /dashboard/kpis [get]
func (h *DashboardHandler) GetKPIs(c *gin.Context) {
	days := 7
	if v := c.Query("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			apierr.BadRequest(c, "days 必须是正整数")
			return
		}
		days = n
	}
	kpi, err := h.svc.KPIs(c.Request.Context(), days)
	if err != nil {
		apierr.Internal(c, "获取 KPI 失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": kpi,
	})
}
