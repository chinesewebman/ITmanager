package handlers

import (
	"net/http"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/integration"

	"github.com/gin-gonic/gin"
)

// IntegrationHandler 集成 HTTP handler（不再用 config.Get()，由 routes 注入）
type IntegrationHandler struct {
	svc    *integration.IntegrationService
	config *config.Config
}

// NewIntegrationHandler 构造函数
func NewIntegrationHandler(svc *integration.IntegrationService, cfg *config.Config) *IntegrationHandler {
	return &IntegrationHandler{svc: svc, config: cfg}
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
		count, e := h.svc.SyncFromNetBox()
		results = map[string]int{"netbox": count}
		err = e
	case "zabbix":
		count, e := h.svc.SyncFromZabbix()
		results = map[string]int{"zabbix": count}
		err = e
	case "glpi":
		count, e := h.svc.SyncFromGLPI()
		results = map[string]int{"glpi": count}
		err = e
	default:
		results, err = h.svc.SyncAll()
	}

	if err != nil {
		apierr.Internal(c, "同步失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"data":    gin.H{"synced": results},
		"message": "同步完成",
	})
}

// GetIntegrationStatus 获取集成状态
func (h *IntegrationHandler) GetIntegrationStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"netbox": gin.H{
				"enabled": h.config.Integrations.Netbox.URL != "",
				"url":     h.config.Integrations.Netbox.URL,
			},
			"zabbix": gin.H{
				"enabled": h.config.Integrations.Zabbix.URL != "",
				"url":     h.config.Integrations.Zabbix.URL,
			},
			"glpi": gin.H{
				"enabled": h.config.Integrations.GLPI.URL != "",
				"url":     h.config.Integrations.GLPI.URL,
			},
		},
	})
}
