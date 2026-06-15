package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
)

// AlertHandler 告警相关 HTTP handler
type AlertHandler struct {
	svc service.AlertService
}

func NewAlertHandler(svc service.AlertService) *AlertHandler {
	return &AlertHandler{svc: svc}
}

// ListAlerts 告警列表（带统计）
func (h *AlertHandler) ListAlerts(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))

	items, stats, err := h.svc.List(c.Request.Context(), service.AlertFilter{
		Status:   c.Query("status"),
		Severity: c.Query("severity"),
		HostID:   c.Query("host_id"),
		Limit:    limit,
	})
	if err != nil {
		apierr.Internal(c, "获取告警列表失败", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"items": items,
			"stats": stats,
		},
	})
}

// GetAlert 告警详情
func (h *AlertHandler) GetAlert(c *gin.Context) {
	alert, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "告警不存在")
			return
		}
		apierr.Internal(c, "获取告警失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": alert,
	})
}

// AcknowledgeAlert 确认告警
func (h *AlertHandler) AcknowledgeAlert(c *gin.Context) {
	userID := c.GetString("username") // JWT 中间件写入
	if userID == "" {
		userID = "unknown"
	}
	if err := h.svc.Acknowledge(c.Request.Context(), c.Param("id"), userID); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "告警不存在")
			return
		}
		apierr.Internal(c, "确认告警失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "告警已确认",
	})
}

// ResolveAlert 解决告警
func (h *AlertHandler) ResolveAlert(c *gin.Context) {
	userID := c.GetString("username")
	if userID == "" {
		userID = "unknown"
	}
	if err := h.svc.Resolve(c.Request.Context(), c.Param("id"), userID); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "告警不存在")
			return
		}
		apierr.Internal(c, "解决告警失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "告警已解决",
	})
}

// GetAlertStats 告警按严重级别/小时统计
func (h *AlertHandler) GetAlertStats(c *gin.Context) {
	bySev, byHour, err := h.svc.Stats(c.Request.Context())
	if err != nil {
		apierr.Internal(c, "获取告警统计失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"by_severity": bySev,
			"by_hour":     byHour,
		},
	})
}

// ListAlertRules 告警规则列表
func (h *AlertHandler) ListAlertRules(c *gin.Context) {
	rules, err := h.svc.ListRules(c.Request.Context())
	if err != nil {
		apierr.Internal(c, "获取告警规则列表失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": rules,
	})
}

// CreateAlertRule 创建告警规则
func (h *AlertHandler) CreateAlertRule(c *gin.Context) {
	var rule models.AlertRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	if err := h.svc.CreateRule(c.Request.Context(), &rule); err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, "规则数据不完整")
			return
		}
		apierr.Internal(c, "创建告警规则失败", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"code": 0,
		"data": rule,
	})
}

// UpdateAlertRule 更新告警规则
func (h *AlertHandler) UpdateAlertRule(c *gin.Context) {
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	rule, err := h.svc.UpdateRule(c.Request.Context(), c.Param("id"), updates)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "告警规则不存在")
			return
		}
		apierr.Internal(c, "更新告警规则失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": rule,
	})
}

// DeleteAlertRule 删除
func (h *AlertHandler) DeleteAlertRule(c *gin.Context) {
	if err := h.svc.DeleteRule(c.Request.Context(), c.Param("id")); err != nil {
		apierr.Internal(c, "删除告警规则失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}

// BulkRequest C-P6: 批量操作请求体
type BulkRequest struct {
	IDs []string `json:"ids" binding:"required"`
}

// BulkAcknowledge C-P6: 批量确认告警（POST /alerts/bulk-ack）。
// 单次 SQL UPDATE，避免 N 次循环。
func (h *AlertHandler) BulkAcknowledge(c *gin.Context) {
	userID := c.GetString("username")
	if userID == "" {
		userID = "unknown"
	}
	var req BulkRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		apierr.BadRequest(c, "ids 不能为空")
		return
	}
	// 防御性：上限保护（防 DoS）
	if len(req.IDs) > 1000 {
		apierr.BadRequest(c, "单次批量最多 1000 条")
		return
	}
	affected, err := h.svc.BulkAcknowledge(c.Request.Context(), req.IDs, userID)
	if err != nil {
		apierr.Internal(c, "批量确认失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"affected": affected},
	})
}

// BulkResolve C-P6: 批量解决告警。
func (h *AlertHandler) BulkResolve(c *gin.Context) {
	userID := c.GetString("username")
	if userID == "" {
		userID = "unknown"
	}
	var req BulkRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		apierr.BadRequest(c, "ids 不能为空")
		return
	}
	if len(req.IDs) > 1000 {
		apierr.BadRequest(c, "单次批量最多 1000 条")
		return
	}
	affected, err := h.svc.BulkResolve(c.Request.Context(), req.IDs, userID)
	if err != nil {
		apierr.Internal(c, "批量解决失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"affected": affected},
	})
}

// BulkDelete C-P6: 批量删除告警。
func (h *AlertHandler) BulkDelete(c *gin.Context) {
	var req BulkRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		apierr.BadRequest(c, "ids 不能为空")
		return
	}
	if len(req.IDs) > 1000 {
		apierr.BadRequest(c, "单次批量最多 1000 条")
		return
	}
	affected, err := h.svc.BulkDelete(c.Request.Context(), req.IDs)
	if err != nil {
		apierr.Internal(c, "批量删除失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"affected": affected},
	})
}
