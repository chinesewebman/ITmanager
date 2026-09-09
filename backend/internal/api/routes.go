// Package api 提供 HTTP 路由与全局 metrics 句柄。
package api

import (
	"context"
	"net/http"
	"time"

	"network-monitor-platform/internal/api/handlers"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/httpx"
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

// NewIntegrationMetricsAdapter v2.3: 暴露给 main.go 用于构造 IntegrationService。
// 内部仍使用 platformMetrics 全局（由 InitMetrics() 注入）。
func NewIntegrationMetricsAdapter() httpx.MetricsRecorder {
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

// SetupRouter v2.3: IntegrationService 由调用方（main.go）传入并复用，
// MetricSyncWorker 也用同一个 svc.zabbix 客户端 — 保证 UI Reload Zabbix 配置后
// worker 立即生效，无需重启。
func SetupRouter(cfg *config.Config, integrationSvc *integration.IntegrationService) *gin.Engine {
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

	// C-P5: 集成 metric 记录器（v2.3：构造在 main.go，此处不再重复构造）

	assetSvc := service.NewAssetService(db)
	alertSvc := service.NewAlertService(db)
	rackSvc := service.NewRackService(db)
	ticketSvc := service.NewTicketService(db)
	userSvc := service.NewUserService(db)
	auditSvc := service.NewAuditService(db) // v2.0: 审计日志查询接口
	dashboardSvc := service.NewDashboardService(db)
	channelSvc := service.NewChannelService(db)
	diagnosticSvc := service.NewDiagnosticService(db)
	suppressionSvc := service.NewAlertSuppressionService(db)
	topologySvc := service.NewTopologyService(db)
	oncallSvc := service.NewOncallService(db)
	runbookSvc := service.NewRunbookService(db)
	metricSvc := service.NewMetricSnapshotService(db)
	// integrationSvc v2.3: 改为参数传入，避免重复构造 ZabbixClient
	postmortemSvc := service.NewPostmortemService(db, diagnosticSvc)

	assetH := handlers.NewAssetHandler(assetSvc)
	alertH := handlers.NewAlertHandler(alertSvc)
	rackH := handlers.NewRackHandler(rackSvc)
	ticketH := handlers.NewTicketHandler(ticketSvc)
	userH := handlers.NewUserHandler(userSvc)
	dashboardH := handlers.NewDashboardHandler(dashboardSvc)
	channelH := handlers.NewChannelHandler(channelSvc)
	diagnosticH := handlers.NewDiagnosticHandler(diagnosticSvc)
	suppressionH := handlers.NewAlertSuppressionHandler(suppressionSvc)
	topologyH := handlers.NewTopologyHandler(topologySvc)
	oncallH := handlers.NewOncallHandler(oncallSvc)
	auditH := handlers.NewAuditHandler(auditSvc) // v2.0: 审计日志查询
	runbookH := handlers.NewRunbookHandler(runbookSvc)
	metricH := handlers.NewMetricSnapshotHandler(metricSvc)
	integrationH := handlers.NewIntegrationHandler(integrationSvc, cfg)
	postmortemH := handlers.NewPostmortemHandler(postmortemSvc)

	// 能力门禁（docs/FIX-PLAN-AUTHZ.md §3.2）：矩阵实现在 middleware.Can，
	// 这里只做别名，避免每个路由重复写 middleware.RequireCapability(...)。
	// 没有 canRead —— read 是地板（未被下面四种能力覆盖的端点默认放行），不挂中间件。
	canWrite := middleware.RequireCapability(middleware.CapWrite)
	canManage := middleware.RequireCapability(middleware.CapManage)
	canAudit := middleware.RequireCapability(middleware.CapAudit)
	canIdentity := middleware.RequireCapability(middleware.CapIdentity)

	api := r.Group("/api")
	{
		// 兼容旧探针：/api/health 内部转发到 liveness（部分 manifest 仍引用旧路径）
		api.GET("/health", func(c *gin.Context) { livenessHandler(c) })

		auth := api.Group("/auth")
		{
			// v1.4 (ADR-001): login 严限 — 防爆破 5 req/min per IP
			// AuditLog：登录失败/锁定此前完全无留痕（既分不清爆破还是忘密码，也看不出谁在
			// 制造锁定）。handler 会把尝试的用户名放进 context，故审计行带用户名。
			// 见 docs/FIX-PLAN-AUTHZ-CLOSURE.md §2 D-E。
			//
			// 顺序必须是 RateLimit 在前：AuditLog 在 c.Next() 之后**无条件**写库，
			// 若排在限流之前，被 429 拒掉的请求照样 INSERT —— 未认证请求即可无限写库
			// （安全审计 F1 实测：10 次请求 → 5×401 + 5×429，但审计 10 行）。
			auth.POST("/login",
				middleware.RateLimit(middleware.DefaultRateLimitConfig(5)),
				middleware.AuditLog(middleware.AuditConfig{DB: database.GetDB()}),
				handlers.Login)
			auth.POST("/logout", handlers.Logout)
			auth.GET("/me", middleware.AuthMiddleware(), handlers.GetCurrentUser)
			// 注：改密 / 跳过改密均已移入下方 protected 组 —— 挂在 auth 组时没有
			// AuditLog 中间件，凭据变更与强改密绕过的尝试都不留痕
			// （docs/FIX-PLAN-AUTHZ-LEFTOVER.md §2 D-A / F-2）。

			// 注：API Key 路由已移入下方 protected 组 —— 原先挂在 auth 组时没有
			// AuditLog 中间件，铸造/吊销长期凭据不留痕（docs/FIX-PLAN-AUTHZ.md §4.3）。
		}

		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware())
		protected.Use(middleware.RateLimit(middleware.DefaultRateLimitConfig(100)))      // v1.4 默认 100 req/min per IP+path
		protected.Use(middleware.AuditLog(middleware.AuditConfig{DB: database.GetDB()})) // v1.4 审计日志
		{
			// C7: 跳过首次登录强改密。无待办时幂等（handler 不写任何状态），
			// 放 protected 组以复用 AuditLog —— 强改密绕过的尝试必须留痕。
			protected.POST("/auth/skip-password-change", handlers.SkipPasswordChange)

			// 改密：变更账号凭据 → 拒绝 API Key 身份（泄露的 write Key 若同时掌握旧密码
			// 即可改掉账号密码，吊销 Key 撤销不了；且 API Key 路径不查 LockedUntil，见 G-2）
			// + 限流 3/min + AuditLog 留痕（原挂 auth 组无审计）。
			protected.PUT("/auth/password", middleware.RejectAPIKeyAuth(), middleware.RateLimit(middleware.DefaultRateLimitConfig(3)), handlers.ChangePassword)

			// 凭据管理（docs/FIX-PLAN-AUTHZ.md §3.3）：签发/查看/吊销 API Key 限 admin。
			// 放在 protected 组而非 auth 组，是为了拿到 AuditLog —— 铸造长期凭据必须留痕。
			// 再叠一层 RejectAPIKeyAuth：长期凭据不得自我复制 —— write scope 的 Key 挂在
			// admin 账号上即可铸造新 Key，吊销旧 Key 后新 Key 仍存活（FIX-PLAN-AUTHZ-LEFTOVER.md S-2）。
			// 整组挂载，未来新增 /auth/api-keys/* 路由自动继承该限制。
			apiKeys := protected.Group("/auth/api-keys")
			apiKeys.Use(middleware.RejectAPIKeyAuth())
			{
				apiKeys.POST("", canIdentity, handlers.CreateAPIKey)
				apiKeys.GET("", canIdentity, handlers.ListAPIKeys)
				apiKeys.DELETE("/:id", canIdentity, handlers.DeleteAPIKey)
				apiKeys.PUT("/:id/revoke", canIdentity, handlers.RevokeAPIKey)
			}

			// 集成配置含 token，写/测试一律限 manage（只读状态查询不限）
			protected.POST("/integrations/sync", canManage, integrationH.Sync)
			protected.GET("/integrations/status", integrationH.GetIntegrationStatus)
			// v2.2: 三个集成的运行时配置管理（UI Settings 保存按钮 + 测试连通）
			//
			// PUT 额外拒绝 API Key：更新时空 token/password 会**保留旧值**，只改 URL ——
			// 于是 write Key 可以「把出站地址改指攻击者 + 触发 /test」，让平台把已存凭据
			// 发给攻击者（安全审查实测复现，docs/FIX-PLAN-AUTHZ-CLOSURE.md §1 S-4）。
			// /test 与 /sync 不拦：堵住 PUT 后它们只能打管理员配置过的地址，是自动化该用的能力。
			protected.POST("/integrations/zabbix/test", canManage, integrationH.TestZabbix)
			protected.PUT("/integrations/zabbix", middleware.RejectAPIKeyAuth(), canManage, integrationH.UpdateZabbix)
			protected.POST("/integrations/netbox/test", canManage, integrationH.TestNetBox)
			protected.PUT("/integrations/netbox", middleware.RejectAPIKeyAuth(), canManage, integrationH.UpdateNetBox)
			protected.POST("/integrations/glpi/test", canManage, integrationH.TestGLPI)
			protected.PUT("/integrations/glpi", middleware.RejectAPIKeyAuth(), canManage, integrationH.UpdateGLPI)

			// v2.0: 审计日志查询端点
			// 审计日志含用户名/IP/操作轨迹 → audit 能力（admin/ops_admin/auditor）。
			// 修复前挂 RequireRole("admin") 导致 auditor 角色读不到审计日志（角色形同虚设）。
			protected.GET("/audit-logs", canAudit, auditH.ListAuditLogs)

			assets := protected.Group("/assets")
			{
				assets.GET("", assetH.ListAssets)
				assets.GET("/export", assetH.ExportAssets) // 静态段必须早于 /:id，否则 /export 被当成 :id
				assets.GET("/:id", assetH.GetAsset)
				assets.POST("", canWrite, assetH.CreateAsset)
				assets.PUT("/:id", canWrite, assetH.UpdateAsset)
				assets.DELETE("/:id", canManage, assetH.DeleteAsset) // 硬删不可逆
				// B4: 软退役 + 恢复（可逆 → write）
				assets.POST("/:id/retire", canWrite, assetH.RetireAsset)   // 静态段 /retire 在 /:id 之后, gin 路径匹配 OK
				assets.POST("/:id/restore", canWrite, assetH.RestoreAsset) // 同上
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

			alerts.GET("", alertH.ListAlerts)
			alerts.GET("/stats", alertH.GetAlertStats)                         // 静态段必须早于 /:id，否则 /stats 被当成 :id
			alerts.GET("/false-positives/export", alertH.ExportFalsePositives) // 同理：静态段早于 /:id
			alerts.POST("/bulk-ack", canWrite, alertH.BulkAcknowledge)
			alerts.POST("/bulk-resolve", canWrite, alertH.BulkResolve)
			alerts.POST("/bulk-delete", canManage, alertH.BulkDelete)
			alerts.GET("/:id", alertH.GetAlert)
			alerts.PUT("/:id/ack", canWrite, alertH.AcknowledgeAlert)
			alerts.PUT("/:id/resolve", canWrite, alertH.ResolveAlert)
			alerts.POST("/:id/mark-fp", canWrite, alertH.MarkFalsePositive) // 小改进 #2：标记/反标记误报

			rules := protected.Group("/alert-rules")
			{
				rules.GET("", alertH.ListAlertRules)
				rules.POST("", canWrite, alertH.CreateAlertRule)
				rules.PUT("/:id", canWrite, alertH.UpdateAlertRule)
				rules.DELETE("/:id", canManage, alertH.DeleteAlertRule)
			}

			tickets := protected.Group("/tickets")
			{
				tickets.GET("", ticketH.ListTickets)
				tickets.GET("/:id", ticketH.GetTicket)
				tickets.POST("", canWrite, ticketH.CreateTicket)
				tickets.PUT("/:id", canWrite, ticketH.UpdateTicket)
			}

			// 用户列表暴露账号/邮箱/角色 → identity（仅 admin）
			users := protected.Group("/users")
			users.Use(canIdentity)
			{
				users.GET("", userH.ListUsers)
				users.GET("/:id", userH.GetUser)
			}
			// 仪表盘
			dashboard := protected.Group("/dashboard")
			{
				dashboard.GET("/stats", dashboardH.GetDashboardStats)
				dashboard.GET("/trends", dashboardH.GetDashboardTrends)
				dashboard.GET("/kpis", dashboardH.GetKPIs)
			}

			// 通知渠道配置含 webhook token / SMTP 凭据（响应体不脱敏），且 /:id/test
			// 由服务端主动外连 —— 整组限 manage（docs/FIX-PLAN-AUTHZ.md §3.2 凭据例外）。
			// 再叠一层 RejectAPIKeyAuth：长期凭据不得读取/改写其它凭据（F-3）。
			// API Key 的 role 取自关联用户（auth.go:196），admin 名下的 write Key
			// 原本能直接读到 config 明文（docs/FIX-PLAN-AUTHZ-CLOSURE.md §1 S-3）。
			channels := protected.Group("/notification-channels")
			channels.Use(canManage)
			channels.Use(middleware.RejectAPIKeyAuth())
			{
				channels.GET("", channelH.ListChannels)
				channels.POST("", channelH.CreateChannel)
				channels.PUT("/:id", channelH.UpdateChannel)
				channels.DELETE("/:id", channelH.DeleteChannel)
				channels.PUT("/:id/test", channelH.TestChannel)
			}

			// 资产诊断（故障时间线 + ping/traceroute 探活）
			diagnostics := protected.Group("/diagnostics")
			{
				diagnostics.GET("/assets/:id/timeline", diagnosticH.GetAssetTimeline)
				// ping/traceroute 是静态段，无 :id 冲突；放 group 末尾便于阅读。
				// 服务端主动外连（内网可达性探测）→ 要 write，只读身份不应触发。
				diagnostics.GET("/ping", canWrite, diagnosticH.PingAsset)
				diagnostics.GET("/traceroute", canWrite, diagnosticH.TracerouteAsset)
			}

			// 资产复盘 PDF 报告
			postmortem := protected.Group("/postmortem")
			{
				postmortem.GET("/assets/:id/report", postmortemH.DownloadReport)
			}

			// 告警抑制规则（P0-2）
			// /preview 静态段必须在 /:id 之前
			suppressions := protected.Group("/alert-suppressions")
			{
				suppressions.GET("", suppressionH.ListAlertSuppressions)
				suppressions.POST("/preview", suppressionH.PreviewSuppression) // 纯计算无副作用，不挂能力
				suppressions.POST("", canWrite, suppressionH.CreateAlertSuppression)
				suppressions.GET("/:id", suppressionH.GetAlertSuppression)
				suppressions.PUT("/:id", canWrite, suppressionH.UpdateAlertSuppression)
				suppressions.DELETE("/:id", canManage, suppressionH.DeleteAlertSuppression)
			}

			// 网络拓扑（P1-1）
			topology := protected.Group("/topology")
			{
				topology.GET("", topologyH.GetTopology)
			}

			// 值班 + 升级（P1-2）
			oncall := protected.Group("/oncall")
			{
				oncall.GET("/current", oncallH.GetCurrentOncall)
				oncall.GET("/schedules", oncallH.ListSchedules)
				oncall.POST("/schedules", canWrite, oncallH.CreateSchedule)
				oncall.DELETE("/schedules/:id", canManage, oncallH.DeleteSchedule)
				oncall.GET("/schedules/:id/shifts", oncallH.ListShifts)
				oncall.POST("/schedules/:id/shifts", canWrite, oncallH.CreateShift)
				oncall.DELETE("/shifts/:shift_id", canManage, oncallH.DeleteShift)
				oncall.GET("/policies", oncallH.ListPolicies)
				oncall.POST("/policies", canWrite, oncallH.CreatePolicy)
				oncall.GET("/policies/:id", oncallH.GetPolicy)
				oncall.DELETE("/policies/:id", canManage, oncallH.DeletePolicy)
			}

			// 故障 Runbook（P2-1）
			runbooks := protected.Group("/runbooks")
			{
				runbooks.POST("", canWrite, runbookH.Create)
				runbooks.GET("", runbookH.List)
				runbooks.GET("/recommend", runbookH.Recommend)
				runbooks.GET("/:id", runbookH.Get)
				runbooks.PUT("/:id", canWrite, runbookH.Update)
				runbooks.DELETE("/:id", canManage, runbookH.Delete)
			}

			// 指标快照（P2-2 Zabbix 兜底）
			metrics := protected.Group("/metric-snapshots")
			{
				metrics.POST("", canWrite, metricH.BulkInsert)
				metrics.GET("", metricH.Query)
				metrics.GET("/latest", metricH.Latest)
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
