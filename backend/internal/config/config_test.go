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

// ==================== v2.2: Zabbix release-mode 校验 ====================

func TestValidate_ReleaseMode_ZabbixDefault_AdminZabbix_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Server.Mode = "release"
	cfg.Integrations.Netbox.Token = "real-netbox-token-123"
	cfg.Integrations.GLPI.AppToken = "app-tok"
	cfg.Integrations.GLPI.UserToken = "user-tok"
	cfg.Integrations.Zabbix.URL = "http://zabbix:8080"
	cfg.Integrations.Zabbix.User = "Admin"
	cfg.Integrations.Zabbix.Password = "zabbix" // 默认占位
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "默认占位 Admin/zabbix")
}

func TestValidate_ReleaseMode_ZabbixEmptyPassword_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Server.Mode = "release"
	cfg.Integrations.Netbox.Token = "real-netbox-token-123"
	cfg.Integrations.GLPI.AppToken = "app-tok"
	cfg.Integrations.GLPI.UserToken = "user-tok"
	cfg.Integrations.Zabbix.URL = "http://zabbix:8080"
	cfg.Integrations.Zabbix.User = "Admin"
	cfg.Integrations.Zabbix.Password = "" // 空
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "integrations.zabbix.user/password")
}

func TestValidate_ReleaseMode_ZabbixEmptyUser_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Server.Mode = "release"
	cfg.Integrations.Netbox.Token = "real-netbox-token-123"
	cfg.Integrations.GLPI.AppToken = "app-tok"
	cfg.Integrations.GLPI.UserToken = "user-tok"
	cfg.Integrations.Zabbix.URL = "http://zabbix:8080"
	cfg.Integrations.Zabbix.User = ""
	cfg.Integrations.Zabbix.Password = "real-zabbix-pwd"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "integrations.zabbix.user/password")
}

func TestValidate_ReleaseMode_ZabbixRealCreds_OK(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Server.Mode = "release"
	cfg.Integrations.Netbox.Token = "real-netbox-token-123"
	cfg.Integrations.GLPI.AppToken = "app-tok"
	cfg.Integrations.GLPI.UserToken = "user-tok"
	cfg.Integrations.Zabbix.URL = "http://zabbix:8080"
	cfg.Integrations.Zabbix.User = "nmp-zabbix-api"
	cfg.Integrations.Zabbix.Password = "real-zabbix-pwd-xyz"
	assert.NoError(t, cfg.Validate())
}

func TestValidate_ReleaseMode_ZabbixNotConfigured_OK(t *testing.T) {
	// Zabbix URL 空 → 集成未启用 → 跳过校验（集成可选）
	cfg := minimalValidConfig()
	cfg.Server.Mode = "release"
	cfg.Integrations.Netbox.Token = "real-netbox-token-123"
	cfg.Integrations.GLPI.AppToken = "app-tok"
	cfg.Integrations.GLPI.UserToken = "user-tok"
	cfg.Integrations.Zabbix.URL = "" // 未配置
	assert.NoError(t, cfg.Validate())
}

