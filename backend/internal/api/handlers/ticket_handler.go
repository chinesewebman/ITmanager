package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/cursor"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// TicketHandler 工单 HTTP handler
type TicketHandler struct {
	svc service.TicketService
}

func NewTicketHandler(svc service.TicketService) *TicketHandler {
	return &TicketHandler{svc: svc}
}

func (h *TicketHandler) ListTickets(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	filter := service.TicketFilter{
		Status:   c.Query("status"),
		Priority: c.Query("priority"),
		Page:     page,
		PageSize: pageSize,
	}
	// v2.0 cursor 分页: 优先级高于 page/size
	if cursorStr := c.Query("cursor"); cursorStr != "" {
		ts, id, err := cursor.Decode(cursorStr)
		if err == nil {
			filter.CursorTS = ts
			filter.CursorID = id
			filter.Page = 0     // cursor 模式不跑 offset
			filter.PageSize = 0 // 强制走 default 20
		}
	}

	items, total, err := h.svc.List(c.Request.Context(), filter)
	if err != nil {
		apierr.Internal(c, "获取工单列表失败", err)
		return
	}
	resp := gin.H{
		"items": items,
		"total": total,
		"page":  page,
		"size":  pageSize,
	}
	// v2.0: cursor 模式返 next_cursor
	if filter.CursorID != uuid.Nil && len(items) > 0 {
		last := items[len(items)-1]
		resp["next_cursor"] = cursor.Encode(last.CreatedAt, last.ID)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": resp})
}

func (h *TicketHandler) GetTicket(c *gin.Context) {
	t, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "工单不存在")
			return
		}
		apierr.Internal(c, "获取工单失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": t})
}

// ListTicketHistory GET /tickets/:id/history —— 某张工单的经手历史。
//
// 返回**平铺行**（一行一个字段变更），前端按 batch_id 分组：后端不做展示层聚合，
// 契约保持直白形状（docs/FIX-PLAN-TICKET-HISTORY.md §2.6）。准入与 GetTicket 同级 ——
// 「跟工单本身的可见性」，不另收口到 canAudit（燕如 2026-09-11 拍板③）。
func (h *TicketHandler) ListTicketHistory(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	items, total, err := h.svc.ListHistory(c.Request.Context(), c.Param("id"), page, pageSize)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "工单不存在")
			return
		}
		apierr.Internal(c, "获取工单经手历史失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"items": items,
		"total": total,
		"page":  page,
		"size":  pageSize,
	}})
}

func (h *TicketHandler) CreateTicket(c *gin.Context) {
	var ticket models.Ticket
	if err := c.ShouldBindJSON(&ticket); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	if err := h.svc.Create(c.Request.Context(), &ticket, actorFromContext(c)); err != nil {
		if errors.Is(err, service.ErrAlreadyExists) {
			apierr.Conflict(c, "工单已存在")
			return
		}
		if errors.Is(err, service.ErrInvalidInput) {
			// 空标题 / 枚举列取值超出契约词表（service 层校验）→ 400。
			// 用 err.Error() 而不是写死文案：原来写死「工单标题不能为空」，
			// 加了枚举校验后同一个分支会返回两种原因，写死的那句就成了假话。
			apierr.BadRequest(c, err.Error())
			return
		}
		apierr.Internal(c, "创建工单失败", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": ticket})
}

// CreateTicketFromAlert POST /alerts/:id/ticket —— 从告警一键建单并回写 alerts.ticket_id
// （TODO D-3，契约见 docs/FIX-PLAN-ALERT-TICKET.md §3）。
// 该告警已有关联工单时幂等返回既有那张（200 + created=false），新建返回 201 + created=true。
func (h *TicketHandler) CreateTicketFromAlert(c *gin.Context) {
	// 经手人解析收在 actorFromContext 一处：这里只用 Name（旧签名收字符串），
	// 与 UpdateTicket 走同一个 helper，避免两处各自 parse 而分叉。
	userID := actorFromContext(c).Name
	ticket, created, err := h.svc.CreateFromAlert(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "告警不存在或关联的工单已失效")
			return
		}
		apierr.Internal(c, "从告警创建工单失败", err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, gin.H{
		"code": 0,
		"data": gin.H{
			"ticket":  ticket,
			"created": created,
		},
	})
}

func (h *TicketHandler) UpdateTicket(c *gin.Context) {
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	t, err := h.svc.Update(c.Request.Context(), c.Param("id"), updates, actorFromContext(c))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "工单不存在")
			return
		}
		if errors.Is(err, service.ErrInvalidInput) {
			// 请求里带了 id/ticket_number/created_at/updated_at 等系统维护的列 →
			// 400 而不是 500（这是调用方的请求错，不是服务端故障）
			apierr.BadRequest(c, err.Error())
			return
		}
		apierr.Internal(c, "更新工单失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": t})
}
