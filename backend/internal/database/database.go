package database

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"time"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/migrate"
	"network-monitor-platform/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// MigrationsFS 在 main 包里 embed migrations/ 目录
// 用法：在 cmd/server/main.go 和 cmd/migrate/main.go 中：
//
//	import "network-monitor-platform"
//	database.SetMigrationsFS(backend.MigrationsFS)
var MigrationsFS embed.FS

// SetMigrationsFS 注入 embed.FS
func SetMigrationsFS(fs embed.FS) {
	MigrationsFS = fs
}

var (
	DB  *gorm.DB
	err error
)

// InitWithAutoMigrate 初始化数据库，按 autoMigrate 决定是否运行迁移。
//  - autoMigrate=true (默认 back-compat): MigrationsFS 注入则跑 migrate.Up; 未注入则回退 gorm AutoMigrate.
//  - autoMigrate=false (M88 / G-14 多副本部署): 仅连接 DB, 不跑迁移. 期望 one-shot `migrate`
//    服务已先跑完. api 副本设 NMP_DATABASE_AUTOMIGRATE=false 后不抢 advisory lock, 多副本冷启动安全.
//
// 详见 TODO.md L70 + docs/FIX-PLAN-COMPOSE-RUNTIME.md D-C (M88 加注) + intent-M88-candidate.md.
func InitWithAutoMigrate(cfg *config.DatabaseConfig, autoMigrate bool) (*gorm.DB, error) {
	newLogger := newGormLogger(log.Writer())

	dsn := cfg.DSN()
	log.Printf("📦 正在连接数据库: %s:%d/%s", cfg.Host, cfg.Port, cfg.Name)

	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: newLogger,
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	if err := applyMigrations(DB, autoMigrate, nil); err != nil {
		return nil, err
	}

	log.Println("✅ 数据库连接成功")
	return DB, nil
}

// applyMigrations 是 InitWithAutoMigrate 内「按 autoMigrate 跑迁移」的纯逻辑, 抽出供
// initDBForTest(dialector, autoMigrate, overrideFS) 走 sqlite 验证. 仅对 dialector
// 类型 (postgres / sqlite) 通用, 不涉及具体 dsn 解析.
//
// overrideFS 优先级: 非 nil → 直接用 (test path); nil → 看 MigrationsFS 是否非零 (生产 path);
// 都拿不到 → 走 autoMigrate 兜底.
func applyMigrations(db *gorm.DB, autoMigrate bool, overrideFS fs.FS) error {
	if !autoMigrate {
		// M88 / G-14: 多副本部署契约. 不调 migrate.Up 也就**不**触发 pg_try_advisory_lock →
		// 多副本冷启动无锁竞争. api 副本期望 one-shot `migrate` 服务已先跑过.
		log.Println("⏭️  database.automigrate=false, 跳过 migrate.Up (期望 migrate one-shot 服务已跑过, 多副本部署契约)")
		return nil
	}
	if overrideFS != nil {
		migrate.FS = overrideFS
		if err := migrate.Up(db); err != nil {
			return fmt.Errorf("数据库迁移失败: %w", err)
		}
		return nil
	}
	if MigrationsFS != (embed.FS{}) {
		migrate.FS = MigrationsFS
		if err := migrate.Up(db); err != nil {
			return fmt.Errorf("数据库迁移失败: %w", err)
		}
		return nil
	}
	// 兜底：开发期用 gorm AutoMigrate（生产必须传 MigrationsFS）
	log.Println("⚠️  MigrationsFS 未注入，回退到 gorm AutoMigrate（仅开发用）")
	return autoMigrateFn()
}

// initDBForTest 是 InitWithAutoMigrate 的核心逻辑, 接受 gorm.Dialector + 迁移 FS 注入让测试可以
// 替换 postgres 为 sqlite + 用 SubFS 包装提供 migrations/ 前缀 (migrate.Load 要求).
//   - 建 gorm.DB + SetMax*(连接池)
//   - 按 autoMigrate + overrideFS 决定是否跑 migrate.Up
//   - 不关心 dialector 是 postgres 还是 sqlite (migrate.go 内置 sqlite 分支,
//     见 ensureTable / acquireLock)
//
// 命名带 ForTest 因为生产 InitWithAutoMigrate 不调它; 它**仅**给同包测试用,
// 避免生产代码直接依赖 sqlite driver (会污染 backend/Dockerfile 镜像构建依赖).
// 见 database_test.go 的 TestInitWithAutoMigrate_*.
func initDBForTest(dialector gorm.Dialector, autoMigrate bool, overrideFS fs.FS) (*gorm.DB, error) {
	newLogger := newGormLogger(log.Writer())

	DB, err = gorm.Open(dialector, &gorm.Config{
		Logger: newLogger,
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层连接失败: %w", err)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := applyMigrations(DB, autoMigrate, overrideFS); err != nil {
		return nil, err
	}

	log.Println("✅ 数据库连接成功 (test path)")
	return DB, nil
}

// Init kept for backward compat — calls InitWithAutoMigrate(cfg, true).
// 既有 4 个 caller (cmd/admin-bootstrap / cmd/seed / cmd/set-role / cmd/migrate) 不动.
// cmd/server 改用 InitWithAutoMigrate(&cfg.Database, cfg.Database.AutoMigrate) 以支持多副本部署.
func Init(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	return InitWithAutoMigrate(cfg, true)
}

// autoMigrateFn 自动迁移表（仅 AutoMigrate 兜底用，生产请用 migrations/*.sql）
func autoMigrateFn() error {
	log.Println("🔄 正在迁移数据库表...")
	models := []interface{}{
		&models.User{},
		&models.APIKey{},
		&models.AuditLog{},
		&models.Asset{},
		&models.AssetNetwork{},
		&models.Rack{},
		&models.Site{},
		&models.Alert{},
		&models.AlertRule{},
		&models.Ticket{},
		&models.TicketHistory{},
		&models.NotificationChannel{},
		&models.NotificationLog{},
	}
	for _, model := range models {
		if err := DB.AutoMigrate(model); err != nil {
			return err
		}
	}
	log.Println("✅ 数据库表迁移完成")
	return nil
}

// autoMigrate 暴露为 alias, 保持既有调用点 + 测试 (database_test.go:170/195) 不破坏.
func autoMigrate() error { return autoMigrateFn() }

// GetDB 获取数据库实例
func GetDB() *gorm.DB {
	return DB
}

// SetDBForTest 测试注入全局 DB（cleanup 时用旧值恢复）
func SetDBForTest(g *gorm.DB) {
	DB = g
}

// Close 关闭数据库连接
func Close() error {
	if DB != nil {
		sqlDB, err := DB.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}
