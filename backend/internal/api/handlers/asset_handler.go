package handlers

import (
	"net/http"
	"strconv"

	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/models"

	"github.com/gin-gonic/gin"
)

// ListAssets 获取资产列表
func ListAssets(c *gin.Context) {
	var assets []models.Asset
	var total int64

	page := c.DefaultQuery("page", "1")
	pageSize := c.DefaultQuery("page_size", "20")
	keyword := c.Query("keyword")
	status := c.Query("status")
	assetType := c.Query("type")

	pageNum, _ := strconv.Atoi(page)
	pageSizeNum, _ := strconv.Atoi(pageSize)

	// 构建查询
	query := database.DB.Model(&models.Asset{})

	// 筛选条件
	if keyword != "" {
		query = query.Where("name ILIKE ? OR asset_tag ILIKE ? OR sn ILIKE ?",
			"%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if assetType != "" {
		query = query.Where("asset_type = ?", assetType)
	}

	// 统计总数
	query.Count(&total)

	// 分页查询
	offset := (pageNum - 1) * pageSizeNum
	query = query.Offset(offset).Limit(pageSizeNum).Order("created_at DESC")

	if err := query.Find(&assets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取资产列表失败",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"items": assets,
			"total": total,
			"page":  pageNum,
			"size":  pageSizeNum,
		},
	})
}

// GetAsset 获取资产详情
func GetAsset(c *gin.Context) {
	id := c.Param("id")
	var asset models.Asset

	if err := database.DB.First(&asset, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "资产不存在",
		})
		return
	}

	// 获取网络接口
	var networks []models.AssetNetwork
	database.DB.Where("asset_id = ?", asset.ID).Find(&networks)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"asset":    asset,
			"networks": networks,
		},
	})
}

// CreateAsset 创建资产
func CreateAsset(c *gin.Context) {
	var asset models.Asset
	if err := c.ShouldBindJSON(&asset); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
			"error":   err.Error(),
		})
		return
	}

	if err := database.DB.Create(&asset).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建资产失败",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0,
		"data": asset,
	})
}

// UpdateAsset 更新资产
func UpdateAsset(c *gin.Context) {
	id := c.Param("id")
	var asset models.Asset

	if err := database.DB.First(&asset, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "资产不存在",
		})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	if err := database.DB.Model(&asset).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新资产失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": asset,
	})
}

// DeleteAsset 删除资产
func DeleteAsset(c *gin.Context) {
	id := c.Param("id")

	if err := database.DB.Delete(&models.Asset{}, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除资产失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"message": "删除成功",
	})
}

// ExportAssets 导出资产
func ExportAssets(c *gin.Context) {
	format := c.DefaultQuery("format", "csv")

	var assets []models.Asset
	database.DB.Find(&assets)

	if format == "csv" {
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", "attachment; filename=assets.csv")
		c.String(http.StatusOK, "ID,Name,Type,Status,IP\n")
		for _, a := range assets {
			c.String(http.StatusOK, "%s,%s,%s,%s,\n", a.ID, a.Name, a.AssetType, a.Status)
		}
	} else {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": assets,
		})
	}
}
