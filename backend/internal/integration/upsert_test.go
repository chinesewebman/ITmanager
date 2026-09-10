package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/models"
)

// 本文件守 TODO G-22：外部系统同步的 upsert 全链路。
// 方案与证据见 docs/FIX-PLAN-NETBOX-UPSERT.md（列名 / excluded / 唯一仲裁索引三层根因）。

// init 注册带 gen_random_uuid() 的 sqlite 驱动。
// models 的 uuid 主键声明为 `default:gen_random_uuid()`，gorm 因此把 id 从 INSERT 列表里
// 剔除、交给 DB 生成（schema/field.go 的 skipParseDefaultValue）—— sqlite 没有这个函数，
// 测试库必须自己提供，否则插入即报 "no such function: gen_random_uuid"。
// 同 cmd/seed/main_test.go:19-27 的做法（各自进程内注册，不会撞名）。
func init() {
	sql.Register("sqlite3_uuid", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("gen_random_uuid", func() string {
				return uuid.New().String()
			}, true)
		},
	})
}

// upsertTestSchema 手写 DDL，不用 AutoMigrate：
//   - AutoMigrate 会把 `default:gen_random_uuid()` 渲染成 sqlite 的 `DEFAULT gen_random_uuid()`，
//     sqlite 的函数默认值必须带括号（`DEFAULT (gen_random_uuid())`）→ 建表直接语法错；
//   - 列名必须与生产迁移一致（net_box_id / asset_type / …），否则测不出「Go 字段名当列名」。
//
// 三个 ON CONFLICT 目标的索引形态**刻意与生产一致**：
//   - assets.net_box_id 有唯一索引（000015 建的，upsert 的仲裁者）；
//   - alerts.trigger_id / tickets.external_id **没有**唯一索引 —— 生产也没有（000013 只加列），
//     且同一 trigger 的多行历史是预期语义。这样「把 ON CONFLICT 加回来」会立刻报
//     "does not match any PRIMARY KEY or UNIQUE constraint"（与 PG 42P10 同源）。
const upsertTestSchema = `
CREATE TABLE assets (
    id TEXT PRIMARY KEY DEFAULT (gen_random_uuid()),
    name TEXT NOT NULL, asset_tag TEXT, sn TEXT, asset_type TEXT, brand TEXT, model TEXT,
    site_id TEXT, site_name TEXT, rack_id TEXT, rack_name TEXT, rack_position TEXT,
    purchase_date DATETIME, warranty_end DATETIME, vendor TEXT, vendor_contact TEXT,
    status TEXT, online_time DATETIME, offline_time DATETIME,
    last_known_ip4 TEXT, last_known_ip6 TEXT, retired_at DATETIME, retired_reason TEXT,
    retired_by TEXT, business_unit TEXT, service_name TEXT, tags TEXT, custom_fields TEXT,
    net_box_id INTEGER, source TEXT, created_at DATETIME, updated_at DATETIME
);
CREATE UNIQUE INDEX idx_assets_net_box_id ON assets(net_box_id);

CREATE TABLE alerts (
    id TEXT PRIMARY KEY DEFAULT (gen_random_uuid()),
    alert_id TEXT, host_id TEXT, host_name TEXT, host_ip TEXT,
    trigger_name TEXT, trigger_id TEXT, severity INTEGER, severity_name TEXT,
    problem TEXT, problem_start DATETIME, problem_end DATETIME, duration INTEGER,
    status TEXT, ack_time DATETIME, ack_user TEXT, resolve_time DATETIME, resolve_user TEXT,
    is_false_positive BOOLEAN DEFAULT false, marked_by TEXT, marked_at DATETIME,
    false_positive_note TEXT, ticket_id TEXT, asset_id TEXT, source TEXT,
    repeat_count INTEGER, created_at DATETIME, updated_at DATETIME
);

CREATE TABLE tickets (
    id TEXT PRIMARY KEY,
    ticket_number TEXT UNIQUE, title TEXT NOT NULL, description TEXT, ticket_type TEXT,
    priority TEXT, status TEXT, requester_id TEXT, requester_name TEXT, requester_email TEXT,
    assignee_id TEXT, assignee_name TEXT, category TEXT, tags TEXT,
    asset_id TEXT, asset_name TEXT, external_id TEXT, source TEXT,
    resolution TEXT, resolved_at DATETIME, closed_at DATETIME, due_date DATETIME,
    created_at DATETIME, updated_at DATETIME
);
`

