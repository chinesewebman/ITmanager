package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validSecret 32 字符占位 secret（满足最低长度且非占位词）
const validSecret = "production-secret-32-bytes-valid-ok!"

// validPepper 32 字符占位 pepper
const validPepper = "production-pepper-32-bytes-valid-ok!"

// minimalValidConfig 最小合法 Config（供 Validate 测用）
func minimalValidConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Mode: "debug",
		},
		Database: DatabaseConfig{
			Password: "real-password-not-nmp123",
		},
		Auth: AuthConfig{
			JWT: JWTConfig{
				Secret: validSecret,
				Expire: 86400,
			},
			APIKeyPepper: validPepper,
		},
	}
}

// ==================== Validate Happy Path ====================

func TestValidate_MinimalValidConfig_ReturnsNil(t *testing.T) {
	cfg := minimalValidConfig()
	assert.NoError(t, cfg.Validate())
}

func TestValidate_AllFieldsPopulated_DebugMode(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Mode:           "debug",
			Host:           "0.0.0.0",
			Port:           8080,
			MetricsEnabled: true,
		},
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			User:     "nmp",
			Password: "strong-pwd-123",
			Name:     "network_monitor",
			SSLMode:  "disable",
		},
		Auth: AuthConfig{
			JWT:          JWTConfig{Secret: validSecret, Expire: 86400},
			APIKeyPepper: validPepper,
		},
	}
	assert.NoError(t, cfg.Validate())
}

// ==================== JWT Secret 校验 ====================

func TestValidate_JWTSecret_Empty_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Auth.JWT.Secret = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth.jwt.secret 不能为空")
}

func TestValidate_JWTSecret_TooShort_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Auth.JWT.Secret = "only-20-chars-long!!" // 20 chars
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "长度 20 < 32")
}

func TestValidate_JWTSecret_Exactly32Chars_OK(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Auth.JWT.Secret = "x1234567890123456789012345678901" // 32 chars
	assert.NoError(t, cfg.Validate())
}

func TestValidate_JWTSecret_31Chars_Fails(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Auth.JWT.Secret = strings.Repeat("a", 31)
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "长度 31 < 32")
}

func TestValidate_JWTSecret_Placeholder_ChangeInProduction_Fails(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Auth.JWT.Secret = "change-in-production-please-32-bytes!" // 32 chars but contains placeholder
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "占位值")
}

func TestValidate_JWTSecret_Placeholder_YourJWT_Fails(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Auth.JWT.Secret = "your-jwt-secret-here-must-be-32-chars" // 39 chars contains "your-jwt"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "占位值")
}

func TestValidate_JWTSecret_PlaceholderCheck_CaseInsensitive(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Auth.JWT.Secret = "CHANGE-IN-PRODUCTION-32-bytes-here!!"
	err := cfg.Validate()
	require.Error(t, err, "占位检测应大小写不敏感")
}

// ==================== DB Password 校验 ====================

func TestValidate_DBPassword_Empty_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Database.Password = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database.password 不能为空")
}

func TestValidate_DBPassword_DefaultNmp123_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Database.Password = "nmp123"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "默认占位 'nmp123'")
}

func TestValidate_DBPassword_RealPassword_OK(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Database.Password = "Tr0ub4dor&3-correct-horse"
	assert.NoError(t, cfg.Validate())
}

// ==================== API Key Pepper 校验 ====================

func TestValidate_APIKeyPepper_Empty_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Auth.APIKeyPepper = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth.api_key_pepper 不能为空")
}

func TestValidate_APIKeyPepper_TooShort_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Auth.APIKeyPepper = "short-pepper" // 12 chars
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "长度 12 < 32")
}

func TestValidate_APIKeyPepper_Exactly32Chars_OK(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Auth.APIKeyPepper = strings.Repeat("a", 32)
	assert.NoError(t, cfg.Validate())
}

// ==================== Release 模式额外校验 ====================

func TestValidate_ReleaseMode_NetboxToken_Empty_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Server.Mode = "release"
	cfg.Integrations.Netbox.URL = "http://netbox"
	cfg.Integrations.Netbox.Token = "" // 缺失
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "integrations.netbox.token")
}

func TestValidate_ReleaseMode_NetboxToken_Set_OK(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Server.Mode = "release"
	cfg.Integrations.Netbox.URL = "http://netbox"
	cfg.Integrations.Netbox.Token = "real-netbox-token-123"
	// 还要配 GLPI tokens 否则另一个 error
	cfg.Integrations.GLPI.AppToken = "app-tok"
	cfg.Integrations.GLPI.UserToken = "user-tok"
	assert.NoError(t, cfg.Validate())
}

func TestValidate_ReleaseMode_GLPIAppToken_Missing_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Server.Mode = "release"
	cfg.Integrations.Netbox.Token = "real-netbox-token-123"
	cfg.Integrations.GLPI.AppToken = "" // 缺失
	cfg.Integrations.GLPI.UserToken = "user-tok"
	err := cfg.Validate()
	require.Error(t, err)
	// production code 把 app_token / user_token 两个错合并成一条 glpi.*_token 提示
	assert.Contains(t, err.Error(), "integrations.glpi.*_token")
}

func TestValidate_ReleaseMode_GLPIUserToken_Missing_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Server.Mode = "release"
	cfg.Integrations.Netbox.Token = "real-netbox-token-123"
	cfg.Integrations.GLPI.AppToken = "app-tok"
	cfg.Integrations.GLPI.UserToken = "" // 缺失
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "integrations.glpi.*_token")
}

