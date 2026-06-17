package handlers

import (
	"errors"
	"net/http"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AlertSuppressionHandler 告警抑制规则 HTTP 处理器（P0-2）
type AlertSuppressionHandler struct {
	svc *service.AlertSuppressionService
}

func NewAlertSuppressionHandler(svc *service.AlertSuppressionService) *AlertSuppressionHandler {
	return &AlertSuppressionHandler{svc: svc}
}

// ListAlertSuppressions godoc
// @Summary      列出告警抑制规则
// @Tags         告警抑制
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}  models.AlertSuppression
// @Router       /alert-suppressions [get]
func (h *AlertSuppressionHandler) ListAlertSuppressions(c *gin.Context) {
	rules, err := h.svc.List(c.Request.Context())
	if err != nil {
		apierr.Internal(c, "列出抑制规则失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": rules})
}

// CreateAlertSuppression godoc
// @Summary      创建告警抑制规则
// @Tags         告警抑制
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        rule  body      models.AlertSuppression  true  "规则"
// @Success      201   {object}  models.AlertSuppression
// @Router       /alert-suppressions [post]
func (h *AlertSuppressionHandler) CreateAlertSuppression(c *gin.Context) {
	var rule models.AlertSuppression
	if err := c.ShouldBindJSON(&rule); err != nil {
		apierr.BadRequest(c, "请求体格式错误: "+err.Error())
		return
	}
	// 客户端可能不传 id，service 层会通过 DEFAULT 触发，但 gorm Save 不会触发
	// 这里显式让 DB 生成
	rule.ID = uuid.Nil
	if err := h.svc.Create(c.Request.Context(), &rule); err != nil {
		if errors.Is(err, service.ErrAlreadyExists) {
			apierr.Conflict(c, "抑制规则已存在")
			return
		}
		apierr.BadRequest(c, "创建失败: "+err.Error())
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": rule})
}

// GetAlertSuppression godoc
// @Summary      获取单条告警抑制规则
// @Tags         告警抑制
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "规则 UUID"
// @Success      200  {object}  models.AlertSuppression
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /alert-suppressions/{id} [get]
func (h *AlertSuppressionHandler) GetAlertSuppression(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.BadRequest(c, "ID 格式错误")
		return
	}
	rule, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "抑制规则不存在")
			return
		}
		apierr.Internal(c, "获取失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": rule})
}

// UpdateAlertSuppression godoc
// @Summary      更新告警抑制规则
// @Tags         告警抑制
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path      string                  true   "规则 UUID"
// @Param        updates body      object                  true   "更新字段"
// @Success      200     {object}  models.AlertSuppression
// @Router       /alert-suppressions/{id} [put]
func (h *AlertSuppressionHandler) UpdateAlertSuppression(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.BadRequest(c, "ID 格式错误")
		return
	}
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		apierr.BadRequest(c, "请求体格式错误: "+err.Error())
		return
	}
	rule, err := h.svc.Update(c.Request.Context(), id, updates)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "抑制规则不存在")
			return
		}
		apierr.BadRequest(c, "更新失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": rule})
}

// DeleteAlertSuppression godoc
// @Summary      删除告警抑制规则
// @Tags         告警抑制
// @Security     BearerAuth
// @Param        id   path      string  true  "规则 UUID"
// @Success      204
// @Router       /alert-suppressions/{id} [delete]
func (h *AlertSuppressionHandler) DeleteAlertSuppression(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.BadRequest(c, "ID 格式错误")
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "抑制规则不存在")
			return
		}
		apierr.Internal(c, "删除失败", err)
		return
	}
	c.Status(http.StatusNoContent)
}

// PreviewSuppression godoc
// @Summary      模拟评估一条告警是否被抑制
// @Tags         告警抑制
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        alert  body      object  true  "{severity, host_id, host_name}"
// @Success      200    {object}  models.SuppressionMatchResult
// @Router       /alert-suppressions/preview [post]
func (h *AlertSuppressionHandler) PreviewSuppression(c *gin.Context) {
	var req struct {
		Severity int    `json:"severity" binding:"required,min=1,max=5"`
		HostID   string `json:"host_id" binding:"required"`
		HostName string `json:"host_name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.BadRequest(c, "请求参数错误: "+err.Error())
		return
	}
	hostID, err := uuid.Parse(req.HostID)
	if err != nil {
		apierr.BadRequest(c, "host_id 必须是 UUID")
		return
	}
	res, err := h.svc.Evaluate(c.Request.Context(), req.Severity, hostID, req.HostName)
	if err != nil {
		apierr.Internal(c, "评估失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": res})
}