// newUpsertTestDB 建内存库并注入 database.DB —— SyncFromXxx 直接读这个全局变量
// （service.go 里 database.DB.WithContext(...)，没有可注入的句柄）。
// 用 file:<uuid>?mode=memory&cache=shared 而不是裸 :memory:：连接池里多条连接必须看到同一个库
// （同 internal/integration/metric_sync_test.go:28）。
func newUpsertTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.NewString())
	db, err := gorm.Open(sqlite.Dialector{DriverName: "sqlite3_uuid", DSN: dsn}, &gorm.Config{})
	require.NoError(t, err, "打开内存 sqlite 失败")
	require.NoError(t, db.Exec(upsertTestSchema).Error, "建测试表失败")

	old := database.GetDB()
	database.SetDBForTest(db)
	t.Cleanup(func() { database.SetDBForTest(old) })
	return db
}

// TestBuildUpsertClause_必须用DB列名 是「Go 字段名当列名」这一根因的**确定性守卫**。
//
// 为什么不能只靠 sqlite 功能测试：sqlite 的列名解析大小写不敏感，`SET "Name"=EXCLUDED.Name`
// 在 sqlite 上**成功执行**（实测），只有真 PG 才报 42703。断言渲染出的 SQL 字符串则跨方言确定。
func TestBuildUpsertClause_必须用DB列名(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	dry := db.Session(&gorm.Session{DryRun: true})

	// 断言的就是调用点真正用的那份清单（service.go 的 netboxUpdateCols）。
	// 不再另抄一份：抄一份的话，调用点被改成 Go 字段名/加回被排除的列，本用例照样绿（审计 F-D）。
	require.NotEmpty(t, netboxUpdateCols, "更新列清单不得为空（空清单会退化成 DO NOTHING，同步等于不更新）")
	require.Equal(t, []string{"name", "asset_type", "brand", "model", "sn", "site_name", "updated_at"},
		netboxUpdateCols, "更新列清单被改动 —— 加/删列都要先想清楚是否覆盖本地人工数据")
	cols := netboxUpdateCols

	stmt := dry.Clauses(buildUpsertClause("net_box_id", cols...)).Create(&models.Asset{})
	sqlText := stmt.Statement.SQL.String()

	assert.Contains(t, sqlText, "ON CONFLICT (`net_box_id`) DO UPDATE SET",
		"冲突目标必须是 DB 列名 net_box_id（不是 Go 字段名/JSON tag netbox_id）")
	for _, col := range cols {
		assert.Contains(t, sqlText, fmt.Sprintf("`%s`=`excluded`.`%s`", col, col),
			"SET 列必须是 DB 列名 %q（clause.AssignmentColumns 的形态）", col)
	}
	assert.NotContains(t, sqlText, "EXCLUDED.", "不得手拼 EXCLUDED.<col>（不带引号，且列名不映射）")
	assert.NotContains(t, sqlText, "`UpdatedAt`", "Go 字段名不得出现在 SQL 里")
	// 以下三列被**刻意排除**在更新列之外（会覆盖/清空本地人工数据），逐个钉住
	assert.NotContains(t, sqlText, "`status`=", "status 不得出现在更新列（会覆盖本地退役状态）")
	assert.NotContains(t, sqlText, "`tags`=", "tags 不得出现在更新列（会抹掉人工标签）")
	assert.NotContains(t, sqlText, "`custom_fields`=", "custom_fields 不得出现在更新列（会抹掉人工字段）")
	assert.NotContains(t, sqlText, "`rack_name`=", "rack_name 不得出现在更新列（ConvertToAsset 不映射机柜，更新等于清空）")

	// 空更新列 → DO NOTHING。gorm 对空 DoUpdates 会渲染 `SET "id"="id"`，
	// 在 PG 的 ON CONFLICT DO UPDATE 里 id 同时存在于目标表与 excluded → 42702 ambiguous（实测）。
	empty := dry.Clauses(buildUpsertClause("net_box_id")).Create(&models.Asset{})
	emptySQL := empty.Statement.SQL.String()
	assert.Contains(t, emptySQL, "DO NOTHING", "空更新列必须是 DO NOTHING")
	assert.NotContains(t, emptySQL, "`id`=`id`", "空更新列不得退化成 `SET id=id`（PG 42702）")
}

