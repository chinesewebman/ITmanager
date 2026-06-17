package handlers

import (
	"net/http"
	"strconv"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/cursor"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuditHandler 审计日志 HTTP handler
type AuditHandler struct {
	svc service.AuditService
}

func NewAuditHandler(svc service.AuditService) *AuditHandler {
	return &AuditHandler{svc: svc}
}

// ListAuditLogs 审计日志列表 (v2.0 新增, admin/debug 用)
// GET /api/v1/audit-logs?user_id=&action=&method=&path=&cursor=&limit=
func (h *AuditHandler) ListAuditLogs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))

	filter := service.AuditFilter{
		Action: c.Query("action"),
		Method: c.Query("method"),
		Path:   c.Query("path"),
		Limit:  limit,
	}
	if uidStr := c.Query("user_id"); uidStr != "" {
		if u, err := uuid.Parse(uidStr); err == nil {
			filter.UserID = &u
		}
	}
	// v2.0 cursor 分页
	if cursorStr := c.Query("cursor"); cursorStr != "" {
		ts, id, err := cursor.Decode(cursorStr)
		if err == nil {
			filter.CursorTS = ts
			filter.CursorID = id
		}
	}

	items, err := h.svc.List(c.Request.Context(), filter)
	if err != nil {
		apierr.Internal(c, "获取审计日志失败", err)
		return
	}

	resp := gin.H{"items": items}
	if len(items) == limit && !items[len(items)-1].CreatedAt.IsZero() {
		last := items[len(items)-1]
		resp["next_cursor"] = cursor.Encode(last.CreatedAt, last.ID)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": resp})
}
