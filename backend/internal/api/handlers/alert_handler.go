package handlers

import (
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"time"

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
	severity, _ := strconv.Atoi(c.DefaultQuery("severity", "0")) // 🐛 BUG#13: severity 改 int

	items, stats, err := h.svc.List(c.Request.Context(), service.AlertFilter{
		Status:   c.Query("status"),
		Severity: severity,
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

// MarkFalsePositiveRequest 标记/反标记误报请求体
// is_false_positive=true  标记为误报（写 marked_by/marked_at/note）
// is_false_positive=false 反标记（清空 FP 元数据）
type MarkFalsePositiveRequest struct {
	IsFalsePositive bool   `json:"is_false_positive"`
	Note            string `json:"note"` // 备注（仅标记时生效，反标记忽略）
}

// MarkFalsePositive POST /alerts/:id/mark-fp
// 标记或反标记指定告警为误报。返回更新后的 alert。
func (h *AlertHandler) MarkFalsePositive(c *gin.Context) {
	userID := c.GetString("username")
	if userID == "" {
		userID = "unknown"
	}
	var req MarkFalsePositiveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	alert, err := h.svc.MarkFalsePositive(c.Request.Context(), c.Param("id"), userID, req.Note, req.IsFalsePositive)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "告警不存在")
			return
		}
		apierr.Internal(c, "标记误报失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "操作成功",
		"data":    alert,
	})
}

// ExportFalsePositives GET /alerts/false-positives/export
// 导出被标记为误报的告警为 CSV 格式（ML 训练集）。
// query `since`（RFC3339）可选：增量导出 marked_at >= since 的记录。
// 复用 asset_handler 的 safeCSV 防 Excel 公式注入。
func (h *AlertHandler) ExportFalsePositives(c *gin.Context) {
	var since *time.Time
	if sinceStr := c.Query("since"); sinceStr != "" {
		t, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			apierr.BadRequest(c, "since 格式错误（需 RFC3339）")
			return
		}
		since = &t
	}
	items, err := h.svc.ListFalsePositives(c.Request.Context(), since)
	if err != nil {
		apierr.Internal(c, "导出误报失败", err)
		return
	}

	// C-F7: 复用 safeCSV 防 Excel DDE 公式注入
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="false_positives.csv"`)
	c.Header("X-Content-Type-Options", "nosniff")

	w := csv.NewWriter(c.Writer)
	defer w.Flush()

	// 表头：ML 训练常用特征
	_ = w.Write([]string{
		"alert_id", "host_name", "host_ip", "trigger_name", "trigger_id",
		"severity", "severity_name", "problem", "problem_start", "duration_seconds",
		"is_false_positive", "marked_by", "marked_at", "false_positive_note",
	})
	for _, a := range items {
		markedBy := ""
		if a.MarkedBy != nil {
			markedBy = *a.MarkedBy
		}
		markedAt := ""
		if a.MarkedAt != nil {
			markedAt = a.MarkedAt.UTC().Format(time.RFC3339)
		}
		note := ""
		if a.FalsePositiveNote != nil {
			note = *a.FalsePositiveNote
		}
		_ = w.Write([]string{
			safeCSV(a.AlertID),
			safeCSV(a.HostName),
			safeCSV(a.HostIP),
			safeCSV(a.TriggerName),
			safeCSV(a.TriggerID),
			strconv.Itoa(a.Severity),
			safeCSV(a.SeverityName),
			safeCSV(a.Problem),
			a.ProblemStart.UTC().Format(time.RFC3339),
			strconv.Itoa(a.Duration),
			"1", // is_false_positive 全为 1
			safeCSV(markedBy),
			markedAt,
			safeCSV(note),
		})
	}
}
