package handlers

import (
	"context"
	"net/http"
	"time"

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

// syncTimeout C-P7: 同步 API 总超时 5min（集成调用链 + 批量写 DB）。
const syncTimeout = 5 * time.Minute

// Sync 同步数据
func (h *IntegrationHandler) Sync(c *gin.Context) {
	var req SyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Type = "all" // 默认同步所有
	}

	// C-P7: ctx 透传到下游 httpx 与 gorm
	ctx, cancel := context.WithTimeout(c.Request.Context(), syncTimeout)
	defer cancel()

	var results map[string]int
	var err error

	switch req.Type {
	case "netbox":
		count, e := h.svc.SyncFromNetBox(ctx)
		results = map[string]int{"netbox": count}
		err = e
	case "zabbix":
		count, e := h.svc.SyncFromZabbix(ctx)
		results = map[string]int{"zabbix": count}
		err = e
	case "glpi":
		count, e := h.svc.SyncFromGLPI(ctx)
		results = map[string]int{"glpi": count}
		err = e
	default:
		results, err = h.svc.SyncAll(ctx)
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
