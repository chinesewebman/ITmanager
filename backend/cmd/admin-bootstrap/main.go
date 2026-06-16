// Package admin_bootstrap 创建首个管理员账号
// 用法：
//
//	FIRST_ADMIN_USERNAME=admin \
//	FIRST_ADMIN_PASSWORD='MyStr0ngP@ssw0rd' \
//	go run ./cmd/admin-bootstrap
//
// 设计动机（C-F2 审计发现）：000001_init.up.sql 原本硬编码 admin/admin123 凭据，
// 任何拿到源码的攻击者都能直接登录生产库。改为受环境变量保护的独立命令，
// 运维首次部署时执行一次。
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("❌ admin bootstrap 失败: %v", err)
	}
}

func run() error {
	// 1. 解析 + 校验 env
	username, password, passwordHash, nickname, email, err := parseBootstrapEnv()
	if err != nil {
		return err
	}

	// 2. 加载配置 + 初始化 DB
	cfg, err := config.Load("config.yaml")
	if err != nil {
		return err
	}
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

	return runWithDeps(db, username, password, passwordHash, nickname, email)
}

// runWithDeps 在已注入 db 的前提下执行 bootstrap 主体。
// 暴露给测试使用：test 注入 sqlite gorm.DB，绕过 database.Init 走 postgres 的限制。
func runWithDeps(db *gorm.DB, username, password, passwordHash, nickname, email string) error {
	// 幂等检查：username 已存在则退出
	var existing models.User
	if err := db.First(&existing, "username = ?", username).Error; err == nil {
		return fmt.Errorf("用户 %q 已存在，跳过 bootstrap", username)
	}

	// 查找 admin 角色（迁移创建的种子角色）
	var adminRole struct {
		ID   string
		Code string
	}
	row := db.Raw("SELECT id, code FROM roles WHERE code = ?", "admin").Row()
	if err := row.Scan(&adminRole.ID, &adminRole.Code); err != nil {
		return errors.New("未找到 admin 角色，请先运行 migrate up 应用 000001_init.up.sql")
	}

	// 创建用户
	now := time.Now()
	user := models.User{
		ID:           uuid.New(),
		Username:     username,
		PasswordHash: passwordHash,
		Nickname:     nickname,
		Email:        email,
		Role:         adminRole.Code,
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.Create(&user).Error; err != nil {
		return fmt.Errorf("创建用户失败: %w", err)
	}

	// 关联 user_role
	if err := db.Exec(
		"INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)",
		user.ID, adminRole.ID,
	).Error; err != nil {
		return fmt.Errorf("关联角色失败: %w", err)
	}

	log.Printf("✅ admin 账号已创建: username=%s id=%s", user.Username, user.ID)
	return nil
}

// parseBootstrapEnv 解析 + 校验 5 个 env 变量。
// 抽出来让测试可独立验证输入校验逻辑。
func parseBootstrapEnv() (username, password, passwordHash, nickname, email string, err error) {
	username = strings.TrimSpace(os.Getenv("FIRST_ADMIN_USERNAME"))
	password = os.Getenv("FIRST_ADMIN_PASSWORD")
	passwordHash = os.Getenv("FIRST_ADMIN_PASSWORD_HASH")
	nickname = os.Getenv("FIRST_ADMIN_NICKNAME")
	email = os.Getenv("FIRST_ADMIN_EMAIL")

	if username == "" {
		return "", "", "", "", "", errors.New("FIRST_ADMIN_USERNAME 环境变量必填")
	}

	// 两种密码注入方式：明文（命令会用 bcrypt hash）或已 hash（CI/部署工具友好）
	if passwordHash == "" && password == "" {
		return "", "", "", "", "", errors.New("FIRST_ADMIN_PASSWORD 或 FIRST_ADMIN_PASSWORD_HASH 必填其一")
	}
	if password != "" && passwordHash != "" {
		return "", "", "", "", "", errors.New("FIRST_ADMIN_PASSWORD 和 FIRST_ADMIN_PASSWORD_HASH 互斥，只能给一个")
	}
	if passwordHash != "" {
		// 简单校验：bcrypt hash 形如 $2a$10$...
		if !strings.HasPrefix(passwordHash, "$2") {
			return "", "", "", "", "", errors.New("FIRST_ADMIN_PASSWORD_HASH 必须是 bcrypt 格式（$2a$... 或 $2b$...）")
		}
	} else {
		// 强度校验：>= 12 字符
		if len(password) < 12 {
			return "", "", "", "", "", errors.New("FIRST_ADMIN_PASSWORD 长度必须 >= 12 字符")
		}
		hash, hErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hErr != nil {
			return "", "", "", "", "", fmt.Errorf("bcrypt hash 失败: %w", hErr)
		}
		passwordHash = string(hash)
	}

	if nickname == "" {
		nickname = "管理员"
	}
	if email == "" {
		email = username + "@company.local"
	}
	return username, password, passwordHash, nickname, email, nil
}
