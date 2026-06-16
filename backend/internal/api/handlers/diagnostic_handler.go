package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// DiagnosticHandler 资产诊断 HTTP 处理器
type DiagnosticHandler struct {
	svc *service.DiagnosticService
}

// NewDiagnosticHandler 构造 DiagnosticHandler
func NewDiagnosticHandler(svc *service.DiagnosticService) *DiagnosticHandler {
	return &DiagnosticHandler{svc: svc}
}

// GetAssetTimeline godoc
// @Summary      获取资产时间线（用于故障诊断）
// @Description  聚合 alerts / tickets / assets / asset_networks 四张表，按时间倒序返回事件流
// @Description  含 MTTR (平均恢复时间)、open_alerts、open_tickets 等摘要
// @Tags         diagnostics
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string  true  "资产 UUID"
// @Param        days  query     int     false "查询窗口（天）默认 30，最大 365"
// @Param        limit query     int     false "事件数上限，默认 200，最大 1000"
// @Success      200   {object}  models.DiagnosticTimeline
// @Failure      400   {object}  apierr.ErrorResponse
// @Failure      401   {object}  apierr.ErrorResponse
// @Failure      404   {object}  apierr.ErrorResponse
// @Router       /diagnostics/assets/{id}/timeline [get]
func (h *DiagnosticHandler) GetAssetTimeline(c *gin.Context) {
	idStr := c.Param("id")
	assetID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.BadRequest(c, "资产 ID 格式错误")
		return
	}

	filter := service.DiagnosticFilter{}
	if v := c.Query("days"); v != "" {
		days, err := strconv.Atoi(v)
		if err != nil || days < 0 {
			apierr.BadRequest(c, "days 参数必须是正整数")
			return
		}
		filter.Days = days
	}
	if v := c.Query("limit"); v != "" {
		limit, err := strconv.Atoi(v)
		if err != nil || limit < 0 {
			apierr.BadRequest(c, "limit 参数必须是正整数")
			return
		}
		filter.Limit = limit
	}

	timeline, err := h.svc.GetTimeline(c.Request.Context(), assetID, filter)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "资产不存在")
			return
		}
		apierr.Internal(c, "获取资产时间线失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": timeline,
	})
}
