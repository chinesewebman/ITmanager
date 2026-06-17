package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"network-monitor-platform"
	"network-monitor-platform/internal/api"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/eventbus"
	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/notification"
	"network-monitor-platform/internal/service"
	"network-monitor-platform/pkg/logger"
)

func main() {
	// 1. 加载配置
	log.Println("🚀 启动网络监控平台...")
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("❌ 配置加载失败: %v", err)
	}

	// 2. 注入 migration 资源
	database.SetMigrationsFS(network_monitor_platform.MigrationsFS)

	// 3. 初始化日志
	logger.Init(&cfg.Log)

	// 3. 初始化数据库
	db, err := database.Init(&cfg.Database)
	if err != nil {
		logger.Fatal("数据库初始化失败: %v", err)
	}

	// 3.4 P1-审计: 注入 db 到 API key tracker（异步批量写 last_used_at）
	middleware.SetAPIKeyTrackerDB(db)

	// 3.5 C-P5: 初始化 metrics registry（无论 /metrics 开关与否，
	// 内部 counter/gauge 都会积累；只有 /metrics 端点开/关控制是否暴露）
	api.InitMetrics()

	// v1.4: 启动通知 worker (消费 pending notification_logs, 调 Sender 真发)
	notifWorker := notification.NewWorker(db, notification.WorkerConfig{Tick: 5 * time.Second})
	notifWorker.Start(context.Background())
	defer notifWorker.Stop()
	logger.Info("📨 通知 worker 已启动 (5s tick)")

	// v2.0.1: 启动 gRPC server (端口 50051, AlertService 内部 s2s)
	alertSvc := service.NewAlertService(db)
	grpcPort := os.Getenv("GRPC_PORT")
	if grpcPort == "" {
		grpcPort = "50051"
	}
	go startGRPCServer(grpcPort, alertSvc)
	logger.Infof("🔌 gRPC server 已启动", "port", grpcPort)

	// v2.0: 启动事件总线 (in-process pub/sub) + 通知 subscriber
	bus := eventbus.New(db, eventbus.Config{
		BufferSize:  1024,
		MaxRetries:  3,
		WorkerCount: 4,
	})
	defer bus.Close()
	// 通知 worker 注册为 alert.created / alert.resolved subscriber
	// (保留老 worker tick 路径, 双轨并行: 事件触发 + 5s tick 兜底)
	notifWorker.SubscribeToBus(bus)
	logger.Info("📨 事件总线已启动 (4 workers, 通知 worker 已注册订阅)")

	// 4. 设置路由
	router := api.SetupRouter(cfg)

	// 5. 创建服务器
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 6. 启动服务器
	go func() {
		logger.Info("🌐 服务器启动: http://%s:%d", cfg.Server.Host, cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("服务器启动失败: %v", err)
		}
	}()

	// 7. 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("👋 正在关闭服务器...")

	// 8. 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("服务器关闭失败: %v", err)
	}

	// 9. 关闭数据库
	if err := database.Close(); err != nil {
		logger.Error("关闭数据库失败: %v", err)
	}

	logger.Info("✅ 服务器已关闭")
}
