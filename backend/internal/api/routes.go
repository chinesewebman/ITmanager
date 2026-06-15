package api

import (
	"net/http"

	"network-monitor-platform/internal/api/handlers"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
)

// SetupRouter 设置路由
func SetupRouter(cfg *config.Config) *gin.Engine {
	// 根据环境设置 gin 模式
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	// 中间件
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// 健康检查
	r.GET("/health", healthCheck)

	// service 构造（共享 db 句柄）
	db := database.GetDB()
	assetSvc := service.NewAssetService(db)
	alertSvc := service.NewAlertService(db)
	assetH := handlers.NewAssetHandler(assetSvc)
	alertH := handlers.NewAlertHandler(alertSvc)

	// API 路由
	api := r.Group("/api")
	{
		// 认证（无需登录）
		auth := api.Group("/auth")
		{
			auth.POST("/login", handlers.Login)
			auth.POST("/logout", handlers.Logout)
			auth.GET("/me", middleware.AuthMiddleware(), handlers.GetCurrentUser)
			auth.PUT("/password", middleware.AuthMiddleware(), handlers.ChangePassword)

			// API Key 管理（需要登录）
			auth.POST("/api-keys", middleware.AuthMiddleware(), handlers.CreateAPIKey)
			auth.GET("/api-keys", middleware.AuthMiddleware(), handlers.ListAPIKeys)
			auth.DELETE("/api-keys/:id", middleware.AuthMiddleware(), handlers.DeleteAPIKey)
			auth.PUT("/api-keys/:id/revoke", middleware.AuthMiddleware(), handlers.RevokeAPIKey)
		}

		// 需要认证的 API
		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware())
		{
			// 集成
			integrationHandler := handlers.NewIntegrationHandler()
			protected.POST("/integrations/sync", integrationHandler.Sync)
			protected.GET("/integrations/status", integrationHandler.GetIntegrationStatus)

			// 资产
			assets := protected.Group("/assets")
			{
				assets.GET("", assetH.ListAssets)
				assets.GET("/:id", assetH.GetAsset)
				assets.POST("", assetH.CreateAsset)
				assets.PUT("/:id", assetH.UpdateAsset)
				assets.DELETE("/:id", assetH.DeleteAsset)
				assets.GET("/export", assetH.ExportAssets)
			}

			// 机柜
			racks := protected.Group("/racks")
			{
				racks.GET("", handlers.ListRacks)
				racks.GET("/:id", handlers.GetRack)
				racks.GET("/:id/devices", handlers.GetRackDevices)
			}

			// 机房
			sites := protected.Group("/sites")
			{
				sites.GET("", handlers.ListSites)
				sites.GET("/:id", handlers.GetSite)
			}

			// 告警
			alerts := protected.Group("/alerts")
			{
				alerts.GET("", alertH.ListAlerts)
				alerts.GET("/:id", alertH.GetAlert)
				alerts.PUT("/:id/ack", alertH.AcknowledgeAlert)
				alerts.PUT("/:id/resolve", alertH.ResolveAlert)
				alerts.GET("/stats", alertH.GetAlertStats)
			}

			// 告警规则
			rules := protected.Group("/alert-rules")
			{
				rules.GET("", alertH.ListAlertRules)
				rules.POST("", alertH.CreateAlertRule)
				rules.PUT("/:id", alertH.UpdateAlertRule)
				rules.DELETE("/:id", alertH.DeleteAlertRule)
			}

			// 工单
			tickets := protected.Group("/tickets")
			{
				tickets.GET("", handlers.ListTickets)
				tickets.GET("/:id", handlers.GetTicket)
				tickets.POST("", handlers.CreateTicket)
				tickets.PUT("/:id", handlers.UpdateTicket)
			}

			// 用户
			users := protected.Group("/users")
			{
				users.GET("", handlers.ListUsers)
				users.GET("/:id", handlers.GetUser)
			}

			// 仪表盘
			dashboard := protected.Group("/dashboard")
			{
				dashboard.GET("/stats", handlers.GetDashboardStats)
				dashboard.GET("/trends", handlers.GetDashboardTrends)
			}

			// 通知渠道
			channels := protected.Group("/notification-channels")
			{
				channels.GET("", handlers.ListChannels)
				channels.POST("", handlers.CreateChannel)
				channels.PUT("/:id", handlers.UpdateChannel)
				channels.DELETE("/:id", handlers.DeleteChannel)
				channels.PUT("/:id/test", handlers.TestChannel)
			}
		}
	}

	// 静态文件
	r.Static("/static", "./static")

	// 前端路由 (SPA)
	r.NoRoute(func(c *gin.Context) {
		c.File("./frontend/dist/index.html")
	})

	return r
}

// healthCheck 健康检查
func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"version":  "1.0.0",
		"database": "connected",
	})
}
