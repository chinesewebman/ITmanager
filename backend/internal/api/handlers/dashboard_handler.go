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
