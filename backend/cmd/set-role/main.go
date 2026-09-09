// Package set_role 修改既有用户的角色（直连 DB，不经 HTTP）。
//
// 用法：
//
//	SET_ROLE_USERNAME=alice SET_ROLE_ROLE=ops_admin go run ./cmd/set-role
//
// 设计动机（docs/FIX-PLAN-AUTHZ.md §1.4）：权限矩阵里的 ops_admin / ops_user /
// auditor 在应用内**没有任何分配路径** —— `/users` 只有 GET，`admin-bootstrap`
// 恒写 admin，`seed` 只写 admin/ops_user/readonly。没有这条命令，矩阵有一半角色
// 在生产不可达。做成独立命令而非 HTTP 接口：不新增攻击面，也不需要为写用户
// 补一套校验/审计/防自锁设计（那属独立任务）。
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"network-monitor-platform"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/models"

	"gorm.io/gorm"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("❌ 设置角色失败: %v", err)
	}
}

func run() error {
	username, role, err := parseSetRoleEnv()
	if err != nil {
		return err
	}

	cfg, err := config.Load("config.yaml")
	if err != nil {
		return err
	}
	// 必须注入 MigrationsFS：否则走 gorm AutoMigrate 兜底，在真实 postgres 上
	// 与迁移 DDL 漂移（实测 "insufficient arguments"）。
	database.SetMigrationsFS(network_monitor_platform.MigrationsFS)
	database.SetGormLogLevel(cfg.Log.Level)
	db, err := database.Init(&cfg.Database)
	if err != nil {
		return err
	}
	defer func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	}()

	return runWithDeps(db, username, role)
}

// runWithDeps 在已注入 db 的前提下执行主体。暴露给测试使用。
func runWithDeps(db *gorm.DB, username, role string) error {
	if !middleware.IsKnownRole(role) {
		return fmt.Errorf("未知角色 %q，可用值：admin / ops_admin / ops_user / auditor / readonly / user", role)
	}
	canonical := middleware.CanonicalRole(role)

	var user models.User
	if err := db.First(&user, "username = ?", username).Error; err != nil {
		return fmt.Errorf("用户 %q 不存在: %w", username, err)
	}

	if user.Role == canonical {
		log.Printf("ℹ️ 用户 %s 角色已是 %s，无需修改", username, canonical)
		return nil
	}

	// 写 users.role 与同步 user_roles 必须原子：中途失败会留下
	// 「users.role 已改、user_roles 仍指向旧角色」的不一致状态。
	err := db.Transaction(func(tx *gorm.DB) error {
		// 防自锁：不允许把最后一个 admin 降级（否则无人能进 identity 路由自救）。
		// 只在「当前是 admin 且要改成非 admin」时才检查 —— 非 admin 改角色不会减少管理员数。
		if canonical != middleware.RoleAdmin && middleware.CanonicalRole(user.Role) == middleware.RoleAdmin {
			var otherAdmins int64
			// LOWER(TRIM(...)) 兜住存量脏值（' Admin '），避免误判「唯一管理员」。
			if err := tx.Model(&models.User{}).
				Where("LOWER(TRIM(role)) = ? AND id <> ?", middleware.RoleAdmin, user.ID).
				Count(&otherAdmins).Error; err != nil {
				return fmt.Errorf("统计管理员失败: %w", err)
			}
			if otherAdmins == 0 {
				return fmt.Errorf("拒绝执行：%q 是唯一的管理员，降级后系统将没有 admin 可自救", username)
			}
		}

		if err := tx.Model(&models.User{}).Where("id = ?", user.ID).
			Update("role", canonical).Error; err != nil {
			return fmt.Errorf("更新 users.role 失败: %w", err)
		}
		return syncUserRoles(tx, user.ID.String(), canonical)
	})
	if err != nil {
		return err
	}

	log.Printf("✅ 用户 %s 角色 %s → %s", username, user.Role, canonical)
	return nil
}

// syncUserRoles 让 user_roles 关联表跟上 users.role。
// user_roles 只在 roles 表里存在对应 code 时才有行（兜底角色 user 没有行）。
func syncUserRoles(db *gorm.DB, userID, role string) error {
	if err := db.Exec("DELETE FROM user_roles WHERE user_id = ?", userID).Error; err != nil {
		return fmt.Errorf("清理 user_roles 失败: %w", err)
	}

	var roleID string
	err := db.Raw("SELECT id FROM roles WHERE code = ?", role).Row().Scan(&roleID)
	if err != nil {
		// user 是 000013 的兜底值，roles 表里没有对应行 —— 不算错误。
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("ℹ️ roles 表无 %q 行，跳过 user_roles 关联（仅 users.role 生效）", role)
			return nil
		}
		return fmt.Errorf("查询角色 %q 失败: %w", role, err)
	}

	if err := db.Exec(
		"INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", userID, roleID,
	).Error; err != nil {
		return fmt.Errorf("写入 user_roles 失败: %w", err)
	}
	return nil
}

// parseSetRoleEnv 解析 + 校验两个 env 变量。
func parseSetRoleEnv() (username, role string, err error) {
	username = strings.TrimSpace(os.Getenv("SET_ROLE_USERNAME"))
	role = strings.TrimSpace(os.Getenv("SET_ROLE_ROLE"))

	if username == "" {
		return "", "", errors.New("SET_ROLE_USERNAME 环境变量必填")
	}
	if role == "" {
		return "", "", errors.New("SET_ROLE_ROLE 环境变量必填")
	}
	return username, role, nil
}
