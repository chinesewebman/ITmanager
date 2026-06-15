package database

import (
	"embed"
	"fmt"
	"log"
	"time"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/migrate"
	"network-monitor-platform/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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

// Init 初始化数据库 + 运行 migration
func Init(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	// 配置日志
	newLogger := logger.New(
		log.New(log.Writer(), "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Info,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)

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

	sqlDB, err := DB.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层连接失败: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 注入 embed.FS 后跑 migration
	if MigrationsFS != (embed.FS{}) {
		migrate.FS = MigrationsFS
		if err := migrate.Up(DB); err != nil {
			return nil, fmt.Errorf("数据库迁移失败: %w", err)
		}
	} else {
		// 兜底：开发期用 gorm AutoMigrate（生产必须传 MigrationsFS）
		log.Println("⚠️  MigrationsFS 未注入，回退到 gorm AutoMigrate（仅开发用）")
		if err := autoMigrate(); err != nil {
			return nil, err
		}
	}

	log.Println("✅ 数据库连接成功")
	return DB, nil
}

// autoMigrate 自动迁移表（仅 AutoMigrate 兜底用，生产请用 migrations/*.sql）
func autoMigrate() error {
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

// GetDB 获取数据库实例
func GetDB() *gorm.DB {
	return DB
}

// SetDBForTest 测试注入全局 DB（cleanup 时用旧值恢复）
// 仅用于集成测试，生产代码不应调用
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
