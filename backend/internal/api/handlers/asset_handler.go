package handlers

import (
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
)

// safeCSV 防 Excel 公式注入（C-F7）：对 = + - @ 	 \r 开头字段加前导单引号
func safeCSV(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '	', '\r':
		return "'" + s
	}
	return s
}

// AssetHandler 资产相关 HTTP handler
type AssetHandler struct {
	svc service.AssetService
}

// NewAssetHandler 构造函数（依赖注入，便于测试时 mock service）
func NewAssetHandler(svc service.AssetService) *AssetHandler {
	return &AssetHandler{svc: svc}
}

// ListAssets 资产列表（分页+筛选）
func (h *AssetHandler) ListAssets(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	items, total, err := h.svc.List(c.Request.Context(), service.AssetFilter{
		Keyword:   c.Query("keyword"),
		Status:    c.Query("status"),
		AssetType: c.Query("type"),
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		apierr.Internal(c, "获取资产列表失败", err)
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

// GetAsset 资产详情（含网络接口）
func (h *AssetHandler) GetAsset(c *gin.Context) {
	asset, networks, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "资产不存在")
			return
		}
		apierr.Internal(c, "获取资产失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"asset":    asset,
			"networks": networks,
		},
	})
}

// CreateAsset 创建资产
func (h *AssetHandler) CreateAsset(c *gin.Context) {
	var asset models.Asset
	if err := c.ShouldBindJSON(&asset); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	if err := h.svc.Create(c.Request.Context(), &asset); err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, "资产名称不能为空")
			return
		}
		apierr.Internal(c, "创建资产失败", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"code": 0,
		"data": asset,
	})
}

// UpdateAsset 部分更新
func (h *AssetHandler) UpdateAsset(c *gin.Context) {
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	asset, err := h.svc.Update(c.Request.Context(), c.Param("id"), updates)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "资产不存在")
			return
		}
		apierr.Internal(c, "更新资产失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": asset,
	})
}

// DeleteAsset 删除
func (h *AssetHandler) DeleteAsset(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		apierr.Internal(c, "删除资产失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}

// ExportAssets 导出 CSV/JSON
func (h *AssetHandler) ExportAssets(c *gin.Context) {
	format := c.DefaultQuery("format", "csv")

	// 导出走全量查询（不分页）
	items, _, err := h.svc.List(c.Request.Context(), service.AssetFilter{Page: 1, PageSize: 500})
	if err != nil {
		apierr.Internal(c, "导出资产失败", err)
		return
	}

	if format == "csv" {
		// C-F7: 用 encoding/csv 正确转义（含逗号/换行/双引号）
		// 并对 = + - @ 	 \r 开头字段加前导单引号防止 Excel 公式注入（DDE）
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", `attachment; filename=assets.csv`)
		c.Header("X-Content-Type-Options", "nosniff")

		w := csv.NewWriter(c.Writer)
		defer w.Flush()
		_ = w.Write([]string{"ID", "Name", "Type", "Status"})
		for _, a := range items {
			row := []string{
				a.ID.String(),
				safeCSV(a.Name),
				safeCSV(a.AssetType),
				safeCSV(a.Status),
			}
			_ = w.Write(row)
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": items,
	})
}
