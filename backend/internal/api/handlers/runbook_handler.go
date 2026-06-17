// runbook HTTP handler
package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"
)

// RunbookHandler Runbook HTTP 接口
type RunbookHandler struct {
	svc *service.RunbookService
}

func NewRunbookHandler(svc *service.RunbookService) *RunbookHandler {
	return &RunbookHandler{svc: svc}
}

// Create POST /api/runbooks
func (h *RunbookHandler) Create(c *gin.Context) {
	var rb models.Runbook
	if err := c.ShouldBindJSON(&rb); err != nil {
		apierr.BadRequest(c, "请求体非法: "+err.Error())
		return
	}
	rb.ID = uuid.New()
	if err := h.svc.Create(c.Request.Context(), &rb); err != nil {
		if errors.Is(err, service.ErrAlreadyExists) {
			apierr.Conflict(c, "Runbook 已存在")
			return
		}
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, err.Error())
			return
		}
		apierr.Internal(c, "服务器内部错误", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": rb})
}

// Update PUT /api/runbooks/:id
func (h *RunbookHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.BadRequest(c, "无效的 ID")
		return
	}
	var rb models.Runbook
	if err := c.ShouldBindJSON(&rb); err != nil {
		apierr.BadRequest(c, "请求体非法: "+err.Error())
		return
	}
	rb.ID = id
	if err := h.svc.Update(c.Request.Context(), &rb); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "Runbook 不存在")
			return
		}
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, err.Error())
			return
		}
		apierr.Internal(c, "服务器内部错误", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": rb})
}

// Delete DELETE /api/runbooks/:id
func (h *RunbookHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.BadRequest(c, "无效的 ID")
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id.String()); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "Runbook 不存在")
			return
		}
		apierr.Internal(c, "服务器内部错误", err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Get GET /api/runbooks/:id
func (h *RunbookHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.BadRequest(c, "无效的 ID")
		return
	}
	rb, err := h.svc.Get(c.Request.Context(), id.String())
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "Runbook 不存在")
			return
		}
		apierr.Internal(c, "服务器内部错误", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": rb})
}

// List GET /api/runbooks?asset_type=&severity=&enabled=&keyword=&limit=&offset=
func (h *RunbookHandler) List(c *gin.Context) {
	opt := service.RunbookListOptions{
		AssetType: c.Query("asset_type"),
		Keyword:   c.Query("keyword"),
	}
	if sevStr := c.Query("severity"); sevStr != "" {
		sev, err := strconv.Atoi(sevStr)
		if err != nil {
			apierr.BadRequest(c, "severity 必须是整数")
			return
		}
		opt.Severity = sev
	}
	if enStr := c.Query("enabled"); enStr != "" {
		v, err := strconv.ParseBool(enStr)
		if err != nil {
			apierr.BadRequest(c, "enabled 必须是 bool")
			return
		}
		opt.Enabled = &v
	}
	if lStr := c.Query("limit"); lStr != "" {
		l, err := strconv.Atoi(lStr)
		if err != nil {
			apierr.BadRequest(c, "limit 必须是整数")
			return
		}
		opt.Limit = l
	}
	if oStr := c.Query("offset"); oStr != "" {
		o, err := strconv.Atoi(oStr)
		if err != nil {
			apierr.BadRequest(c, "offset 必须是整数")
			return
		}
		opt.Offset = o
	}

	items, total, err := h.svc.List(c.Request.Context(), opt)
	if err != nil {
		apierr.Internal(c, "服务器内部错误", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items, "total": total}})
}

// Recommend GET /api/runbooks/recommend?asset_type=switch&severity=4
func (h *RunbookHandler) Recommend(c *gin.Context) {
	opt := service.RunbookListOptions{
		AssetType: c.Query("asset_type"),
	}
	if sevStr := c.Query("severity"); sevStr != "" {
		sev, err := strconv.Atoi(sevStr)
		if err != nil {
			apierr.BadRequest(c, "severity 必须是整数")
			return
		}
		opt.Severity = sev
	}
	items, err := h.svc.ListForAssetTypeAndSeverity(c.Request.Context(), opt.AssetType, opt.Severity)
	if err != nil {
		apierr.Internal(c, "服务器内部错误", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": items})
}

// 显式 import 验证
var _ = gorm.ErrRecordNotFound
