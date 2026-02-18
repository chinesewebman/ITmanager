package handlers

import (
	"net/http"
	"time"

	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ListAlerts 获取告警列表
func ListAlerts(c *gin.Context) {
	var alerts []models.Alert

	status := c.Query("status")
	severity := c.Query("severity")
	hostID := c.Query("host_id")

	query := database.DB.Model(&models.Alert{})

	if status != "" {
		query = query.Where("status = ?", status)
	}
	if severity != "" {
		query = query.Where("severity >= ?", severity)
	}
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}

	if err := query.Order("created_at DESC").Limit(100).Find(&alerts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取告警列表失败",
		})
		return
	}

	// 统计
	var total, problem, acknowledged, resolved int64
	database.DB.Model(&models.Alert{}).Count(&total)
	database.DB.Model(&models.Alert{}).Where("status = ?", "problem").Count(&problem)
	database.DB.Model(&models.Alert{}).Where("status = ?", "acknowledged").Count(&acknowledged)
	database.DB.Model(&models.Alert{}).Where("status = ?", "resolved").Count(&resolved)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"items": alerts,
			"stats": gin.H{
				"total":        total,
				"problem":      problem,
				"acknowledged":  acknowledged,
				"resolved":     resolved,
			},
		},
	})
}

// GetAlert 获取告警详情
func GetAlert(c *gin.Context) {
	id := c.Param("id")
	var alert models.Alert

	if err := database.DB.First(&alert, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "告警不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": alert,
	})
}

// AcknowledgeAlert 确认告警
func AcknowledgeAlert(c *gin.Context) {
	id := c.Param("id")
	var alert models.Alert

	if err := database.DB.First(&alert, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "告警不存在",
		})
		return
	}

	now := time.Now()
	updates := map[string]interface{}{
		"status":     "acknowledged",
		"ack_time":   now,
		"ack_user":   "admin", // TODO: 从上下文获取
	}

	if err := database.DB.Model(&alert).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "确认告警失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"message": "告警已确认",
	})
}

// ResolveAlert 解决告警
func ResolveAlert(c *gin.Context) {
	id := c.Param("id")
	var alert models.Alert

	if err := database.DB.First(&alert, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "告警不存在",
		})
		return
	}

	now := time.Now()
	duration := int(now.Sub(alert.ProblemStart).Seconds())

	updates := map[string]interface{}{
		"status":        "resolved",
		"resolve_time":   now,
		"resolve_user":  "admin",
		"problem_end":   now,
		"duration":      duration,
	}

	if err := database.DB.Model(&alert).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "解决告警失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"message": "告警已解决",
	})
}

// GetAlertStats 获取告警统计
func GetAlertStats(c *gin.Context) {
	// 按严重级别统计
	type SeverityStat struct {
		Severity    int   `json:"severity"`
		SeverityName string `json:"severity_name"`
		Count       int64 `json:"count"`
	}

	var severityStats []SeverityStat
	database.DB.Model(&models.Alert{}).
		Select("severity, severity_name, COUNT(*) as count").
		Where("status = ?", "problem").
		Group("severity, severity_name").
		Scan(&severityStats)

	// 按小时统计 (最近24小时)
	var hourlyStats []struct {
		Hour  time.Time `json:"hour"`
		Count int64     `json:"count"`
	}
	database.DB.Model(&models.Alert{}).
		Select("date_trunc('hour', created_at) as hour, COUNT(*) as count").
		Where("created_at > ?", time.Now().AddDate(0, 0, -1)).
		Group("hour").
		Order("hour").
		Scan(&hourlyStats)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"by_severity": severityStats,
			"by_hour":     hourlyStats,
		},
	})
}

// ListAlertRules 获取告警规则列表
func ListAlertRules(c *gin.Context) {
	var rules []models.AlertRule
	if err := database.DB.Where("is_enabled = ?", true).Order("priority ASC").Find(&rules).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取告警规则列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": rules,
	})
}

// CreateAlertRule 创建告警规则
func CreateAlertRule(c *gin.Context) {
	var rule models.AlertRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	rule.ID = uuid.New()
	if err := database.DB.Create(&rule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建告警规则失败",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0,
		"data": rule,
	})
}

// UpdateAlertRule 更新告警规则
func UpdateAlertRule(c *gin.Context) {
	id := c.Param("id")
	var rule models.AlertRule

	if err := database.DB.First(&rule, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "告警规则不存在",
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

	if err := database.DB.Model(&rule).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新告警规则失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": rule,
	})
}

// DeleteAlertRule 删除告警规则
func DeleteAlertRule(c *gin.Context) {
	id := c.Param("id")
	if err := database.DB.Delete(&models.AlertRule{}, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除告警规则失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"message": "删除成功",
	})
}
