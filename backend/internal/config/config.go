package config

import (
	"fmt"
	"log"
	"time"

	"github.com/spf13/viper"
)

// Config 全局配置
type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	Database      DatabaseConfig      `mapstructure:"database"`
	Redis         RedisConfig         `mapstructure:"redis"`
	Integrations  IntegrationsConfig  `mapstructure:"integrations"`
	Auth          AuthConfig          `mapstructure:"auth"`
	Log           LogConfig           `mapstructure:"log"`
	Notifications NotificationsConfig `mapstructure:"notifications"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"`
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
	JWT  JWTConfig  `mapstructure:"jwt"`
	LDAP LDAPConfig `mapstructure:"ldap"`
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

// Load 加载配置
func Load(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	// 设置默认值
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.mode", "debug")
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("auth.jwt_secret.expire", 86400)

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	cfg = &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	log.Printf("✅ 配置加载成功 (env: %s)", cfg.Server.Mode)
	return cfg, nil
}

// Get 获取配置
func Get() *Config {
	if cfg == nil {
		panic("配置未加载，请先调用 config.Load()")
	}
	return cfg
}

// GetDuration 获取持续时间
func (c *Config) GetDuration() time.Duration {
	return time.Duration(c.Auth.JWT.Expire) * time.Second
}
