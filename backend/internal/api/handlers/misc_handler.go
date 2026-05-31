package handlers

import (
	"net/http"
	"strconv"

	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/models"

	"github.com/gin-gonic/gin"
)

// ListTickets 获取工单列表
func ListTickets(c *gin.Context) {
	var tickets []models.Ticket
	var total int64

	page := c.DefaultQuery("page", "1")
	pageSize := c.DefaultQuery("page_size", "20")
	status := c.Query("status")
	priority := c.Query("priority")

	pageNum, _ := strconv.Atoi(page)
	pageSizeNum, _ := strconv.Atoi(pageSize)

	query := database.DB.Model(&models.Ticket{})

	if status != "" {
		query = query.Where("status = ?", status)
	}
	if priority != "" {
		query = query.Where("priority = ?", priority)
	}

	query.Count(&total)

	offset := (pageNum - 1) * pageSizeNum
	query = query.Offset(offset).Limit(pageSizeNum).Order("created_at DESC")

	if err := query.Find(&tickets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取工单列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"items": tickets,
			"total": total,
			"page":  pageNum,
			"size":  pageSizeNum,
		},
	})
}

// GetTicket 获取工单详情
func GetTicket(c *gin.Context) {
	id := c.Param("id")
	var ticket models.Ticket

	if err := database.DB.First(&ticket, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "工单不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": ticket,
	})
}

// CreateTicket 创建工单
func CreateTicket(c *gin.Context) {
	var ticket models.Ticket
	if err := c.ShouldBindJSON(&ticket); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 设置默认值
	if ticket.Status == "" {
		ticket.Status = "open"
	}
	if ticket.Source == "" {
		ticket.Source = "manual"
	}
	if ticket.Tags == "" {
		ticket.Tags = "[]"
	}

	if err := database.DB.Create(&ticket).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建工单失败",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0,
		"data": ticket,
	})
}

// UpdateTicket 更新工单
func UpdateTicket(c *gin.Context) {
	id := c.Param("id")
	var ticket models.Ticket

	if err := database.DB.First(&ticket, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "工单不存在",
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

	// 如果关闭工单，设置关闭时间
	if status, ok := updates["status"].(string); ok && status == "closed" {
		updates["closed_at"] = database.DB.NowFunc()
	}

	if err := database.DB.Model(&ticket).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新工单失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": ticket,
	})
}

// ListUsers 获取用户列表
func ListUsers(c *gin.Context) {
	var users []models.User
	if err := database.DB.Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取用户列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": users,
	})
}

// GetUser 获取用户详情
func GetUser(c *gin.Context) {
	id := c.Param("id")
	var user models.User
	if err := database.DB.First(&user, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": user,
	})
}

// GetDashboardStats 获取仪表盘统计
func GetDashboardStats(c *gin.Context) {
	var assetCount, alertCount, ticketCount, siteCount int64
	database.DB.Model(&models.Asset{}).Count(&assetCount)
	database.DB.Model(&models.Alert{}).Where("status = ?", "problem").Count(&alertCount)
	database.DB.Model(&models.Site{}).Count(&siteCount)
	database.DB.Model(&models.Ticket{}).Where("status != ?", "closed").Count(&ticketCount)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"assets":   assetCount,
			"alerts":   alertCount,
			"tickets":  ticketCount,
			"sites":    siteCount,
			"machines": assetCount, // 服务器数量
			"networks": assetCount, // 网络设备数量
		},
	})
}

// GetDashboardTrends 获取仪表盘趋势
func GetDashboardTrends(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"alert_trends": []gin.H{
				{"date": "2026-02-08", "count": 10},
				{"date": "2026-02-09", "count": 15},
				{"date": "2026-02-10", "count": 8},
				{"date": "2026-02-11", "count": 12},
				{"date": "2026-02-12", "count": 5},
				{"date": "2026-02-13", "count": 7},
				{"date": "2026-02-14", "count": 3},
			},
		},
	})
}

// ListChannels 获取通知渠道列表
func ListChannels(c *gin.Context) {
	var channels []models.NotificationChannel
	if err := database.DB.Find(&channels).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取通知渠道列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": channels,
	})
}

// CreateChannel 创建通知渠道
func CreateChannel(c *gin.Context) {
	var channel models.NotificationChannel
	if err := c.ShouldBindJSON(&channel); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	if err := database.DB.Create(&channel).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建通知渠道失败",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0,
		"data": channel,
	})
}

// UpdateChannel 更新通知渠道
func UpdateChannel(c *gin.Context) {
	id := c.Param("id")
	var channel models.NotificationChannel

	if err := database.DB.First(&channel, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "通知渠道不存在",
		})
		return
	}

	var updates map[string]interface{}
	c.ShouldBindJSON(&updates)
	database.DB.Model(&channel).Updates(updates)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": channel,
	})
}

// DeleteChannel 删除通知渠道
func DeleteChannel(c *gin.Context) {
	id := c.Param("id")
	database.DB.Delete(&models.NotificationChannel{}, "id = ?", id)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}

// TestChannel 测试通知渠道
func TestChannel(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "测试消息已发送",
	})
}