// intPtr 取 *int（models.Asset.NetBoxID 是指针，NULL 表示手工资产）。
func intPtr(v int) *int { return &v }

// netboxDevicesJSON 拼 NetBox /api/dcim/devices 的响应体。
func netboxDevicesJSON(t *testing.T, devices ...map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"count": len(devices), "results": devices})
	require.NoError(t, err)
	return string(b)
}

func netboxDevice(id int, name, roleSlug, model, sn string) map[string]any {
	return map[string]any{
		"id":              id,
		"name":            name,
		"device_type":     map[string]any{"slug": "cisco", "model": model},
		"device_role":     map[string]any{"slug": roleSlug, "name": roleSlug},
		"site":            map[string]any{"id": 1, "slug": "dc1", "name": "DC1"},
		"serial_number":   sn,
		"primary_ip":      nil,
		"primary_ip4":     nil,
		"rack":            nil,
		"asset_tag":       nil,
		"custom_fields":   nil,
		"local_context":   nil,
		"interfaces":      nil,
		"device_bay":      nil,
		"platform":        nil,
		"manufacturer":    nil,
		"virtual_chassis": nil,
	}
}

// TestSyncFromNetBox_冲突时更新且保留ID 走**真实调用点**（SyncFromNetBox → 真 SQL），
// 守住「冲突目标列名 + 唯一仲裁索引 + 更新列清单」三件事。
//
// 删除「预查询已存在 + 回填 ID」后，更新完全依赖 ON CONFLICT (net_box_id)：
// 若冲突目标写错，sqlite 会报 no such column；若索引不是唯一，会报
// "does not match any PRIMARY KEY or UNIQUE constraint"（生产即 PG 42P10）。
func TestSyncFromNetBox_冲突时更新且保留ID(t *testing.T) {
	db := newUpsertTestDB(t)

	var payload string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	svc := &IntegrationService{netbox: NewNetBoxClient(&config.NetboxConfig{URL: srv.URL, Token: "t"}, nil)}

	// 第一轮：两台设备 → 全部新增
	payload = netboxDevicesJSON(t,
		netboxDevice(1, "sw01", "switch", "Catalyst 9300", "SN-001"),
		netboxDevice(2, "srv01", "server", "PowerEdge R750", "SN-002"),
	)
	n, err := svc.SyncFromNetBox(context.Background())
	require.NoError(t, err, "首次同步失败 —— 冲突目标列名或唯一索引有问题")
	require.Equal(t, 2, n)

	var first models.Asset
	require.NoError(t, db.Where("net_box_id = ?", 1).First(&first).Error)
	assert.Equal(t, "sw01", first.Name)
	assert.Equal(t, "network", first.AssetType, "device_role.slug=switch → asset_type=network")
	assert.Equal(t, "cisco", first.Brand)
	assert.Equal(t, "SN-001", first.SN)
	assert.NotEqual(t, uuid.Nil, first.ID, "id 必须由 DB 生成（gorm 省略了零值 uuid）")

	// 本地先退役 net_box_id=1（模拟运维在 ITmanager 里退役；NetBox 侧不管退役）
	require.NoError(t, db.Model(&models.Asset{}).Where("net_box_id = ?", 1).
		Update("status", "retired").Error)

	// 第二轮：同一 net_box_id=1 改 name / role / 序列号 / 机房；net_box_id=2 原样
	// 注意 Status 恒为 "active"（netbox.go 的 ConvertToAsset 硬编码），不能用它验证更新。
	moved := netboxDevice(1, "sw01-renamed", "server", "Catalyst 9300", "SN-999")
	moved["site"] = map[string]any{"id": 2, "slug": "dc2", "name": "DC2"}
	payload = netboxDevicesJSON(t, moved,
		netboxDevice(2, "srv01", "server", "PowerEdge R750", "SN-002"),
	)
	n, err = svc.SyncFromNetBox(context.Background())
	require.NoError(t, err, "二次同步（冲突更新）失败")
	require.Equal(t, 2, n)

	var total int64
	require.NoError(t, db.Model(&models.Asset{}).Count(&total).Error)
	assert.Equal(t, int64(2), total, "同 net_box_id 必须更新而非新增")

	var updated models.Asset
	require.NoError(t, db.Where("net_box_id = ?", 1).First(&updated).Error)
	assert.Equal(t, "sw01-renamed", updated.Name, "冲突行的 name 必须被更新")
	assert.Equal(t, "server", updated.AssetType, "冲突行的 asset_type 必须被更新")
	assert.Equal(t, "SN-999", updated.SN, "冲突行的 sn 必须被更新")
	assert.Equal(t, "DC2", updated.SiteName, "冲突行的 site_name 必须跟着 NetBox 走")
	assert.Equal(t, first.ID, updated.ID, "upsert 不得改写已有行的 id")
	// status 不在更新列：NetBox 侧写的是常量 active（零信息量），更新它会把本地退役状态抹掉
	assert.Equal(t, "retired", updated.Status, "同步不得覆盖本地 status（否则退役资产被静默改回 active）")

	// 手工资产（net_box_id 为 NULL）不受同步影响
	manual := models.Asset{Name: "manual-asset", AssetType: "server", Source: "manual"}
	require.NoError(t, db.Create(&manual).Error)
	payload = netboxDevicesJSON(t, netboxDevice(1, "sw01-renamed", "server", "Catalyst 9300", "SN-999"))
	_, err = svc.SyncFromNetBox(context.Background())
	require.NoError(t, err, "存在 NULL net_box_id 的手工资产时同步失败（唯一索引不该约束 NULL）")
	require.NoError(t, db.First(&models.Asset{}, "id = ?", manual.ID).Error, "手工资产被误删/误改")
}

