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

	items, total, err := h.svc.List(c.Request.Context(), service.TicketFilter{
		Status:   c.Query("status"),
		Priority: c.Query("priority"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		apierr.Internal(c, "获取工单列表失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"items": items,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
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

func (h *TicketHandler) CreateTicket(c *gin.Context) {
	var ticket models.Ticket
	if err := c.ShouldBindJSON(&ticket); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	if err := h.svc.Create(c.Request.Context(), &ticket); err != nil {
		if errors.Is(err, service.ErrAlreadyExists) {
			apierr.Conflict(c, "工单已存在")
			return
		}
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, "工单标题不能为空")
			return
		}
		apierr.Internal(c, "创建工单失败", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": ticket})
}

func (h *TicketHandler) UpdateTicket(c *gin.Context) {
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	t, err := h.svc.Update(c.Request.Context(), c.Param("id"), updates)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "工单不存在")
			return
		}
		apierr.Internal(c, "更新工单失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": t})
}