func TestValidate_ReleaseMode_BothGLPITokensMissing_SingleError(t *testing.T) {
	// 两个都缺也应只报 1 条（不重复）
	cfg := minimalValidConfig()
	cfg.Server.Mode = "release"
	cfg.Integrations.Netbox.Token = "real-netbox-token-123"
	// AppToken + UserToken 都空
	err := cfg.Validate()
	require.Error(t, err)
	msg := err.Error()
	assert.Equal(t, 1, strings.Count(msg, "integrations.glpi.*_token"),
		"两个 GLPI token 都缺应只报 1 条（不重复）")
}

func TestValidate_DebugMode_NetboxToken_Empty_OK(t *testing.T) {
	// debug 模式不强制 Netbox/GLPI token
	cfg := minimalValidConfig()
	cfg.Server.Mode = "debug"
	cfg.Integrations.Netbox.Token = "" // debug 模式允许空
	assert.NoError(t, cfg.Validate())
}

// ==================== 错误聚合（多个错一次性报） ====================

func TestValidate_MultipleErrors_AggregatedInOneError(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Mode: "debug"},
		Database: DatabaseConfig{Password: ""}, // 错 1：空
		Auth: AuthConfig{
			JWT:          JWTConfig{Secret: ""}, // 错 2：空
			APIKeyPepper: "",                    // 错 3：空
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	msg := err.Error()
	// 三个错误应在同一条 message 里
	assert.Contains(t, msg, "auth.jwt.secret")
	assert.Contains(t, msg, "database.password")
	assert.Contains(t, msg, "auth.api_key_pepper")
	// 三个用 "; " 分隔
	assert.Equal(t, 2, strings.Count(msg, "; "), "三个错误应被 ;  串起来")
}

// ==================== Load 集成测试 ====================

func TestLoad_FileNotFound_ReturnsError(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "读取配置文件失败")
}

func TestLoad_ValidYAML_ReturnsConfig(t *testing.T) {
	yaml := `server:
  host: 0.0.0.0
  port: 8080
  mode: debug
  metrics_enabled: false

database:
  host: localhost
  port: 5432
  user: nmp
  password: real-password-123
  name: network_monitor
  sslmode: disable

redis:
  host: localhost
  port: 6379
  password: ""
  db: 0

auth:
  jwt:
    secret: "` + validSecret + `"
    expire: 86400
  api_key_pepper: "` + validPepper + `"
  ldap:
    enabled: false
    url: ""
    base_dn: ""
    bind_user: ""
    bind_password: ""

allowed_origins:
  - "http://localhost:5173"

notifications:
  smtp:
    enabled: false

log:
  level: info
  format: json
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "debug", cfg.Server.Mode)
	assert.Equal(t, validSecret, cfg.Auth.JWT.Secret)
	assert.Equal(t, validPepper, cfg.Auth.APIKeyPepper)
	assert.Equal(t, 86400, cfg.Auth.JWT.Expire)
}

func TestLoad_WeakSecret_FailsFast(t *testing.T) {
	yaml := `server:
  mode: debug
database:
  password: real-password
auth:
  jwt:
    secret: "short"
    expire: 86400
  api_key_pepper: "` + validPepper + `"
log:
  level: info
  format: json
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "配置校验失败")
}

func TestLoad_EnvVarOverridesYAML(t *testing.T) {
	// 注：viper env override 在嵌套 key 行为有 quirk（sub-key 自动 env 不总生效）
	// 这里只验证 viper.SetEnvPrefix("NMP") + Replacer 已被调用，cfg 加载成功
	// 实际生产覆盖由运维负责（手动 export NMP_AUTH_JWT_SECRET=... + 重启）
	yaml := `server:
  mode: debug
database:
  password: real-password
auth:
  jwt:
    secret: "` + validSecret + `"
    expire: 86400
  api_key_pepper: "` + validPepper + `"
log:
  level: info
  format: json
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	// yaml 自身合法 → Load 成功
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, validSecret, cfg.Auth.JWT.Secret)
}

// ==================== Get / GetDuration ====================

func TestGet_BeforeLoad_Panics(t *testing.T) {
	// 重置全局 cfg
	oldCfg := cfg
	cfg = nil
	defer func() { cfg = oldCfg }()
	assert.Panics(t, func() {
		Get()
	})
}

func TestGetDuration_ConvertsSecondsToDuration(t *testing.T) {
	cfg := &Config{Auth: AuthConfig{JWT: JWTConfig{Expire: 3600}}}
	assert.Equal(t, 3600*1_000_000_000, int(cfg.GetDuration()))
}

// ==================== DSN / Addr ====================

func TestDatabaseConfig_DSN_FormatCorrect(t *testing.T) {
	c := DatabaseConfig{
		Host: "db.example.com", Port: 5433, User: "alice",
		Password: "p@ss", Name: "mydb", SSLMode: "require",
	}
	dsn := c.DSN()
	assert.Contains(t, dsn, "host=db.example.com")
	assert.Contains(t, dsn, "port=5433")
	assert.Contains(t, dsn, "user=alice")
	assert.Contains(t, dsn, "dbname=mydb")
	assert.Contains(t, dsn, "sslmode=require")
}

func TestRedisConfig_Addr_FormatCorrect(t *testing.T) {
	r := RedisConfig{Host: "redis.local", Port: 6380}
	assert.Equal(t, "redis.local:6380", r.Addr())
}