// TestSyncFromNetBox_混合批次 守「同一批里既有冲突行又有新行」（审计 F-H）。
// 单批内 DO UPDATE 与 INSERT 混跑时，行数、冲突行 id、新行 id 都必须正确。
func TestSyncFromNetBox_混合批次(t *testing.T) {
	db := newUpsertTestDB(t)

	existing := models.Asset{Name: "old-name", AssetType: "server", Source: "netbox", NetBoxID: intPtr(1)}
	require.NoError(t, db.Create(&existing).Error)

	var payload string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	svc := &IntegrationService{netbox: NewNetBoxClient(&config.NetboxConfig{URL: srv.URL, Token: "t"}, nil)}
	payload = netboxDevicesJSON(t,
		netboxDevice(1, "sw01-renamed", "switch", "Catalyst 9300", "SN-001"), // 冲突行
		netboxDevice(2, "srv01", "server", "PowerEdge R750", "SN-002"),       // 新行
	)

	n, err := svc.SyncFromNetBox(context.Background())
	require.NoError(t, err, "混合批次（1 冲突 + 1 新增）失败")
	require.Equal(t, 2, n)

	var total int64
	require.NoError(t, db.Model(&models.Asset{}).Count(&total).Error)
	assert.Equal(t, int64(2), total, "冲突行应更新、新行应插入，不得多出行")

	var conflict models.Asset
	require.NoError(t, db.Where("net_box_id = ?", 1).First(&conflict).Error)
	assert.Equal(t, existing.ID, conflict.ID, "冲突行 id 必须保留")
	assert.Equal(t, "sw01-renamed", conflict.Name)
	assert.Equal(t, "network", conflict.AssetType)

	var inserted models.Asset
	require.NoError(t, db.Where("net_box_id = ?", 2).First(&inserted).Error)
	assert.Equal(t, "srv01", inserted.Name)
	assert.NotEqual(t, uuid.Nil, inserted.ID, "新行 id 必须由 DB 生成")
}

