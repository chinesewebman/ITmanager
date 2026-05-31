package handlers

import (
	"net/http"

	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/models"

	"github.com/gin-gonic/gin"
)

// ListRacks 获取机柜列表
func ListRacks(c *gin.Context) {
	var racks []models.Rack

	siteID := c.Query("site_id")

	query := database.DB.Model(&models.Rack{})
	if siteID != "" {
		query = query.Where("site_id = ?", siteID)
	}

	if err := query.Order("name ASC").Find(&racks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取机柜列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": racks,
	})
}

// GetRack 获取机柜详情
func GetRack(c *gin.Context) {
	id := c.Param("id")
	var rack models.Rack

	if err := database.DB.First(&rack, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "机柜不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": rack,
	})
}

// GetRackDevices 获取机柜设备
func GetRackDevices(c *gin.Context) {
	id := c.Param("id")
	var assets []models.Asset

	if err := database.DB.Where("rack_id = ?", id).Order("rack_position ASC").Find(&assets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取机柜设备失败",
		})
		return
	}

	// 为每个设备获取告警状态
	type RackDevice struct {
		models.Asset
		Status     string `json:"status"` // green, yellow, red
		AlertCount int    `json:"alert_count"`
	}

	devices := make([]RackDevice, len(assets))
	for i, asset := range assets {
		var alertCount int64
		database.DB.Model(&models.Alert{}).Where("asset_id = ? AND status = ?", asset.ID, "problem").Count(&alertCount)

		status := "green"
		if alertCount > 0 {
			status = "red"
		}

		devices[i] = RackDevice{
			Asset:      asset,
			Status:     status,
			AlertCount: int(alertCount),
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": devices,
	})
}

// ListSites 获取机房列表
func ListSites(c *gin.Context) {
	var sites []models.Site

	if err := database.DB.Where("is_active = ?", true).Order("name ASC").Find(&sites).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取机房列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": sites,
	})
}

// GetSite 获取机房详情
func GetSite(c *gin.Context) {
	id := c.Param("id")
	var site models.Site

	if err := database.DB.First(&site, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "机房不存在",
		})
		return
	}

	// 统计机柜数量
	var rackCount int64
	database.DB.Model(&models.Rack{}).Where("site_id = ?", id).Count(&rackCount)

	// 统计设备数量
	var assetCount int64
	database.DB.Model(&models.Asset{}).Where("site_id = ?", id).Count(&assetCount)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"site":        site,
			"rack_count":  rackCount,
			"asset_count": assetCount,
		},
	})
}
