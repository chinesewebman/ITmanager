package handlers

import (
	"errors"
	"net/http"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
)

// RackHandler 机柜/机房 HTTP handler
type RackHandler struct {
	svc service.RackService
}

func NewRackHandler(svc service.RackService) *RackHandler {
	return &RackHandler{svc: svc}
}

func (h *RackHandler) ListRacks(c *gin.Context) {
	racks, err := h.svc.ListRacks(c.Request.Context(), c.Query("site_id"))
	if err != nil {
		apierr.Internal(c, "获取机柜列表失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": racks})
}

func (h *RackHandler) GetRack(c *gin.Context) {
	rack, err := h.svc.GetRack(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "机柜不存在")
			return
		}
		apierr.Internal(c, "获取机柜失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": rack})
}

func (h *RackHandler) GetRackDevices(c *gin.Context) {
	devices, err := h.svc.GetRackDevices(c.Request.Context(), c.Param("id"))
	if err != nil {
		apierr.Internal(c, "获取机柜设备失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": devices})
}

func (h *RackHandler) ListSites(c *gin.Context) {
	sites, err := h.svc.ListSites(c.Request.Context())
	if err != nil {
		apierr.Internal(c, "获取机房列表失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sites})
}

func (h *RackHandler) GetSite(c *gin.Context) {
	detail, err := h.svc.GetSite(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "机房不存在")
			return
		}
		apierr.Internal(c, "获取机房失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"site":        detail.Site,
			"rack_count":  detail.RackCount,
			"asset_count": detail.AssetCount,
		},
	})
}
