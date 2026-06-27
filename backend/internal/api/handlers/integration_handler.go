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

	// 🐛 BUG#7: 之前 type=garbage 静默走 default("all")，现在严格校验
	switch req.Type {
	case "netbox", "zabbix", "glpi", "all", "":
		// 合法
	default:
		apierr.BadRequest(c, "type 必须是 netbox/zabbix/glpi/all 之一")
		return
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
				"user":    h.config.Integrations.Zabbix.User,
				// 不回显 password（明文）
				"has_password": h.config.Integrations.Zabbix.Password != "",
			},
			"glpi": gin.H{
				"enabled": h.config.Integrations.GLPI.URL != "",
				"url":     h.config.Integrations.GLPI.URL,
			},
		},
	})
}

// TestZabbix v2.2: 仅 Login 一次，验证 URL/账号密码通不通，不入 DB 不入指标。
func (h *IntegrationHandler) TestZabbix(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	if err := h.svc.TestZabbixConnection(ctx); err != nil {
		apierr.BadRequest(c, "Zabbix 连通失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "Zabbix 连通 OK"})
}

// UpdateZabbixRequest v2.2: UI 保存按钮提交的三件套。
// password 允许为空（"保持原值"语义）；URL/user 必填。
type UpdateZabbixRequest struct {
	URL      string `json:"url" binding:"required"`
	User     string `json:"user" binding:"required"`
	Password string `json:"password"` // 空 = 不改；非空 = 覆盖
}

// UpdateZabbix v2.2: UI 保存按钮 → 内存 cfg 更新 + 客户端 Reload。
// 不落盘到 yaml（重启需重新走 env/yaml），但运行时立即生效。
func (h *IntegrationHandler) UpdateZabbix(c *gin.Context) {
	var req UpdateZabbixRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.BadRequest(c, "url/user 必填: "+err.Error())
		return
	}
	// 空 password = 保留旧值（避免 UI 清空密码时把后端改成空）
	if req.Password == "" {
		req.Password = h.config.Integrations.Zabbix.Password
	}
	// 写入内存 cfg（同步生效）
	newCfg := &config.ZabbixConfig{
		URL:      req.URL,
		User:     req.User,
		Password: req.Password,
	}
	h.config.Integrations.Zabbix = *newCfg
	// 客户端热重载：清 auth 让下次 GetTriggers 重新 Login
	h.svc.ReloadZabbix(newCfg)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Zabbix 配置已生效（运行时），重启后回到 yaml/env 值",
		"data": gin.H{
			"url":  req.URL,
			"user": req.User,
		},
	})
}
