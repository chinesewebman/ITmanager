package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

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

// PingAsset godoc
// @Summary      资产 ICMP ping 探活
// @Description  调用系统 ping 工具探测资产 IP 的可达性
// @Description  count 默认 4，上限 20
// @Tags         diagnostics
// @Produce      json
// @Security     BearerAuth
// @Param        host   query     string  true  "目标 host（IP 或 hostname）"
// @Param        count  query     int     false "ping 次数，默认 4，上限 20"
// @Success      200    {object}  diagnostic.PingResult
// @Failure      400    {object}  apierr.ErrorResponse
// @Failure      401    {object}  apierr.ErrorResponse
// @Router       /diagnostics/ping [get]
func (h *DiagnosticHandler) PingAsset(c *gin.Context) {
	host := c.Query("host")
	if host == "" {
		apierr.BadRequest(c, "host 必传")
		return
	}
	count := 4
	if v := c.Query("count"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			apierr.BadRequest(c, "count 必须是正整数")
			return
		}
		count = n
	}

	// 默认 30s 超时（前端可调整）
	ctx, cancel := withTimeout(c.Request.Context(), 30)
	defer cancel()

	res, err := h.svc.PingAsset(ctx, host, count)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, err.Error())
			return
		}
		apierr.Internal(c, "ping 失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": res,
	})
}

// TracerouteAsset godoc
// @Summary      资产 traceroute 网络路径
// @Description  调用系统 traceroute 跟踪资产 IP 的网络路径
// @Description  maxHops 默认 30，上限 64
// @Tags         diagnostics
// @Produce      json
// @Security     BearerAuth
// @Param        host     query     string  true  "目标 host（IP 或 hostname）"
// @Param        maxHops  query     int     false "最大跳数，默认 30，上限 64"
// @Success      200      {object}  diagnostic.TracerouteResult
// @Failure      400      {object}  apierr.ErrorResponse
// @Failure      401      {object}  apierr.ErrorResponse
// @Router       /diagnostics/traceroute [get]
func (h *DiagnosticHandler) TracerouteAsset(c *gin.Context) {
	host := c.Query("host")
	if host == "" {
		apierr.BadRequest(c, "host 必传")
		return
	}
	maxHops := 30
	if v := c.Query("maxHops"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			apierr.BadRequest(c, "maxHops 必须是正整数")
			return
		}
		maxHops = n
	}

	ctx, cancel := withTimeout(c.Request.Context(), 60)
	defer cancel()

	res, err := h.svc.TracerouteAsset(ctx, host, maxHops)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, err.Error())
			return
		}
		apierr.Internal(c, "traceroute 失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": res,
	})
}

// withTimeout 给 ctx 加超时（秒）。若 ctx 已有 Deadline 则不覆盖。
func withTimeout(parent context.Context, seconds int) (context.Context, context.CancelFunc) {
	if _, has := parent.Deadline(); has {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, time.Duration(seconds)*time.Second)
}
