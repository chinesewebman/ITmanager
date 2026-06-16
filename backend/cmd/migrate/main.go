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

	"gorm.io/gorm"
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
	if err := runWithDeps(db, cmd); err != nil {
		log.Fatalf("%s failed: %v", cmd, err)
	}
}

// runWithDeps 接受外部 db 注入（生产路径仍走 main()，test 调 runWithDeps 注入 sqlite）。
func runWithDeps(db *gorm.DB, cmd string) error {
	switch cmd {
	case "up":
		return migrate.Up(db)
	case "down":
		return migrate.Down(db)
	case "status":
		return migrate.Status(db)
	case "reset":
		return migrateReset(db)
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

// migrateReset 循环回滚所有 migration（生产入口会再次确认）
func migrateReset(db *gorm.DB) error {
	fmt.Println("⚠️  reset 将回滚全部 migration（DDL 会被撤，业务数据保留）")
	fmt.Print("确认执行? (yes/no): ")
	var confirm string
	fmt.Scanln(&confirm)
	if confirm != "yes" {
		fmt.Println("aborted")
		return nil
	}
	for {
		var n int
		row := db.Raw("SELECT COUNT(*) FROM schema_migrations").Row()
		if err := row.Scan(&n); err != nil || n == 0 {
			break
		}
		if err := migrate.Down(db); err != nil {
			return fmt.Errorf("reset failed at step: %w", err)
		}
	}
	return nil
}