// TestSyncFromNetBox_同批重复ID去重 守 service.go 的批次内去重（审计 F-5）：
// 同一批出现两个相同 net_box_id 时，PG 会因 DO UPDATE 二次命中同一行报 21000。
func TestSyncFromNetBox_同批重复ID去重(t *testing.T) {
	db := newUpsertTestDB(t)

	var payload string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	svc := &IntegrationService{netbox: NewNetBoxClient(&config.NetboxConfig{URL: srv.URL, Token: "t"}, nil)}
	payload = netboxDevicesJSON(t,
		netboxDevice(7, "dup-first", "switch", "Catalyst 9300", "SN-A"),
		netboxDevice(7, "dup-second", "switch", "Catalyst 9300", "SN-B"),
	)

	n, err := svc.SyncFromNetBox(context.Background())
	require.NoError(t, err, "同批重复 id 必须被去重，而不是让整条 upsert 失败")
	assert.Equal(t, 1, n, "重复 id 只保留一条")

	var total int64
	require.NoError(t, db.Model(&models.Asset{}).Count(&total).Error)
	assert.Equal(t, int64(1), total)
}

// TestSyncFromNetBox_空列表与错误 覆盖两条早退路径（审计 F-H：SyncFromNetBox 语句覆盖 <80%）。
func TestSyncFromNetBox_空列表与错误(t *testing.T) {
	newUpsertTestDB(t)

	var payload string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	svc := &IntegrationService{netbox: NewNetBoxClient(&config.NetboxConfig{URL: srv.URL, Token: "t"}, nil)}

	// 空列表 → (0, nil)：不写库
	payload = `{"count":0,"results":[]}`
	n, err := svc.SyncFromNetBox(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n)

	// NetBox 报错 → 必须把错误传出去，不能静默当空列表（否则同步失败无人知）
	status = http.StatusBadRequest
	payload = `{"detail":"bad request"}`
	_, err = svc.SyncFromNetBox(context.Background())
	require.Error(t, err, "NetBox 4xx 必须报错")
	assert.Contains(t, err.Error(), "400", "错误里应带状态码: %v", err)
}

// TestSyncFromZabbix_保留历史且不重复 守「Zabbix 路径不生成 ON CONFLICT」。
//
// 反证方式：alerts.trigger_id 在生产与测试 DDL 里都**没有**唯一索引。若把
// buildUpsertClause("trigger_id", ...) 加回来，第一次同步就会报
// "does not match any PRIMARY KEY or UNIQUE constraint"（生产即 PG 42P10）。
//
// 为什么要预置一行 resolved 历史：预过滤只跳过 status='problem' 的，预置历史行才能
// 真正走到 INSERT 分支（否则第二次同步因预过滤根本不执行 INSERT，断言恒真 = 空转）。
func TestSyncFromZabbix_保留历史且不重复(t *testing.T) {
	db := newUpsertTestDB(t)

	// 历史行：同一 trigger 已经 resolved 过一轮
	history := models.Alert{
		TriggerID: "100", TriggerName: "CPU > 90%", HostName: "web-01",
		Status: "resolved", Source: "zabbix", Severity: 5,
	}
	require.NoError(t, db.Create(&history).Error)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		bodyStr := string(body[:n])
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(bodyStr, `"user.login"`):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"tok-1","id":1}`))
		case strings.Contains(bodyStr, `"trigger.get"`):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[
				{"triggerid":"100","description":"CPU > 90%","priority":5,"hosts":[{"hostid":"1","host":"web-01"}],"value":"1"}
			],"id":2}`))
		default:
			t.Errorf("未预期的 Zabbix 请求: %s", bodyStr)
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()

	svc := &IntegrationService{zabbix: NewZabbixClient(&config.ZabbixConfig{URL: srv.URL, User: "admin", Password: "p"}, nil)}

	// 第一次：预过滤只命中 problem 行 → 历史行不算「已存在」，必须插入一条新的 problem 行
	n, err := svc.SyncFromZabbix(context.Background())
	require.NoError(t, err, "首次同步失败 —— 加回了 ON CONFLICT？alerts.trigger_id 没有唯一索引")
	require.Equal(t, 1, n)

	var rows []models.Alert
	require.NoError(t, db.Where("trigger_id = ?", "100").Order("created_at").Find(&rows).Error)
	require.Len(t, rows, 2, "历史行必须保留 + 新增一条 problem 行")
	assert.Equal(t, "resolved", rows[0].Status, "历史行不得被覆盖")
	assert.Equal(t, "problem", rows[1].Status)

	// 第二次：新插入的 problem 行进了预过滤 → 不再插入
	n, err = svc.SyncFromZabbix(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "同 trigger 的未恢复告警已存在，不应重复插入")

	var after int64
	require.NoError(t, db.Model(&models.Alert{}).Where("trigger_id = ?", "100").Count(&after).Error)
	assert.Equal(t, int64(2), after, "同步两次后仍应是 2 行")
}

