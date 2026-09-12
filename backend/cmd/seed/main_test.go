package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"io/fs"
	"net"
	"testing"

	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/notification"

	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// init 注册带 gen_random_uuid() 的 sqlite3 driver
func init() {
	sql.Register("sqlite3_uuid", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("gen_random_uuid", func() string {
				return uuid.New().String()
			}, true)
		},
	})
}

//go:embed all:seed-testdata/migrations/*.sql
var seedTestMigrations embed.FS

// seedFS 包装 embed.FS 让 .sql 文件直接暴露在根（"migrations/xxx.sql"）
type seedFS struct{ embed.FS }

func (s seedFS) Open(name string) (fs.File, error) {
	return s.FS.Open("seed-testdata/" + name)
}
func (s seedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return s.FS.ReadDir("seed-testdata/" + name)
}
func (s seedFS) ReadFile(name string) ([]byte, error) {
	return s.FS.ReadFile("seed-testdata/" + name)
}

// newTestDB 开 :memory: sqlite + 跑手写 schema（覆盖 seedData 调用的所有 model）
// 手写跟 gorm AutoMigrate 行为对齐：uuid 用 text，所有 model 字段都覆盖
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 9 张表，跟 model 一一对应
	stmts := []string{
		`CREATE TABLE users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			nickname TEXT,
			email TEXT,
			phone TEXT,
			avatar TEXT,
			department_id TEXT,
			status TEXT DEFAULT 'active',
			role TEXT DEFAULT 'user',
			failed_login INTEGER DEFAULT 0,
			locked_until DATETIME,
			last_login DATETIME,
			last_login_ip TEXT,
			-- C7: 跟 production 000012 migration 同步
			must_change_password INTEGER DEFAULT 1,
			password_set_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE sites (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			code TEXT,
			province TEXT,
			city TEXT,
			address TEXT,
			contact TEXT,
			contact_phone TEXT,
			tier TEXT,
			is_active INTEGER DEFAULT 1,
			net_box_id INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE racks (
			id TEXT PRIMARY KEY,
			site_id TEXT,
			site_name TEXT,
			name TEXT NOT NULL,
			total_u INTEGER DEFAULT 42,
			max_weight INTEGER,
			floor TEXT,
			row TEXT,
			column TEXT,
			status TEXT DEFAULT 'active',
			net_box_id INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE assets (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			asset_tag TEXT,
			sn TEXT,
			asset_type TEXT,
			brand TEXT,
			model TEXT,
			site_id TEXT,
			site_name TEXT,
			rack_id TEXT,
			rack_name TEXT,
			rack_position TEXT,
			purchase_date DATETIME,
			warranty_end DATETIME,
			vendor TEXT,
			vendor_contact TEXT,
			status TEXT DEFAULT 'active',
			online_time DATETIME,
			offline_time DATETIME,
			business_unit TEXT,
			service_name TEXT,
			tags TEXT,
			custom_fields TEXT,
			net_box_id INTEGER,
			source TEXT,
			last_known_ip4 TEXT,
			last_known_ip6 TEXT,
			retired_at DATETIME,
			retired_reason TEXT,
			retired_by TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME,
			-- 镜像真库约束（000001 的 unique_asset，000013 改名后指向 site_id）
			UNIQUE (asset_tag, site_id)
		)`,
		`CREATE TABLE asset_networks (
			id TEXT PRIMARY KEY,
			asset_id TEXT NOT NULL,
			interface_name TEXT,
			interface_type TEXT,
			mac_address TEXT,
			ipv4_address TEXT,
			ipv4_netmask TEXT,
			ipv6_address TEXT,
			speed INTEGER,
			duplex TEXT,
			status TEXT,
			connected_to TEXT,
			connected_port TEXT,
			purpose TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE alerts (
			id TEXT PRIMARY KEY,
			alert_id TEXT,
			host_id TEXT,
			host_name TEXT,
			host_ip TEXT,
			trigger_name TEXT,
			trigger_id TEXT,
			severity INTEGER,
			severity_name TEXT,
			problem TEXT,
			problem_start DATETIME,
			problem_end DATETIME,
			duration INTEGER,
			status TEXT DEFAULT 'problem',
			ack_time DATETIME,
			ack_user TEXT,
			resolve_time DATETIME,
			resolve_user TEXT,
			is_false_positive INTEGER DEFAULT 0,
			marked_by TEXT,
			marked_at DATETIME,
			false_positive_note TEXT,
			ticket_id TEXT,
			asset_id TEXT,
			source TEXT,
			alert_rule_id TEXT,
			repeat_count INTEGER DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE alert_rules (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			condition TEXT,
			asset_type TEXT,
			host_group TEXT,
			metric TEXT,
			operator TEXT,
			threshold REAL,
			duration INTEGER,
			severity INTEGER,
			severity_name TEXT,
			notify_enabled INTEGER,
			notify_channels TEXT,
			notify_users TEXT,
			is_enabled INTEGER,
			priority INTEGER,
			created_by TEXT,
			updated_by TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE tickets (
			id TEXT PRIMARY KEY,
			ticket_number TEXT,
			title TEXT,
			description TEXT,
			ticket_type TEXT,
			priority TEXT,
			status TEXT,
			requester_id TEXT,
			requester_name TEXT,
			requester_email TEXT,
			assignee_id TEXT,
			assignee_name TEXT,
			category TEXT,
			asset_id TEXT,
			asset_name TEXT,
			external_id TEXT,
			source TEXT,
			resolution TEXT,
			resolved_at DATETIME,
			closed_at DATETIME,
			due_date DATETIME,
			tags TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE notification_channels (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT,
			config TEXT,
			is_enabled INTEGER,
			is_default INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
	}
	for _, s := range stmts {
		require.NoError(t, db.Exec(s).Error)
	}
	return db
}

// ==================== seedData 测试 ====================

func TestSeed_空DB_创建默认用户(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var users []models.User
	require.NoError(t, db.Find(&users).Error)
	assert.GreaterOrEqual(t, len(users), 3, "应有 admin + operator + viewer")

	names := map[string]bool{}
	for _, u := range users {
		names[u.Username] = true
	}
	assert.True(t, names["admin"], "admin 用户应创建")
	assert.True(t, names["operator"], "operator 用户应创建")
	assert.True(t, names["viewer"], "viewer 用户应创建")
}

func TestSeed_admin密码为admin123_bcrypt加密(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var admin models.User
	require.NoError(t, db.Where("username = ?", "admin").First(&admin).Error)
	assert.True(t, len(admin.PasswordHash) > 50, "bcrypt hash 长度应 > 50")
	assert.NotEqual(t, "admin123", admin.PasswordHash, "不应明文存密码")
}

func TestSeed_已有users_跳过userSeed分支(t *testing.T) {
	db := newTestDB(t)
	// 预创建 1 个 user（userCount != 0 → seedData 整个 user 分支跳过）
	existing := models.User{
		ID:           uuid.New(),
		Username:     "pre_existing",
		PasswordHash: "hashed",
		Status:       "active",
		Role:         "user",
	}
	require.NoError(t, db.Create(&existing).Error)

	seedData(db)

	// users 总数应 = 1（只有 pre_existing）—— seed 跳过 user 分支
	var count int64
	db.Model(&models.User{}).Count(&count)
	assert.Equal(t, int64(1), count, "已有 user 时 seed 应跳过 user 分支")
}

func TestSeed_空DB_创建3个site(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var sites []models.Site
	require.NoError(t, db.Find(&sites).Error)
	assert.Equal(t, 3, len(sites), "应有 3 个 site")

	names := map[string]bool{}
	for _, s := range sites {
		names[s.Name] = true
	}
	assert.True(t, names["北京数据中心A"])
	assert.True(t, names["上海数据中心B"])
	assert.True(t, names["广州数据中心C"])
}

// TestSeed_资产asset_tag站内唯一 守真库约束 unique_asset UNIQUE(asset_tag, site_id)
// （000001 建的是 (asset_tag, idc_id)，000013 把 assets.idc_id 改名为 site_id，约束随之指向新列）。
//
// 反证：把 asset_tag 里的 rack.Name 去掉（退回只带 site.Code + 序号）→ 同站点 4 个机柜的
// 资产 tag 相同 → 真库 27+9 行 duplicate key。G-20 前这些失败被静默吞掉（exit 0，48 个演示资产
// 只建出 12 个）；G-20 后 seed 以非零码退出，才让这个存量缺陷浮出。
func TestSeed_资产asset_tag站内唯一(t *testing.T) {
	db := newTestDB(t)
	require.Equal(t, 0, seedData(db), "seedData 不应有任何失败处")

	var dup int64
	require.NoError(t, db.Raw(
		`SELECT count(*) FROM (SELECT asset_tag, site_id FROM assets
		    GROUP BY asset_tag, site_id HAVING count(*) > 1)`).Scan(&dup).Error)
	assert.Zero(t, dup, "同站点内 asset_tag 必须唯一，否则真库 unique_asset 冲突")

	var assets int64
	require.NoError(t, db.Model(&models.Asset{}).Count(&assets).Error)
	assert.Equal(t, int64(48), assets, "3 站点 × 4 机柜 × (3 服务器 + 1 交换机) = 48")
}

func TestSeed_空DB_创建告警(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var alerts []models.Alert
	require.NoError(t, db.Find(&alerts).Error)
	assert.Equal(t, 6, len(alerts), "应有 6 条告警")

	// severity_name 字段有不同级别
	sevNames := map[string]bool{}
	for _, a := range alerts {
		sevNames[a.SeverityName] = true
	}
	assert.GreaterOrEqual(t, len(sevNames), 4, "至少 4 个不同 severity_name")

	// alert_id/host_name/trigger_name 都填了
	for _, a := range alerts {
		assert.NotEmpty(t, a.AlertID, "AlertID 必填")
		assert.NotEmpty(t, a.HostName, "HostName 必填")
		assert.NotEmpty(t, a.TriggerName, "TriggerName 必填")
		assert.NotEmpty(t, a.Source, "Source 必填")
	}
}

func TestSeed_空DB_创建告警规则(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var rules []models.AlertRule
	require.NoError(t, db.Find(&rules).Error)
	assert.Equal(t, 5, len(rules), "应有 5 条告警规则")
}

func TestSeed_空DB_创建通知渠道(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var channels []models.NotificationChannel
	require.NoError(t, db.Find(&channels).Error)
	assert.Equal(t, 3, len(channels), "应有 3 个通知渠道 (email/dingtalk/wechat)")

	// H-1（测试有效性审计）：只断言 NewSender 成功钉不住可选键 —— NewDingTalkSender
	// 只要求 webhook_url 非空，把 sign_secret 改成 secret 照样构造成功、全绿（变异 S17）。
	// 这里对原始 Config JSON 逐类型断言键名，可选键的键名也被钉住。
	requiredKeys := map[string][]string{
		"email":    {"smtp_host", "smtp_port", "smtp_user", "from", "to"},
		"dingtalk": {"webhook_url", "sign_secret"},
		"wechat":   {"url"},
	}

	byName := map[string]models.NotificationChannel{}
	for _, c := range channels {
		byName[c.Name] = c
		// G-33 M1：seed 用 db.Create 直写，绕过 ChannelService 的配置校验 →
		// 这里独立断言每行都能构造出 Sender（键名/必填项与 channelConfig 对齐）。
		_, err := notification.NewSender(&c)
		assert.NoError(t, err, "seed 渠道 %q(type=%s) 必须能构造出 Sender", c.Name, c.Type)

		var cfg map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(c.Config), &cfg), "seed %q 的 Config 必须是 JSON 对象", c.Name)
		for _, k := range requiredKeys[c.Type] {
			assert.Contains(t, cfg, k, "seed %q 缺少键 %q（键名须与 channelConfig tag 一致）", c.Name, k)
		}
	}
	// 按**渠道名**断言类型（M3 正确性审计 LOW-3）：早先写的是 `assert.False(types["webhook"])`，
	// 那是拿「seed 里不许有 webhook 类型」来表达「企微行不许是 webhook」——将来 seed 加一条
	// 合法的通用 webhook 行会误红，维护者多半直接删断言、连带丢掉 G-36 的钉子。
	assert.Equal(t, "email", byName["邮件通知"].Type)
	assert.Equal(t, "dingtalk", byName["钉钉群通知"].Type)
	assert.Equal(t, "wechat", byName["企业微信通知"].Type,
		"企微行 type 必须是 wechat（WebhookSender 发 {\"content\":…}，企微一律拒收，G-36）")
}

func TestSeed_空DB_创建工单(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var tickets []models.Ticket
	require.NoError(t, db.Find(&tickets).Error)
	assert.Equal(t, 4, len(tickets), "应有 4 个工单")
}

func TestSeed_已有site_跳过site_下游asset不创建(t *testing.T) {
	db := newTestDB(t)
	preSite := models.Site{
		Name:     "已存在的机房",
		Code:     "DC-EXIST-01",
		IsActive: true,
	}
	require.NoError(t, db.Create(&preSite).Error)

	seedData(db)

	var count int64
	db.Model(&models.Site{}).Count(&count)
	assert.Equal(t, int64(1), count, "不应重复 seed 已有 site")

	// 既有 site 时不创建 assets
	var assetCount int64
	db.Model(&models.Asset{}).Count(&assetCount)
	assert.Equal(t, int64(0), assetCount, "跳过 site 分支时不应创建任何 asset")
}

func TestSeed_跑两次_幂等性_users数不变(t *testing.T) {
	db := newTestDB(t)
	seedData(db)
	var count1 int64
	db.Model(&models.User{}).Count(&count1)

	seedData(db)

	var count2 int64
	db.Model(&models.User{}).Count(&count2)
	assert.Equal(t, count1, count2, "seed 跑两次 users 数应不变")
}

func TestSeed_不panic_空DB再跑一次(t *testing.T) {
	db := newTestDB(t)
	assert.NotPanics(t, func() { seedData(db) }, "seed 跑一次不应 panic")
	assert.NotPanics(t, func() { seedData(db) }, "seed 跑两次不应 panic")
}

// TestSeed_演示IP合法且不重复 是缺陷 G-24 的回归测试。
//
// 原实现用 fmt.Sprintf("192.168.%s.10", rack.Row) 拼 IP，而 rack.Row 是机柜排字母
// （A/B）→ 产出 "192.168.A.10" 这种**非法 IPv4**；且同一机房下 A01/A02/A03 三排的
// Row 都是 "A"，9 台服务器 × 3 张网卡会共用同一批地址。两类问题都要钉住：
// ① 每个非空地址必须能被 net.ParseIP 解析（旧拼法直接红在这里）；
// ② 全局不得有重复地址（旧拼法在这里也会红 —— 只修「合法」不修「唯一」挡不住）。
func TestSeed_演示IP合法且不重复(t *testing.T) {
	db := newTestDB(t)
	require.Equal(t, 0, seedData(db), "seedData 不应有任何失败处")

	// 注：sqlite 测试库里 assets.id 恒为零值（GORM 对带 default 标签的零值主键不发列，
	// 而 sqlite 的 TEXT PRIMARY KEY 允许多个 NULL —— 既有测试台特性，与 G-24 无关），
	// 所以这里的归属描述用 interface_name，不用 asset_id。
	byIP := map[string][]string{} // ip → 占用它的网卡名列表

	var nets []models.AssetNetwork
	require.NoError(t, db.Find(&nets).Error)
	require.NotEmpty(t, nets, "seed 必须建出网卡，否则本用例空转")
	nonEmpty := 0
	for _, n := range nets {
		if n.IPv4Address == "" {
			continue // 交换机接入端口按设计无 IP
		}
		nonEmpty++
		ip := net.ParseIP(n.IPv4Address)
		require.NotNil(t, ip, "网卡 %s 的 ipv4_address=%q 不是合法 IPv4（G-24）",
			n.InterfaceName, n.IPv4Address)
		assert.True(t, ip.To4() != nil, "%q 必须是 IPv4 而非 IPv6", n.IPv4Address)
		byIP[n.IPv4Address] = append(byIP[n.IPv4Address], n.InterfaceName)
	}
	require.NotZero(t, nonEmpty, "一个带 IP 的网卡都没有 —— 本用例在空转")
	for ip, owners := range byIP {
		assert.Len(t, owners, 1, "网卡地址 %s 被 %d 张网卡共用：%v", ip, len(owners), owners)
	}
	assert.Len(t, byIP, nonEmpty, "%d 张网卡应产出 %d 个互不相同的地址", nonEmpty, nonEmpty)
	t.Logf("已校验 %d 张网卡的 %d 个唯一地址（%v 三段）", nonEmpty, len(byIP), demoSiteNets)

	// 告警 HostIP 是同一族地址，同样不得是非法字面量（原先写死 192.168.A.10）
	var alerts []models.Alert
	require.NoError(t, db.Find(&alerts).Error)
	require.NotEmpty(t, alerts)
	ips := map[string]bool{}
	for _, a := range alerts {
		ips[a.HostIP] = true
	}
	assert.True(t, ips[""], "交换机那条告警的 HostIP 应保持为空（seed 未给交换机端口配 IP）")
	for _, a := range alerts {
		if a.HostIP == "" {
			continue
		}
		assert.NotNil(t, net.ParseIP(a.HostIP),
			"告警 %s 的 HostIP=%q 不是合法 IPv4（G-24）", a.AlertID, a.HostIP)
	}
}

// TestSeed_演示IP限定在RFC5737文档网段 保证演示地址**不可能**撞上真实设备：
// 192.0.2.0/24、198.51.100.0/24、203.0.113.0/24 被标准保留给示例与文档。
// 只断言「合法」不够 —— 有人把拼法改成 192.168.1.10 一样合法，却可能指向真实资产。
func TestSeed_演示IP限定在RFC5737文档网段(t *testing.T) {
	db := newTestDB(t)
	require.Equal(t, 0, seedData(db))

	prefixes := make([]*net.IPNet, 0, len(demoSiteNets))
	for _, p := range demoSiteNets {
		_, block, err := net.ParseCIDR(p + ".0/24")
		require.NoError(t, err)
		prefixes = append(prefixes, block)
	}
	require.Len(t, prefixes, 3, "RFC 5737 只定义了 3 个文档网段")

	inDemoNet := func(ipStr string) bool {
		ip := net.ParseIP(ipStr)
		for _, b := range prefixes {
			if ip != nil && b.Contains(ip) {
				return true
			}
		}
		return false
	}

	var nets []models.AssetNetwork
	require.NoError(t, db.Find(&nets).Error)
	checked := 0
	for _, n := range nets {
		if n.IPv4Address == "" {
			continue
		}
		assert.True(t, inDemoNet(n.IPv4Address),
			"网卡地址 %s 不在 RFC 5737 文档网段内（演示数据不得使用可路由地址）", n.IPv4Address)
		checked++
	}

	var alerts []models.Alert
	require.NoError(t, db.Find(&alerts).Error)
	for _, a := range alerts {
		if a.HostIP == "" {
			continue
		}
		assert.True(t, inDemoNet(a.HostIP),
			"告警 %s 的 HostIP=%s 不在 RFC 5737 文档网段内", a.AlertID, a.HostIP)
		checked++
	}
	assert.Greater(t, checked, 0, "一个地址都没校验到 —— 本用例在空转")
}

func TestSeed_生成MAC格式正确(t *testing.T) {
	mac := generateMAC()
	// 形如 XX:XX:XX:XX:XX:XX（6 段 16 进制）
	assert.Regexp(t, `^[0-9A-F]{2}(:[0-9A-F]{2}){5}$`, mac)
}
