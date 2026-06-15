package handlers

import (
	"errors"
	"net/http"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
)

// ChannelHandler 通知渠道 HTTP handler
type ChannelHandler struct {
	svc service.ChannelService
}

func NewChannelHandler(svc service.ChannelService) *ChannelHandler {
	return &ChannelHandler{svc: svc}
}

func (h *ChannelHandler) ListChannels(c *gin.Context) {
	chs, err := h.svc.List(c.Request.Context())
	if err != nil {
		apierr.Internal(c, "获取通知渠道列表失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": chs})
}

func (h *ChannelHandler) CreateChannel(c *gin.Context) {
	var ch models.NotificationChannel
	if err := c.ShouldBindJSON(&ch); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	if err := h.svc.Create(c.Request.Context(), &ch); err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, "渠道名称不能为空")
			return
		}
		apierr.Internal(c, "创建通知渠道失败", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": ch})
}

func (h *ChannelHandler) UpdateChannel(c *gin.Context) {
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	ch, err := h.svc.Update(c.Request.Context(), c.Param("id"), updates)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "通知渠道不存在")
			return
		}
		apierr.Internal(c, "更新通知渠道失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": ch})
}

func (h *ChannelHandler) DeleteChannel(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		apierr.Internal(c, "删除通知渠道失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

func (h *ChannelHandler) TestChannel(c *gin.Context) {
	if err := h.svc.Test(c.Request.Context(), c.Param("id")); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "通知渠道不存在")
			return
		}
		apierr.Internal(c, "测试发送失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "测试消息已发送"})
}