func TestValidate_DebugMode_ZabbixDefault_OK(t *testing.T) {
	// debug 模式不强制 Zabbix 占位校验（开发友好）
	cfg := minimalValidConfig()
	cfg.Server.Mode = "debug"
	cfg.Integrations.Zabbix.URL = "http://localhost:8080"
	cfg.Integrations.Zabbix.User = "Admin"
	cfg.Integrations.Zabbix.Password = "zabbix" // debug 模式允许
	assert.NoError(t, cfg.Validate())
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

// ==================== 受信代理校验（G-7） ====================

func TestValidate_TrustedProxies_合法(t *testing.T) {
	for _, entry := range []string{
		"172.28.0.10",            // 裸 IPv4（gin 等价 /32）
		"172.28.0.10/32",         // 单机
		"172.28.0.0/24",          // 一个网段
		"10.0.0.0/8",             // 私有段（合法但过宽，见文档「信任的是代理本身」）
		"::1",                    // 裸 IPv6（gin 等价 /128）
		"fd00:1234::/64",         // IPv6 网段
		"2001:db8::1/128",        // IPv6 单机
		"::ffff:172.28.0.10/128", // IPv4-mapped 单机（等效 /32）
		"::ffff:10.0.0.0/104",    // IPv4-mapped 网段（等效 IPv4 /8，与 10.0.0.0/8 同宽）
		" 172.28.0.10 ",          // 带空白 → 规整后合法
	} {
		cfg := minimalValidConfig()
		cfg.Server.TrustedProxies = []string{entry}
		assert.NoError(t, cfg.Validate(), "条目 %q 应合法", entry)
	}
}

func TestValidate_TrustedProxies_非法(t *testing.T) {
	for _, entry := range []string{
		"not-a-cidr",
		"300.1.1.1",
		"172.28.0.10/33",
		"172.28.0.10/24", // 主机位非零 → 会被静默归一为整个 /24
		"",
		"  ", // 仅空白
	} {
		cfg := minimalValidConfig()
		cfg.Server.TrustedProxies = []string{entry}
		err := cfg.Validate()
		require.Error(t, err, "条目 %q 应被拒", entry)
		assert.Contains(t, err.Error(), "trusted_proxies")
	}
}

// 主机位非零必须单独报错（不是「过宽」也不是「非法 CIDR」）：运维要能看懂该改成什么。
func TestValidate_TrustedProxies_主机位非零被拒(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Server.TrustedProxies = []string{"172.28.0.10/24"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "主机位非零")
	assert.Contains(t, err.Error(), `"172.28.0.0/24"`, "提示里应给出归一后的网段写法")
	assert.Contains(t, err.Error(), `"172.28.0.10/32"`, "提示里应给出单机写法")
}

// IPv6 的主机位非零同样要拒，提示里给 /128 写法。
func TestValidate_TrustedProxies_IPv6主机位非零被拒(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Server.TrustedProxies = []string{"2001:db8::1/64"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "主机位非零")
	assert.Contains(t, err.Error(), `"2001:db8::/64"`)
	assert.Contains(t, err.Error(), `"2001:db8::1/128"`)
}

// 空白规整：校验值与运行期交给 gin 的值必须是同一个（安全审计发现的不一致）。
func TestValidate_TrustedProxies_空白被规整回写(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Server.TrustedProxies = []string{" 172.28.0.10 "}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, []string{"172.28.0.10"}, cfg.Server.TrustedProxies,
		"Validate 必须把 trim 后的值写回 cfg，否则 gin 侧会拿到带空白的串而报错")
}

// IPv4-mapped IPv6 段必须按「等效 IPv4 前缀」判定（安全审计实测绕过：
// ::ffff:0:0/96 只过 /16 的 v6 门槛，实际覆盖全部 IPv4）。
func TestValidate_TrustedProxies_IPv4Mapped段过宽被拒(t *testing.T) {
	for _, entry := range []string{
		"::ffff:0:0/96",         // 等效 IPv4 /0 = 全部 IPv4
		"::ffff:0.0.0.0/96",     // 同上，点分写法
		"0:0:0:0:0:ffff:0:0/96", // 同上，全展开写法
		"::ffff:128.0.0.0/97",   // 等效 IPv4 /1 = 半个 IPv4
	} {
		cfg := minimalValidConfig()
		cfg.Server.TrustedProxies = []string{entry}
		err := cfg.Validate()
		require.Error(t, err, "条目 %q 应被拒", entry)
		assert.Contains(t, err.Error(), "过宽", "条目 %q 的错误信息应说明过宽", entry)
	}
}

// 不误拒：基址不再是 v4-mapped 的更宽 v6 网段（如 ::fffe:0:0/95）在 gin 侧
// 匹配不上 IPv4 客户端（IPNet.Contains 会把参数转成 4 字节，长度不匹配），
// 不构成「等效 IPv4」绕过，按普通 v6 规则（≥ /16）处理即可。
func TestValidate_TrustedProxies_宽v6网段不误拒(t *testing.T) {
	for _, entry := range []string{"::fffe:0:0/95", "::/64"} {
		cfg := minimalValidConfig()
		cfg.Server.TrustedProxies = []string{entry}
		assert.NoError(t, cfg.Validate(), "条目 %q 不应被误拒", entry)
	}
}

// 过宽前缀必须硬拒：安全审查实测 `0.0.0.0/1` + `128.0.0.0/1` 就能覆盖全部 IPv4，
// 只拒 `/0` 的护栏会被这种写法绕过；而这两个条目覆盖全网 = gin 默认的不安全配置。
func TestValidate_TrustedProxies_过宽被拒(t *testing.T) {
	for _, entry := range []string{
		"0.0.0.0/0",
		"0.0.0.0/1",
		"128.0.0.0/1",
		"::/0",
		"::/1",
	} {
		cfg := minimalValidConfig()
		cfg.Server.TrustedProxies = []string{entry}
		err := cfg.Validate()
		require.Error(t, err, "过宽条目 %q 应被拒", entry)
		assert.Contains(t, err.Error(), "过宽")
	}
}

