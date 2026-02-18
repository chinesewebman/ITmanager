package handlers

import (
	"net/http"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/integration"

	"github.com/gin-gonic/gin"
)

// IntegrationHandler 集成处理器
type IntegrationHandler struct {
	service *integration.IntegrationService
}

// NewIntegrationHandler 创建集成处理器
func NewIntegrationHandler() *IntegrationHandler {
	cfg := config.Get()
	return &IntegrationHandler{
		service: integration.NewIntegrationService(cfg),
	}
}

// SyncRequest 同步请求
type SyncRequest struct {
	Type string `json:"type"` // netbox, zabbix, glpi, all
}

// Sync 同步数据
func (h *IntegrationHandler) Sync(c *gin.Context) {
	var req SyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Type = "all" // 默认同步所有
	}

	var results map[string]int
	var err error

	switch req.Type {
	case "netbox":
		count, e := h.service.SyncFromNetBox()
		results = map[string]int{"netbox": count}
		err = e
	case "zabbix":
		count, e := h.service.SyncFromZabbix()
		results = map[string]int{"zabbix": count}
		err = e
	case "glpi":
		count, e := h.service.SyncFromGLPI()
		results = map[string]int{"glpi": count}
		err = e
	default:
		results, err = h.service.SyncAll()
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "同步失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"synced": results,
		},
		"message": "同步完成",
	})
}

// GetIntegrationStatus 获取集成状态
func (h *IntegrationHandler) GetIntegrationStatus(c *gin.Context) {
	cfg := config.Get()

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"netbox": gin.H{
				"enabled": cfg.Integrations.Netbox.URL != "",
				"url":     cfg.Integrations.Netbox.URL,
			},
			"zabbix": gin.H{
				"enabled": cfg.Integrations.Zabbix.URL != "",
				"url":     cfg.Integrations.Zabbix.URL,
			},
			"glpi": gin.H{
				"enabled": cfg.Integrations.GLPI.URL != "",
				"url":     cfg.Integrations.GLPI.URL,
			},
		},
	})
}
