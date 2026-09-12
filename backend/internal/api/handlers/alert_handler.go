package handlers

import (
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"time"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/cursor"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AlertHandler 告警相关 HTTP handler
type AlertHandler struct {
	svc service.AlertService
}

func NewAlertHandler(svc service.AlertService) *AlertHandler {
	return &AlertHandler{svc: svc}
}

// alertPathID 取并校验路径上的 :id，非法则写 400 并返回 false。
//
// 为什么要校验：`:id` 是裸字符串，直接进 gorm 的 `WHERE id = ?` 会与 **UUID 列**比较，
// Postgres 报 `22P02 invalid input syntax for type uuid`（已实测）。该错误既不是
// ErrRecordNotFound 也不是哨兵错误，于是经 apierr.Internal 变成 **500** ——
// 把「调用方把 id 写错了」报成「服务端故障」，调用方只会重试同样的请求。
// 正确动作是改请求，所以是 400（同 oncall_handler.go 等既有端点的做法）。
//
// 只校验不转换：service 层签名收的是 string，这里不做无谓的类型改写。
func alertPathID(c *gin.Context) (string, bool) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		apierr.BadRequest(c, "ID 格式错误")
		return "", false
	}
	return id, true
}

// ListAlerts 告警列表（带统计, v2.0 支持 cursor 分页）
func (h *AlertHandler) ListAlerts(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	severity, _ := strconv.Atoi(c.DefaultQuery("severity", "0")) // 🐛 BUG#13: severity 改 int
	page, _ := strconv.Atoi(c.DefaultQuery("page", "0"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "0"))

	filter := service.AlertFilter{
		Status:       c.Query("status"),
		Severity:     severity,
		HostID:       c.Query("host_id"),
		Limit:        limit,
		Page:         page,
		PageSize:     pageSize,
		IncludeStats: true, // HTTP 列表页需要 stats 全表聚合（统计卡）
	}
	// v2.0 cursor 分页: ?cursor=xxx (从 response.next_cursor 拿)
	cursorMode := false
	if cursorStr := c.Query("cursor"); cursorStr != "" {
		ts, id, err := cursor.Decode(cursorStr)
		if err == nil {
			filter.CursorTS = ts
			filter.CursorID = id
			cursorMode = true
		}
		// err 时降级到 v1.x 行为 (忽略 cursor)
	}

	items, stats, total, err := h.svc.List(c.Request.Context(), filter)
	if err != nil {
		apierr.Internal(c, "获取告警列表失败", err)
		return
	}

	resp := gin.H{
		"items": items,
		"stats": stats,
		"total": total,
	}
	// v2.0: 返 next_cursor (用最后一条 item 的 created_at + id)。只在 cursor 模式设置——
	// offset 模式用 total + page 翻页，next_cursor 无意义（page_size 可能 == limit 造成误判）。
	if cursorMode && len(items) == limit && !items[len(items)-1].CreatedAt.IsZero() {
		last := items[len(items)-1]
		resp["next_cursor"] = cursor.Encode(last.CreatedAt, last.ID)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": resp})
}

// GetAlert 告警详情
func (h *AlertHandler) GetAlert(c *gin.Context) {
	id, ok := alertPathID(c)
	if !ok {
		return
	}
	alert, err := h.svc.Get(c.Request.Context(), id)
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
	id, ok := alertPathID(c)
	if !ok {
		return
	}
	userID := c.GetString("username") // JWT 中间件写入
	if userID == "" {
		userID = "unknown"
	}
	if err := h.svc.Acknowledge(c.Request.Context(), id, userID); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "告警不存在")
			return
		}
		// M19: 对已解决的告警再确认 → 409。落 500 会让调用方以为服务端故障而重试，
		// 落 400 会被当成「参数写错了」去改参数 —— 两者都指错方向，正确的动作是刷新列表。
		// 用 err.Error() 而不是写死文案（同 M18）：这个分支将来可能承载多种状态冲突原因。
		if errors.Is(err, service.ErrInvalidState) {
			apierr.Conflict(c, err.Error())
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
	id, ok := alertPathID(c)
	if !ok {
		return
	}
	userID := c.GetString("username")
	if userID == "" {
		userID = "unknown"
	}
	if err := h.svc.Resolve(c.Request.Context(), id, userID); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "告警不存在")
			return
		}
		// M19: 同 AcknowledgeAlert —— 状态冲突是 409，不是 500/400
		if errors.Is(err, service.ErrInvalidState) {
			apierr.Conflict(c, err.Error())
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
	id, ok := alertPathID(c)
	if !ok {
		return
	}
	rule, err := h.svc.UpdateRule(c.Request.Context(), id, updates)
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
	id, ok := alertPathID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteRule(c.Request.Context(), id); err != nil {
		apierr.Internal(c, "删除告警规则失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}

// ==================== M38-B: triggerid → rule_id 映射 CRUD ====================
//
// POST   /api/alert-rules/:id/triggers  body { "triggerid": "zabbix-trigger-123" }
// GET    /api/alert-rules/:id/triggers  query ?rule_id=:id 可省（运营总览）
// DELETE /api/alert-rules/:id/triggers  query ?triggerid=... 必填
//
// 路径子段 :id = ruleID（与现有 /:id 路由形状保持一致，避免另起 /rule-triggers）；
// 静态段 /triggers 比 :id/path 早注册（Gin 默认按声明顺序匹配，已在 routes.go 处理）。
//
// 状态码：
//   400 — 非法 UUID / 缺 triggerid / 空 triggerid
//   404 — rule 不存在 (CreateMapping)
//   409 — 留作未来扩展（当前 last-write-wins 不抛 409）
//   500 — 真错

// CreateTriggerMapping POST /api/alert-rules/:id/triggers
// body: {"triggerid":"..."}  → 201 + 写入行；同 triggerid 后写覆盖前写
func (h *AlertHandler) CreateTriggerMapping(c *gin.Context) {
	ruleID, ok := alertPathID(c)
	if !ok {
		return
	}
	var req struct {
		TriggerID string `json:"triggerid" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TriggerID == "" {
		apierr.BadRequest(c, "triggerid 必填")
		return
	}
	row, err := h.svc.CreateMapping(c.Request.Context(), ruleID, req.TriggerID)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "告警规则不存在")
			return
		}
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, "参数错误 (id 必须为 UUID, triggerid 非空)")
			return
		}
		apierr.Internal(c, "创建映射失败", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"code": 0,
		"data": row,
	})
}

// ListTriggerMappings GET /api/alert-rules/:id/triggers
// :id 即 ruleID — 路径子段复用 alertPathID 校验 UUID
func (h *AlertHandler) ListTriggerMappings(c *gin.Context) {
	ruleID, ok := alertPathID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListMappings(c.Request.Context(), ruleID)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, "参数错误")
			return
		}
		apierr.Internal(c, "列出映射失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": items,
	})
}

// DeleteTriggerMapping DELETE /api/alert-rules/:id/triggers?triggerid=...
// 不存在 → 200 (idempotent, 客户端脚本跑幂等不应被拒)
func (h *AlertHandler) DeleteTriggerMapping(c *gin.Context) {
	ruleID, ok := alertPathID(c)
	if !ok {
		return
	}
	triggerID := c.Query("triggerid")
	if triggerID == "" {
		apierr.BadRequest(c, "triggerid 查询参数必填")
		return
	}
	if err := h.svc.DeleteMapping(c.Request.Context(), ruleID, triggerID); err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, "参数错误")
			return
		}
		apierr.Internal(c, "删除映射失败", err)
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
	id, ok := alertPathID(c)
	if !ok {
		return
	}
	alert, err := h.svc.MarkFalsePositive(c.Request.Context(), id, userID, req.Note, req.IsFalsePositive)
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
