package api

import (
	"net/http"

	"network-monitor-platform/internal/api/handlers"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/integration"
	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
)

// SetupRouter 设置路由
func SetupRouter(cfg *config.Config) *gin.Engine {
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	r.Use(middleware.CORS(cfg))
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	r.GET("/health", healthCheck)

	// 构造 service / handler
	db := database.GetDB()
	assetSvc := service.NewAssetService(db)
	alertSvc := service.NewAlertService(db)
	rackSvc := service.NewRackService(db)
	ticketSvc := service.NewTicketService(db)
	userSvc := service.NewUserService(db)
	dashboardSvc := service.NewDashboardService(db)
	channelSvc := service.NewChannelService(db)
	integrationSvc := integration.NewIntegrationService(cfg)

	assetH := handlers.NewAssetHandler(assetSvc)
	alertH := handlers.NewAlertHandler(alertSvc)
	rackH := handlers.NewRackHandler(rackSvc)
	ticketH := handlers.NewTicketHandler(ticketSvc)
	userH := handlers.NewUserHandler(userSvc)
	dashboardH := handlers.NewDashboardHandler(dashboardSvc)
	channelH := handlers.NewChannelHandler(channelSvc)
	integrationH := handlers.NewIntegrationHandler(integrationSvc, cfg)

	api := r.Group("/api")
	{
		auth := api.Group("/auth")
		{
			auth.POST("/login", handlers.Login)
			auth.POST("/logout", handlers.Logout)
			auth.GET("/me", middleware.AuthMiddleware(), handlers.GetCurrentUser)
			auth.PUT("/password", middleware.AuthMiddleware(), handlers.ChangePassword)

			auth.POST("/api-keys", middleware.AuthMiddleware(), handlers.CreateAPIKey)
			auth.GET("/api-keys", middleware.AuthMiddleware(), handlers.ListAPIKeys)
			auth.DELETE("/api-keys/:id", middleware.AuthMiddleware(), handlers.DeleteAPIKey)
			auth.PUT("/api-keys/:id/revoke", middleware.AuthMiddleware(), handlers.RevokeAPIKey)
		}

		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware())
		{
			protected.POST("/integrations/sync", integrationH.Sync)
			protected.GET("/integrations/status", integrationH.GetIntegrationStatus)

			assets := protected.Group("/assets")
			{
				assets.GET("", assetH.ListAssets)
				assets.GET("/export", assetH.ExportAssets) // 静态段必须早于 /:id，否则 /export 被当成 :id
				assets.GET("/:id", assetH.GetAsset)
				assets.POST("", assetH.CreateAsset)
				assets.PUT("/:id", assetH.UpdateAsset)
				assets.DELETE("/:id", assetH.DeleteAsset)
			}

			racks := protected.Group("/racks")
			{
				racks.GET("", rackH.ListRacks)
				racks.GET("/:id", rackH.GetRack)
				racks.GET("/:id/devices", rackH.GetRackDevices)
			}

			sites := protected.Group("/sites")
			{
				sites.GET("", rackH.ListSites)
				sites.GET("/:id", rackH.GetSite)
			}

			alerts := protected.Group("/alerts")
			{
				alerts.GET("", alertH.ListAlerts)
				alerts.GET("/stats", alertH.GetAlertStats) // 静态段必须早于 /:id，否则 /stats 被当成 :id
				alerts.GET("/:id", alertH.GetAlert)
				alerts.PUT("/:id/ack", alertH.AcknowledgeAlert)
				alerts.PUT("/:id/resolve", alertH.ResolveAlert)
			}

			rules := protected.Group("/alert-rules")
			{
				rules.GET("", alertH.ListAlertRules)
				rules.POST("", alertH.CreateAlertRule)
				rules.PUT("/:id", alertH.UpdateAlertRule)
				rules.DELETE("/:id", alertH.DeleteAlertRule)
			}

			tickets := protected.Group("/tickets")
			{
				tickets.GET("", ticketH.ListTickets)
				tickets.GET("/:id", ticketH.GetTicket)
				tickets.POST("", ticketH.CreateTicket)
				tickets.PUT("/:id", ticketH.UpdateTicket)
			}

			users := protected.Group("/users")
			{
				users.GET("", userH.ListUsers)
				users.GET("/:id", userH.GetUser)
			}

			dashboard := protected.Group("/dashboard")
			{
				dashboard.GET("/stats", dashboardH.GetDashboardStats)
				dashboard.GET("/trends", dashboardH.GetDashboardTrends)
			}

			channels := protected.Group("/notification-channels")
			{
				channels.GET("", channelH.ListChannels)
				channels.POST("", channelH.CreateChannel)
				channels.PUT("/:id", channelH.UpdateChannel)
				channels.DELETE("/:id", channelH.DeleteChannel)
				channels.PUT("/:id/test", channelH.TestChannel)
			}
		}
	}

	r.Static("/static", "./static")
	r.NoRoute(func(c *gin.Context) {
		c.File("./frontend/dist/index.html")
	})

	return r
}

func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"version":  "1.0.0",
		"database": "connected",
	})
}
