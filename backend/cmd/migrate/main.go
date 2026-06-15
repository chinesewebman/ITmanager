// migrate 是独立 migration 管理命令：
//
//	go run ./cmd/migrate up        # 应用所有未执行的 migration
//	go run ./cmd/migrate down      # 回滚最后一个
//	go run ./cmd/migrate status    # 列出所有 migration 和状态
//	go run ./cmd/migrate reset     # 警告：回滚全部（不常用）
package main

import (
	"fmt"
	"log"
	"os"

	"network-monitor-platform"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/migrate"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: migrate <up|down|status|reset>")
		os.Exit(1)
	}

	database.SetMigrationsFS(network_monitor_platform.MigrationsFS)

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	db, err := database.Init(&cfg.Database)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	defer database.Close()

	cmd := os.Args[1]
	switch cmd {
	case "up":
		if err := migrate.Up(db); err != nil {
			log.Fatalf("up failed: %v", err)
		}
	case "down":
		if err := migrate.Down(db); err != nil {
			log.Fatalf("down failed: %v", err)
		}
	case "status":
		if err := migrate.Status(db); err != nil {
			log.Fatalf("status failed: %v", err)
		}
	case "reset":
		fmt.Println("⚠️  reset 将回滚全部 migration（DDL 会被撤，业务数据保留）")
		fmt.Print("确认执行? (yes/no): ")
		var confirm string
		fmt.Scanln(&confirm)
		if confirm != "yes" {
			fmt.Println("aborted")
			return
		}
		for {
			var n int
			row := db.Raw("SELECT COUNT(*) FROM schema_migrations").Row()
			if err := row.Scan(&n); err != nil || n == 0 {
				break
			}
			if err := migrate.Down(db); err != nil {
				log.Fatalf("reset failed at step: %v", err)
			}
		}
	default:
		fmt.Printf("unknown command: %s\n", cmd)
		os.Exit(1)
	}
}
