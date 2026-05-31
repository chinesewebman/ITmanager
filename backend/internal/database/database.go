package database

import (
	"fmt"
	"log"
	"time"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	DB  *gorm.DB
	err error
)

// Init 初始化数据库
func Init(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	// 配置日志
	newLogger := logger.New(
		log.New(log.Writer(), "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             time.Second, // 慢查询阈值
			LogLevel:                  logger.Info, // 日志级别
			IgnoreRecordNotFoundError: true,        // 忽略记录未找到错误
			Colorful:                  true,        // 彩色输出
		},
	)

	// 连接数据库
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

	// 配置连接池
	sqlDB, err := DB.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层连接失败: %w", err)
	}

	// 最大空闲连接数
	sqlDB.SetMaxIdleConns(10)
	// 最大打开连接数
	sqlDB.SetMaxOpenConns(100)
	// 连接最大存活时间
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 自动迁移表
	if err := autoMigrate(); err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}

	log.Println("✅ 数据库连接成功")
	return DB, nil
}

// autoMigrate 自动迁移表
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
