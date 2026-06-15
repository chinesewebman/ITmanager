package handlers

import (
	"errors"
	"net/http"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
)

// UserHandler 用户 HTTP handler（只读）
type UserHandler struct {
	svc service.UserService
}

func NewUserHandler(svc service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

func (h *UserHandler) ListUsers(c *gin.Context) {
	users, err := h.svc.List(c.Request.Context())
	if err != nil {
		apierr.Internal(c, "获取用户列表失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": users})
}

func (h *UserHandler) GetUser(c *gin.Context) {
	u, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "用户不存在")
			return
		}
		apierr.Internal(c, "获取用户失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": u})
}