// TestSyncFromZabbix_本地已确认的告警会重复插入 把**已知边界**钉住（TODO G-27）。
//
// 预过滤只认 status='problem'，本地已 ack 的行（status='acknowledged'）不算「已存在」
// → 同一 trigger 再插一行。这是 G-22 轮刻意不改的语义决策（改法见 G-27），
// 此断言的作用是：谁改了预过滤语义，必须一起改这里，而不是让它悄悄漂移。
func TestSyncFromZabbix_本地已确认的告警会重复插入(t *testing.T) {
	db := newUpsertTestDB(t)

	acked := models.Alert{
		TriggerID: "100", TriggerName: "CPU > 90%", HostName: "web-01",
		Status: "acknowledged", Source: "zabbix", Severity: 5,
	}
	require.NoError(t, db.Create(&acked).Error)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		bodyStr := string(body[:n])
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(bodyStr, `"user.login"`):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"tok-1","id":1}`))
		case strings.Contains(bodyStr, `"trigger.get"`):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[
				{"triggerid":"100","description":"CPU > 90%","priority":5,"hosts":[{"hostid":"1","host":"web-01"}],"value":"1"}
			],"id":2}`))
		default:
			t.Errorf("未预期的 Zabbix 请求: %s", bodyStr)
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()

	svc := &IntegrationService{zabbix: NewZabbixClient(&config.ZabbixConfig{URL: srv.URL, User: "admin", Password: "p"}, nil)}

	n, err := svc.SyncFromZabbix(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "当前语义：已 ack 的行不算「已存在」，会再插一行（TODO G-27）")

	var rows []models.Alert
	require.NoError(t, db.Where("trigger_id = ?", "100").Order("created_at").Find(&rows).Error)
	require.Len(t, rows, 2, "已知边界：同一 trigger 出现 ack 行 + problem 行两行（TODO G-27）")
	assert.Equal(t, "acknowledged", rows[0].Status, "ack 行必须原样保留")
	assert.Equal(t, "problem", rows[1].Status)
}

