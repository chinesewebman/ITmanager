package main

import (
	"log"
	"net"
	"os"

	alertv1 "network-monitor-platform/api/proto/alert/v1"
	"network-monitor-platform/internal/grpcserver"
	"network-monitor-platform/internal/service"

	"google.golang.org/grpc"
)

// startGRPCServer 启动 gRPC server (v2.0.1)
// 端口从 GRPC_PORT env 读取 (默认 50051)
// 注册 AlertService (后续可加 Ticket / Notification 等)
func startGRPCServer(port string, alertSvc service.AlertService) {
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Printf("gRPC 监听失败: %v", err)
		os.Exit(1)
	}
	srv := grpc.NewServer()
	alertv1.RegisterAlertServiceServer(srv, grpcserver.NewAlertServer(alertSvc))
	log.Printf("🔌 gRPC listening on :%s (AlertService)", port)
	if err := srv.Serve(lis); err != nil {
		log.Printf("gRPC serve 失败: %v", err)
	}
}
