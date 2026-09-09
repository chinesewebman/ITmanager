package config

import (
	"errors"
	"fmt"
	"log"
	"net"
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
	// TrustedProxies 反向代理的来源（CIDR 或裸 IP）。空 = 不信任任何来源，
	// ClientIP() 取直连对端、忽略 X-Forwarded-For —— 直连部署下这是正确值。
	// 部署在反代后必须显式列出代理，否则限流退化为全站单桶、审计 IP 失真、
	// IP 白名单 API Key 一律 403（TODO G-7 / docs/FIX-PLAN-TRUSTED-PROXY.md）。
	TrustedProxies []string `mapstructure:"trusted_proxies"`
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
	// G-7：默认空 = 不信任任何来源。这行还承担一个作用——viper 只对「存在于
	// AllKeys 的键」做 env 覆盖，而 AllKeys 由 yaml + SetDefault 构成：升级时挂载的
	// 旧 config.yaml 若没有 trusted_proxies 键，NMP_SERVER_TRUSTED_PROXIES 会被静默忽略。
	viper.SetDefault("server.trusted_proxies", []string{})
	// G-13：同上。shipped config.yaml 里补了 api_key_pepper 空占位，但生产常按文档挂载
	// 自定义/旧的 config.yaml —— 那种文件没有这个键，只有 SetDefault 才能让 env 生效。
	viper.SetDefault("auth.api_key_pepper", "")
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("auth.jwt.expire", 86400)
	// G-16：缺这个键时 cfg.Log.Level 为空串，两条日志路径（pkg/logger 与 gorm）都会
	// 退化成最啰嗦档。SetDefault 同时让 NMP_LOG_LEVEL 在旧 config.yaml 上生效
	// （viper 只对 AllKeys 里的键做 env 覆盖，同 trusted_proxies / api_key_pepper）。
	viper.SetDefault("log.level", "info")

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
		// G-13b: 变量名必须与 viper 实际键一致（NMP_ + auth.api_key_pepper 的 . → _）。
		// 原先写 NMP_API_KEY_PEPPER，运维照提示注入仍然起不来。
		errs = append(errs, "auth.api_key_pepper 不能为空（通过 NMP_AUTH_API_KEY_PEPPER 注入）")
	} else if len(c.Auth.APIKeyPepper) < 32 {
		errs = append(errs, fmt.Sprintf("auth.api_key_pepper 长度 %d < 32 位最低要求", len(c.Auth.APIKeyPepper)))
	}

	// 受信代理（G-7）：先规整（去空白）再校验。必须把规整结果写回 cfg——
	// 否则「校验用 trim 后的值、运行期把原值交给 gin」会出现校验通过但
	// SetTrustedProxies 报错（gin 不 trim），且因列表非空连运行期告警都不挂。
	c.Server.TrustedProxies = trimTrustedProxies(c.Server.TrustedProxies)
	errs = append(errs, validateTrustedProxies(c.Server.TrustedProxies)...)

	// 生产模式额外校验集成 token
	if c.Server.Mode == "release" {
		if c.Integrations.Netbox.Token == "" {
			errs = append(errs, "integrations.netbox.token 在 release 模式下不能为空")
		}
		if c.Integrations.GLPI.AppToken == "" || c.Integrations.GLPI.UserToken == "" {
			errs = append(errs, "integrations.glpi.*_token 在 release 模式下不能为空")
		}
		// v2.2: Zabbix 用 user.login 鉴权，user/password 是真凭据；占位/默认密码必须拒掉
		if c.Integrations.Zabbix.URL != "" { // 未配置则跳过（集成可选）
			if c.Integrations.Zabbix.User == "" || c.Integrations.Zabbix.Password == "" {
				errs = append(errs, "integrations.zabbix.user/password 在 release 模式下不能为空（通过 NMP_INTEGRATIONS_ZABBIX_USER / NMP_INTEGRATIONS_ZABBIX_PASSWORD 注入）")
			} else if c.Integrations.Zabbix.User == "Admin" && c.Integrations.Zabbix.Password == "zabbix" {
				errs = append(errs, "integrations.zabbix 仍为 Zabbix 默认占位 Admin/zabbix（首次部署必须修改）")
			}
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// trimTrustedProxies 去掉每项的空白，保证校验值与交给 gin 的值是同一个。
func trimTrustedProxies(list []string) []string {
	if len(list) == 0 {
		return list
	}
	out := make([]string, len(list))
	for i, raw := range list {
		out[i] = strings.TrimSpace(raw)
	}
	return out
}

// validateTrustedProxies 校验 server.trusted_proxies 的每一项（G-7）。
//
// 规则与 gin 的解析保持一致（裸 IP 等价 /32、/128），并额外拒绝三类会让护栏
// 形同虚设的写法（均由安全审计实测绕过）：
//  1. 过宽前缀——`0.0.0.0/1` + `128.0.0.0/1` 就覆盖全部 IPv4，只拒 `/0` 不够；
//  2. IPv4-mapped IPv6——`::ffff:0:0/96` 等效 IPv4 `/0`，却只按 v6 规则量；
//  3. 主机位非零——`172.28.0.10/24` 被 ParseCIDR 静默归一成 `172.28.0.0/24`，
//     本意单机却信任了整个网段。
//
// 过宽等于 gin 默认的「信任所有来源」，配了等于没修。
func validateTrustedProxies(list []string) []string {
	var errs []string
	for _, entry := range list {
		if entry == "" {
			errs = append(errs, "server.trusted_proxies 含空条目")
			continue
		}
		if !strings.Contains(entry, "/") {
			if net.ParseIP(entry) == nil {
				errs = append(errs, fmt.Sprintf("server.trusted_proxies 条目 %q 不是合法 IP 或 CIDR", entry))
			}
			continue
		}
		ip, ipNet, err := net.ParseCIDR(entry)
		if err != nil {
			errs = append(errs, fmt.Sprintf("server.trusted_proxies 条目 %q 不是合法 CIDR", entry))
			continue
		}
		// 主机位非零 → 静默放宽，宁可让运维写清楚
		if !ip.Equal(ipNet.IP) {
			errs = append(errs, fmt.Sprintf(
				"server.trusted_proxies 条目 %q 主机位非零，等价于 %q（信任整个网段）：单机请写 %q",
				entry, ipNet.String(), hostCIDR(ip)))
			continue
		}
		ones, bits := ipNet.Mask.Size()
		switch {
		case bits == 32: // 纯 IPv4：代理不可能覆盖整个 /8 或更宽
			if ones < 8 {
				errs = append(errs, tooWideErr(entry, ones, 8))
			}
		case ipNet.IP.To4() != nil: // IPv4-mapped IPv6：::ffff:a.b.c.d/N
			// 只有「网段基址本身是 v4-mapped」的写法才会在 gin 里被当成 IPv4 网段
			// （gin 用 IPNet.Contains，而 Contains 对 v4-mapped 网段会取后 4 字节做掩码）。
			// 比 /96 更宽的写法（如 ::fffe:0:0/95）基址不再是 mapped，gin 侧对 IPv4
			// 客户端反而匹配不上 —— 不在这里拦，避免误拒（实测确认）。
			if eff := ones - 96; eff < 8 {
				errs = append(errs, fmt.Sprintf(
					"server.trusted_proxies 条目 %q 过宽：IPv4-mapped 形式等效 IPv4 前缀 /%d < /8（覆盖 %s 的 IPv4 客户端）",
					entry, eff, ipNet.String()))
			}
		default: // 纯 IPv6
			if ones < 16 {
				errs = append(errs, tooWideErr(entry, ones, 16))
			}
		}
	}
	return errs
}

func tooWideErr(entry string, ones, minOnes int) string {
	return fmt.Sprintf(
		"server.trusted_proxies 条目 %q 过宽（前缀 /%d < /%d）：受信范围必须限定到具体代理，否则 X-Forwarded-For 可被伪造",
		entry, ones, minOnes)
}

// hostCIDR 给出 ip 的单机 CIDR 写法（/32 或 /128）。
func hostCIDR(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String() + "/32"
	}
	return ip.String() + "/128"
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