// TestSyncFromGLPI_两次同步不重复 守「GLPI 路径不生成 ON CONFLICT」。
// 原实现生成 `ON CONFLICT (external_id) DO UPDATE SET "id"="id"`：既没有唯一索引仲裁
// （PG 42P10），SET 里的 id 又是 ambiguous（PG 42702）。tickets.external_id 在测试与
// 生产 DDL 里都**没有**唯一索引，加回 ON CONFLICT 即红。
//
// 只喂 1 张工单：本用例守的是 ON CONFLICT 语义，「一次同步多张票」由
// TestSyncFromGLPI_一次同步多张工单不撞号 单独守（G-25，2026-09-10 已修）。
// 两者刻意分开，任一侧红了都能一眼看出是哪条语义破了。
func TestSyncFromGLPI_两次同步不重复(t *testing.T) {
	db := newUpsertTestDB(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/initSession"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"session_token":"sess-1"}`))
		case strings.Contains(r.URL.Path, "/Ticket"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"id":1,"name":"Disk full","content":"disk 100%","status":1,"priority":4,"date":"2026-09-09 10:00"}
			]`))
		default:
			t.Errorf("未预期的 GLPI 请求: %s", r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()

	svc := &IntegrationService{glpi: NewGLPIClient(&config.GLPIConfig{URL: srv.URL, AppToken: "a", UserToken: "u"}, nil)}

	n, err := svc.SyncFromGLPI(context.Background())
	require.NoError(t, err, "首次同步失败 —— 加回了 ON CONFLICT？tickets.external_id 没有唯一索引")
	require.Equal(t, 1, n)

	n, err = svc.SyncFromGLPI(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "已存在的工单应被跳过")

	var total int64
	require.NoError(t, db.Model(&models.Ticket{}).Count(&total).Error)
	assert.Equal(t, int64(1), total, "同步两次后仍应是 1 行")
}

// TestSyncFromGLPI_一次同步多张工单不撞号 是缺陷 G-25 的回归测试。
//
// 原实现直接 CreateInBatches：gorm 的 before_create 回调对整片 slice 的每一行都跑完
// 才进 INSERT，每行各自按「当天条数」算号拿到**同一个值** → 整批同一个 ticket_number
// → 唯一索引整批拒绝 → 首次同步（或任何一次新增 ≥2 张票的同步）全部失败，
// 且 SyncAll 把它记成 glpi 失败。修复：插入前 models.AssignTicketNumbers 预分配。
func TestSyncFromGLPI_一次同步多张工单不撞号(t *testing.T) {
	db := newUpsertTestDB(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/initSession"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"session_token":"sess-1"}`))
		case strings.Contains(r.URL.Path, "/Ticket"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"id":1,"name":"Disk full","content":"disk 100%","status":1,"priority":4,"date":"2026-09-10 10:00"},
				{"id":2,"name":"CPU 飙高","content":"load 40","status":1,"priority":5,"date":"2026-09-10 10:01"},
				{"id":3,"name":"内存不足","content":"oom","status":1,"priority":3,"date":"2026-09-10 10:02"}
			]`))
		default:
			t.Errorf("未预期的 GLPI 请求: %s", r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()

	svc := &IntegrationService{glpi: NewGLPIClient(&config.GLPIConfig{URL: srv.URL, AppToken: "a", UserToken: "u"}, nil)}

	n, err := svc.SyncFromGLPI(context.Background())
	require.NoError(t, err, "一次同步 3 张新工单不得撞号（G-25）")
	require.Equal(t, 3, n)

	var rows []models.Ticket
	require.NoError(t, db.Order("ticket_number").Find(&rows).Error)
	require.Len(t, rows, 3, "3 张工单必须全部入库，不能整批被唯一索引拒掉")

	seen := make(map[string]string, len(rows))
	for _, r := range rows {
		assert.NotEmpty(t, r.TicketNumber, "external_id=%s 未生成工单号", r.ExternalID)
		if first, dup := seen[r.TicketNumber]; dup {
			t.Fatalf("工单号 %s 被 external_id=%s 与 %s 重复占用", r.TicketNumber, first, r.ExternalID)
		}
		seen[r.TicketNumber] = r.ExternalID
	}

	// 第二次同步：3 张都已在库，应全跳过，且不因重算编号而改写
	n, err = svc.SyncFromGLPI(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "已存在的工单应被跳过")

	var after int64
	require.NoError(t, db.Model(&models.Ticket{}).Count(&after).Error)
	assert.Equal(t, int64(3), after, "同步两次后仍应是 3 行")
}
