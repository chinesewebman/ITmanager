package config

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 全局配置
type Config struct {
	Server         ServerConfig        `mapstructure:"server"`
	Database       DatabaseConfig      `mapstructure:"database"`
	Redis          RedisConfig         `mapstructure:"redis"`
	Integrations   IntegrationsConfig  `mapstructure:"integrations"`
	Auth           AuthConfig          `mapstructure:"auth"`
	Log            LogConfig           `mapstructure:"log"`
	Notifications  NotificationsConfig `mapstructure:"notifications"`
	AllowedOrigins []string            `mapstructure:"allowed_origins"`
}

type ServerConfig struct {
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	Mode           string `mapstructure:"mode"`
	MetricsEnabled bool   `mapstructure:"metrics_enabled"` // C-P5: 暴露 /metrics
}

type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	SSLMode  string `mapstructure:"sslmode"`
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.Name, d.SSLMode)
}

type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

func (r RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

type IntegrationsConfig struct {
	Netbox NetboxConfig `mapstructure:"netbox"`
	Zabbix ZabbixConfig `mapstructure:"zabbix"`
	GLPI   GLPIConfig   `mapstructure:"glpi"`
}

type NetboxConfig struct {
	URL   string `mapstructure:"url"`
	Token string `mapstructure:"token"`
}

type ZabbixConfig struct {
	URL      string `mapstructure:"url"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
}

type GLPIConfig struct {
	URL       string `mapstructure:"url"`
	AppToken  string `mapstructure:"app_token"`
	UserToken string `mapstructure:"user_token"`
}

type AuthConfig struct {
	JWT          JWTConfig  `mapstructure:"jwt"`
	LDAP         LDAPConfig `mapstructure:"ldap"`
	APIKeyPepper string     `mapstructure:"api_key_pepper"`
}

type JWTConfig struct {
	Secret string `mapstructure:"secret"`
	Expire int    `mapstructure:"expire"`
}

type LDAPConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	BaseDN       string `mapstructure:"base_dn"`
	BindDN       string `mapstructure:"bind_dn"`
	BindPassword string `mapstructure:"bind_password"`
}

type LogConfig struct {
	Level  string        `mapstructure:"level"`
	Format string        `mapstructure:"format"`
	Output string        `mapstructure:"output"`
	File   LogFileConfig `mapstructure:"file"`
}

type LogFileConfig struct {
	Path       string `mapstructure:"path"`
	MaxSize    int    `mapstructure:"max_size"`
	MaxBackups int    `mapstructure:"max_backups"`
}

type NotificationsConfig struct {
	Dingtalk DingtalkConfig `mapstructure:"dingtalk"`
	Email    EmailConfig    `mapstructure:"email"`
}

type DingtalkConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	WebhookURL string `mapstructure:"webhook_url"`
	Secret     string `mapstructure:"secret"`
}

type EmailConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	SMTPHost     string `mapstructure:"smtp_host"`
	SMTPPort     int    `mapstructure:"smtp_port"`
	SMTPUser     string `mapstructure:"smtp_user"`
	SMTPPassword string `mapstructure:"smtp_password"`
	From         string `mapstructure:"from"`
}

var cfg *Config

// Load 加载配置（env var 优先覆盖 yaml）
func Load(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	// F1: 允许 NMP_DATABASE_PASSWORD / NMP_AUTH_JWT_SECRET / NMP_INTEGRATIONS_* 等
	// 环境变量覆盖 yaml 配置，避免硬编码 secret 落进仓库
	viper.SetEnvPrefix("NMP")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// 设置默认值
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.mode", "debug")
	viper.SetDefault("server.metrics_enabled", false) // C-P5: 默认关（外部暴露时再开）
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("auth.jwt.expire", 86400)

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	cfg = &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// F3: 启动 fail-fast 校验弱 secret
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置校验失败: %w", err)
	}

	log.Printf("✅ 配置加载成功 (env: %s)", cfg.Server.Mode)
	return cfg, nil
}

// Validate 启动 fail-fast 校验（C-F3 弱 secret 检测）
func (c *Config) Validate() error {
	var errs []string

	// JWT secret
	secret := c.Auth.JWT.Secret
	if secret == "" {
		errs = append(errs, "auth.jwt.secret 不能为空（通过 NMP_AUTH_JWT_SECRET 环境变量注入）")
	} else if len(secret) < 32 {
		errs = append(errs, fmt.Sprintf("auth.jwt.secret 长度 %d < 32 位最低要求", len(secret)))
	} else {
		low := strings.ToLower(secret)
		if strings.Contains(low, "change-in-production") ||
			strings.Contains(low, "your-jwt") {
			errs = append(errs, "auth.jwt.secret 仍为占位值")
		}
	}

	// DB password
	if c.Database.Password == "" {
		errs = append(errs, "database.password 不能为空（通过 NMP_DATABASE_PASSWORD 注入）")
	} else if c.Database.Password == "nmp123" {
		errs = append(errs, "database.password 仍为默认占位 'nmp123'")
	}

	// API Key pepper（C-F6 防离线彩虹表）
	if c.Auth.APIKeyPepper == "" {
		errs = append(errs, "auth.api_key_pepper 不能为空（通过 NMP_API_KEY_PEPPER 注入）")
	} else if len(c.Auth.APIKeyPepper) < 32 {
		errs = append(errs, fmt.Sprintf("auth.api_key_pepper 长度 %d < 32 位最低要求", len(c.Auth.APIKeyPepper)))
	}

	// 生产模式额外校验集成 token
	if c.Server.Mode == "release" {
		if c.Integrations.Netbox.Token == "" {
			errs = append(errs, "integrations.netbox.token 在 release 模式下不能为空")
		}
		if c.Integrations.GLPI.AppToken == "" || c.Integrations.GLPI.UserToken == "" {
			errs = append(errs, "integrations.glpi.*_token 在 release 模式下不能为空")
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Get 获取配置
func Get() *Config {
	if cfg == nil {
		panic("配置未加载，请先调用 config.Load()")
	}
	return cfg
}

// SetForTest 测试用 setter（允许 handler 测试不依赖 yaml 文件）
func SetForTest(c *Config) {
	cfg = c
}

// GetDuration 获取持续时间
func (c *Config) GetDuration() time.Duration {
	return time.Duration(c.Auth.JWT.Expire) * time.Second
}