// 多项里只要有一个非法就要拒（防止「配了一堆、坏的那个被忽略」）。
func TestValidate_TrustedProxies_多项含非法则整体拒(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Server.TrustedProxies = []string{"172.28.0.10/32", "bogus"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}

// V-6：env 覆盖依赖 viper 的 AllKeys 机制（纯 env 键不进 AllKeys），
// 所以 yaml 里必须有 `trusted_proxies: []` 占位。这条测试同时钉住「占位存在时覆盖生效」。
func TestLoad_TrustedProxiesEnvOverride(t *testing.T) {
	yaml := `server:
  mode: debug
  trusted_proxies: []
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

	// 无 env → 空（不信任任何来源）
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.Server.TrustedProxies, "未设 env 时应为空")

	// 有 env → 逗号分隔覆盖
	t.Setenv("NMP_SERVER_TRUSTED_PROXIES", "172.28.0.10,10.0.1.5/32")
	cfg, err = Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"172.28.0.10", "10.0.1.5/32"}, cfg.Server.TrustedProxies,
		"env 必须能覆盖 yaml 的空列表（否则部署漏配且无报错）")
}

// 升级场景：挂载的旧 config.yaml 没有 trusted_proxies 键时，env 也必须生效。
// 依赖 Load() 里的 viper.SetDefault("server.trusted_proxies", []string{})
// （viper 只对 AllKeys 里的键做 env 覆盖，缺键会静默忽略 env）。
func TestLoad_TrustedProxiesEnvOverride_YAML无键时仍生效(t *testing.T) {
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

	t.Setenv("NMP_SERVER_TRUSTED_PROXIES", "172.28.0.10")
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"172.28.0.10"}, cfg.Server.TrustedProxies,
		"旧 config.yaml 缺 trusted_proxies 键时 env 也必须生效（否则升级后静默不信任任何来源）")
}

// TestLoad_ShippedConfigYAML_必填Env齐备时成功 直接读仓库里随镜像发布的
// backend/config.yaml（不是临时构造的 yaml），钉住「文档化的部署路径真的能用」。
//
// 为什么必须用 shipped 文件：G-13 的根因是 shipped yaml 缺 auth.api_key_pepper 键，
// 而 viper 只对 AllKeys（yaml 键 + SetDefault 键）做 env 覆盖。其余测试都用临时 yaml
// （都带这个键），于是全绿而真实部署必挂。以后谁新增「必须 env 注入」的字段却忘加
// yaml 占位，这条测试会红。
func TestLoad_ShippedConfigYAML_必填Env齐备时成功(t *testing.T) {
	path := filepath.Join("..", "..", "config.yaml")
	require.FileExists(t, path, "随仓库发布的 backend/config.yaml 必须在，测试才有意义")

	t.Setenv("NMP_AUTH_JWT_SECRET", validSecret)
	t.Setenv("NMP_DATABASE_PASSWORD", "shipped-yaml-probe-password")
	t.Setenv("NMP_AUTH_API_KEY_PEPPER", validPepper)

	cfg, err := Load(path)
	require.NoError(t, err, "shipped config.yaml + 三个必需 env 必须能启动（否则文档化部署路径是死的）")
	assert.Equal(t, validSecret, cfg.Auth.JWT.Secret)
	assert.Equal(t, "shipped-yaml-probe-password", cfg.Database.Password)
	assert.Equal(t, validPepper, cfg.Auth.APIKeyPepper)
}

// 升级场景：挂载的旧 config.yaml 没有 api_key_pepper 键时，env 也必须生效。
// 与 TestLoad_TrustedProxiesEnvOverride_YAML无键时仍生效 同款，依赖
// Load() 里的 viper.SetDefault("auth.api_key_pepper", "")（G-13 + 审查 S-1）。
func TestLoad_APIKeyPepperEnvOverride_YAML无键时仍生效(t *testing.T) {
	yaml := `server:
  mode: debug
database:
  password: real-password
auth:
  jwt:
    secret: "` + validSecret + `"
    expire: 86400
log:
  level: info
  format: json
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	t.Setenv("NMP_AUTH_API_KEY_PEPPER", validPepper)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, validPepper, cfg.Auth.APIKeyPepper,
		"旧 config.yaml 缺 api_key_pepper 键时 env 也必须生效（否则升级后服务起不来）")
}

// 报错文案必须写 viper 真正认的变量名（G-13b）：NMP_API_KEY_PEPPER 是错的，
// 运维照提示注入会继续起不来。
func TestLoad_缺少Pepper时报错文案指向正确的环境变量名(t *testing.T) {
	yaml := `server:
  mode: debug
database:
  password: real-password
auth:
  jwt:
    secret: "` + validSecret + `"
    expire: 86400
log:
  level: info
  format: json
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	t.Setenv("NMP_AUTH_API_KEY_PEPPER", "")

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NMP_AUTH_API_KEY_PEPPER",
		"报错文案必须给出 viper 真正认的变量名")
	assert.NotContains(t, err.Error(), "NMP_API_KEY_PEPPER",
		"不能继续提示这个不存在的变量名")
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
