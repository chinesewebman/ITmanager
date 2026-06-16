// Package api 提供 HTTP 路由与全局 metrics 句柄。
package api

import (
	"context"
	"net/http"
	"time"

	"network-monitor-platform/internal/api/handlers"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/integration"
	"network-monitor-platform/internal/metrics"
	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// platformMetrics C-P5: 全局 metrics registry。
// 暴露在 /metrics 端点；HTTP 中间件自动记录请求级 metric。
var platformMetrics *metrics.Registry

// InitMetrics 初始化 metrics registry 与默认 metric。
// 必须在 SetupRouter 之前调用（main.go 已调用）。
func InitMetrics() *metrics.Registry {
	if platformMetrics != nil {
		return platformMetrics
	}
	platformMetrics = metrics.New()

	// HTTP 请求级 metric（由 middleware.HTTPMetrics 写入）
	platformMetrics.NewCounterVec(
		"http_requests_total",
		"HTTP 请求总数",
		[]string{"method", "path", "status"},
	)
	platformMetrics.NewHistogramVec(
		"http_request_duration_seconds",
		"HTTP 请求耗时（秒）",
		[]string{"method", "path"},
		[]float64{.005, .01, .05, .1, .25, .5, 1, 2.5, 5, 10},
	)
	// DB pool gauge（由 /metrics 收集时实时拉取）
	platformMetrics.NewGaugeVec(
		"db_pool_open_connections",
		"DB pool 打开连接数",
		nil,
	)
	platformMetrics.NewGaugeVec(
		"db_pool_in_use",
		"DB pool in-use 连接数",
		nil,
	)
	platformMetrics.NewGaugeVec(
		"db_pool_idle",
		"DB pool idle 连接数",
		nil,
	)
	platformMetrics.NewGaugeVec(
		"db_pool_wait_count",
		"DB pool 等待连接总数（累计）",
		nil,
	)

	return platformMetrics
}

// UpdateDBPoolMetrics 拉取 sql.DB 状态写入 gauge。
// 在 /metrics 拉取前调用（与 Handler 集成）。
func UpdateDBPoolMetrics(gormDB *gorm.DB) {
	if platformMetrics == nil || gormDB == nil {
		return
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return
	}
	stats := sqlDB.Stats()
	platformMetrics.SetGauge("db_pool_open_connections", float64(stats.OpenConnections))
	platformMetrics.SetGauge("db_pool_in_use", float64(stats.InUse))
	platformMetrics.SetGauge("db_pool_idle", float64(stats.Idle))
	platformMetrics.SetGauge("db_pool_wait_count", float64(stats.WaitCount))
}

// integrationMetricsAdapter 把 metrics.Registry 适配成 httpx.MetricsRecorder 接口。
// 集成层 httpx 调用时把事件桥接到全局 platformMetrics。
func integrationMetricsAdapter() *integrationHTTPMetrics {
	return &integrationHTTPMetrics{}
}

type integrationHTTPMetrics struct{}

func (i *integrationHTTPMetrics) IncRequest(system, status string) {
	if platformMetrics == nil {
		return
	}
	platformMetrics.IncCounter("integration_requests_total", system, status)
}
func (i *integrationHTTPMetrics) ObserveDuration(system string, seconds float64) {
	if platformMetrics == nil {
		return
	}
	platformMetrics.ObserveHistogram("integration_request_duration_seconds", seconds, system)
}

// SetupRouter 设置路由
func SetupRouter(cfg *config.Config) *gin.Engine {
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	r.Use(middleware.CORS(cfg))
	// C-P5: HTTP metrics 中间件（仅在 metrics 启用时挂载，避免无意义开销）
	if platformMetrics != nil {
		r.Use(middleware.HTTPMetrics(platformMetrics))
	}
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// C-P1 + C-P5: 健康/就绪/metrics 探针（无需鉴权）
	db := database.GetDB()
	r.GET("/healthz", livenessHandler)
	r.GET("/readyz", readinessHandler(db))
	// C-P5: Prometheus metrics 端点
	if cfg.Server.MetricsEnabled {
		// 拉取时实时更新 DB pool gauge
		r.GET("/metrics", func(c *gin.Context) {
			UpdateDBPoolMetrics(database.GetDB())
			platformMetrics.Handler().ServeHTTP(c.Writer, c.Request)
		})
	}

	// C-P5: 集成 metric 记录器
	integMetrics := integrationMetricsAdapter()

	assetSvc := service.NewAssetService(db)
	alertSvc := service.NewAlertService(db)
	rackSvc := service.NewRackService(db)
	ticketSvc := service.NewTicketService(db)
	userSvc := service.NewUserService(db)
	dashboardSvc := service.NewDashboardService(db)
	channelSvc := service.NewChannelService(db)
	diagnosticSvc := service.NewDiagnosticService(db)
	integrationSvc := integration.NewIntegrationService(cfg, integMetrics)

	assetH := handlers.NewAssetHandler(assetSvc)
	alertH := handlers.NewAlertHandler(alertSvc)
	rackH := handlers.NewRackHandler(rackSvc)
	ticketH := handlers.NewTicketHandler(ticketSvc)
	userH := handlers.NewUserHandler(userSvc)
	dashboardH := handlers.NewDashboardHandler(dashboardSvc)
	channelH := handlers.NewChannelHandler(channelSvc)
	diagnosticH := handlers.NewDiagnosticHandler(diagnosticSvc)
	integrationH := handlers.NewIntegrationHandler(integrationSvc, cfg)

	api := r.Group("/api")
	{
		// 兼容旧探针：/api/health 内部转发到 liveness（部分 manifest 仍引用旧路径）
		api.GET("/health", func(c *gin.Context) { livenessHandler(c) })

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
				// C-P6: 批量端点（静态段，挂在 :id 之前）
				alerts.POST("/bulk-ack", alertH.BulkAcknowledge)
				alerts.POST("/bulk-resolve", alertH.BulkResolve)
				alerts.POST("/bulk-delete", alertH.BulkDelete)
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

			// 资产诊断（故障时间线）
			diagnostics := protected.Group("/diagnostics")
			{
				diagnostics.GET("/assets/:id/timeline", diagnosticH.GetAssetTimeline)
			}
		}
	}

	r.Static("/static", "./static")
	r.NoRoute(func(c *gin.Context) {
		c.File("./frontend/dist/index.html")
	})

	// Swagger UI（用 backend/openapi.yaml 作为 spec）
	RegisterSwagger(r)

	return r
}

