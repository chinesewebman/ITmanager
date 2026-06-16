package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
)

// TopologyHandler 网络拓扑 HTTP 处理器（P1-1）
type TopologyHandler struct {
	svc *service.TopologyService
}

func NewTopologyHandler(svc *service.TopologyService) *TopologyHandler {
	return &TopologyHandler{svc: svc}
}

// GetTopology godoc
// @Summary      获取网络拓扑图
// @Description  聚合 assets + asset_networks + alerts 返回节点/边/统计，含自动布局位置 + open_alerts 计数
// @Tags         网络拓扑
// @Produce      json
// @Security     BearerAuth
// @Param        days            query     int     false "查询窗口（天）默认 30，最大 365"
// @Param        only_with_alerts query    bool    false "只显示有告警的节点"
// @Param        asset_types     query     string  false "资产类型过滤（逗号分隔）"
// @Success      200             {object}  models.TopologyGraph
// @Router       /topology [get]
func (h *TopologyHandler) GetTopology(c *gin.Context) {
	filter := service.TopologyFilter{}

	if v := c.Query("days"); v != "" {
		days, err := strconv.Atoi(v)
		if err != nil || days < 0 {
			apierr.BadRequest(c, "days 参数必须是正整数")
			return
		}
		filter.Days = days
	}
	if v := c.Query("only_with_alerts"); v != "" {
		b, _ := strconv.ParseBool(v)
		filter.OnlyWithAlerts = b
	}
	if v := c.Query("asset_types"); v != "" {
		types := strings.Split(v, ",")
		for i := range types {
			types[i] = strings.TrimSpace(types[i])
		}
		filter.AssetTypes = types
	}

	graph, err := h.svc.GetTopology(c.Request.Context(), filter)
	if err != nil {
		apierr.Internal(c, "获取拓扑失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": graph})
}