func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"version":  "1.0.0",
		"database": "connected",
	})
}

// livenessHandler C-P1: 进程存活探针（K8s livenessProbe）
// 只确认进程能响应 HTTP，不依赖 DB/外部服务
func livenessHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "alive"})
}

// readinessHandler C-P1: 就绪探针（K8s readinessProbe）
// 真 ping DB（500ms 超时）+ 报告 DB pool 状态，失败 → 503 摘流
func readinessHandler(gormDB *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
		defer cancel()

		checks := gin.H{"status": "ready"}
		httpStatus := http.StatusOK

		sqlDB, err := gormDB.DB()
		if err != nil {
			checks["status"] = "not_ready"
			checks["database"] = "no_sql_handle: " + err.Error()
			httpStatus = http.StatusServiceUnavailable
		} else if err := sqlDB.PingContext(ctx); err != nil {
			checks["status"] = "not_ready"
			checks["database"] = "ping_failed: " + err.Error()
			httpStatus = http.StatusServiceUnavailable
		} else {
			// 报告 DB pool 状态，便于容量规划
			stats := sqlDB.Stats()
			checks["database"] = gin.H{
				"open":     stats.OpenConnections,
				"in_use":   stats.InUse,
				"idle":     stats.Idle,
				"max_open": stats.MaxOpenConnections,
			}
		}

		c.JSON(httpStatus, checks)
	}
}
