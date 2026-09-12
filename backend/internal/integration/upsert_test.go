package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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
// ON CONFLICT 目标的索引形态**刻意与生产一致**：
//   - assets.net_box_id 有唯一索引（000015 建的，upsert 的仲裁者）；
//   - alerts 有**部分**唯一索引 uq_alerts_zabbix_identity（000027 建的，M27/A）——
//     注意是 (trigger_id, **problem_start**) 两列，**不是** trigger_id 单列：同一 trigger
//     的多行历史（触发 → 恢复 → 再触发）仍是预期语义，只有「同一次故障发生」才唯一。
//   - tickets.external_id 有**部分**唯一索引（000026 建的，谓词与生产逐字相同）。
//     **方向别搞反**：带**匹配 TargetWhere** 的 ON CONFLICT 可以仲裁；
//     **不带** TargetWhere（或列集/谓词对不上）才会报
//     "ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint"
//     —— 与 PG 42P10 同源。它不是「这一列上没有索引」。
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
-- 000027 的部分唯一索引，谓词与迁移逐字相同（sqlite 支持部分索引）。
-- 少了它，SyncFromZabbix 的 ON CONFLICT (trigger_id, problem_start) WHERE ... 会直接报
-- "ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint"，
-- 本文件的 Zabbix 用例会全红 —— 那是基座缺件，不是被测代码的问题。
CREATE UNIQUE INDEX uq_alerts_zabbix_identity
    ON alerts(trigger_id, problem_start)
    WHERE source = 'zabbix' AND trigger_id IS NOT NULL AND trigger_id <> '';

CREATE TABLE tickets (
    id TEXT PRIMARY KEY,
    ticket_number TEXT UNIQUE, title TEXT NOT NULL, description TEXT, ticket_type TEXT,
    priority TEXT, status TEXT, requester_id TEXT, requester_name TEXT, requester_email TEXT,
    assignee_id TEXT, assignee_name TEXT, category TEXT, tags TEXT,
    asset_id TEXT, asset_name TEXT, external_id TEXT, source TEXT,
    resolution TEXT, resolved_at DATETIME, closed_at DATETIME, due_date DATETIME,
    created_at DATETIME, updated_at DATETIME
);
-- 000026 的部分唯一索引，谓词与迁移逐字相同（sqlite 支持部分索引）。
-- 少了它，SyncFromGLPI 的 ON CONFLICT (external_id) WHERE ... 会直接报
-- "ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint"，
-- 本文件的 GLPI 用例会全红 —— 那是基座缺件，不是被测代码的问题。
CREATE UNIQUE INDEX uq_tickets_glpi_external_id
    ON tickets(external_id) WHERE source = 'glpi' AND external_id <> '';
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
	n, _, err := svc.SyncFromNetBox(context.Background())
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
	n, _, err = svc.SyncFromNetBox(context.Background())
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
	_, _, err = svc.SyncFromNetBox(context.Background())
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

	n, _, err := svc.SyncFromNetBox(context.Background())
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

	n, _, err := svc.SyncFromNetBox(context.Background())
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
	n, _, err := svc.SyncFromNetBox(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n)

	// NetBox 报错 → 必须把错误传出去，不能静默当空列表（否则同步失败无人知）
	status = http.StatusBadRequest
	payload = `{"detail":"bad request"}`
	_, _, err = svc.SyncFromNetBox(context.Background())
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
	n, _, _, err := svc.SyncFromZabbix(context.Background())
	require.NoError(t, err, "首次同步失败 —— 加回了 ON CONFLICT？alerts.trigger_id 没有唯一索引")
	require.Equal(t, 1, n)

	var rows []models.Alert
	require.NoError(t, db.Where("trigger_id = ?", "100").Order("created_at").Find(&rows).Error)
	require.Len(t, rows, 2, "历史行必须保留 + 新增一条 problem 行")
	assert.Equal(t, "resolved", rows[0].Status, "历史行不得被覆盖")
	assert.Equal(t, "problem", rows[1].Status)

	// 第二次：新插入的 problem 行进了预过滤 → 不再插入
	n, _, _, err = svc.SyncFromZabbix(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "同 trigger 的未恢复告警已存在，不应重复插入")

	var after int64
	require.NoError(t, db.Model(&models.Alert{}).Where("trigger_id = ?", "100").Count(&after).Error)
	assert.Equal(t, int64(2), after, "同步两次后仍应是 2 行")
}

// fakeZabbixServer 假 Zabbix：只答 user.login 与 trigger.get。
// lastChange 为空串时**不带** lastchange 字段（模拟 Zabbix 没给）。
// 这是外部 HTTP 依赖的替身，被测代码（SyncFromZabbix 及其下游）走真实路径。
func fakeZabbixServer(t *testing.T, lastChange string) *httptest.Server {
	t.Helper()
	lc := ""
	if lastChange != "" {
		lc = fmt.Sprintf(`,"lastchange":%s`, strconv.Quote(lastChange))
	}
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
				{"triggerid":"100","description":"CPU > 90%","priority":5,
				 "hosts":[{"hostid":"1","host":"web-01"}],"value":"1"` + lc + `}
			],"id":2}`))
		default:
			t.Errorf("未预期的 Zabbix 请求: %s", bodyStr)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSyncFromZabbix_ProblemStart来自LastChange 守 M26 的核心修复（需求 §1.2、§2.3）。
//
// 这个缺陷是**静默的**：problem_start 不写时告警列表照样有数据（列表根本不渲染该列），
// 但所有基于 problem_start 的 KPI / SLA 窗口恒空 —— 从界面上看不出来。
// 所以断言必须落在库里的值上，而不是返回值或日志。
//
// 反证：把 `ProblemStart: problemStart` 改回不写（或写 now），本条必红。
func TestSyncFromZabbix_ProblemStart来自LastChange(t *testing.T) {
	db := newUpsertTestDB(t)

	// 故意取一个远离 now 的固定时刻：若实现误写成 now，比较必然不等。
	lastChange := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	srv := fakeZabbixServer(t, strconv.FormatInt(lastChange.Unix(), 10))

	svc := &IntegrationService{zabbix: NewZabbixClient(&config.ZabbixConfig{URL: srv.URL, User: "admin", Password: "p"}, nil)}
	n, _, _, err := svc.SyncFromZabbix(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	var row models.Alert
	require.NoError(t, db.Where("trigger_id = ?", "100").First(&row).Error)

	assert.True(t, row.ProblemStart.Equal(lastChange),
		"problem_start 必须取 Zabbix 的 lastchange；期望 %v，实际 %v。"+
			"若实际接近当前时间，说明回落到 now 了；若为零值，说明整列没写",
		lastChange, row.ProblemStart)
	assert.Equal(t, "2026-09-01 12:00:00", row.ProblemStart.UTC().Format("2006-01-02 15:04:05"),
		"落库挂钟（lastchange 是 Unix 秒，恒按 UTC 解释）")
}

// TestSyncFromZabbix_LastChange缺失回落 守「回落必须可见」（不静默）。
//
// 回落本身是允许的（lastchange 非必需字段），但必须留下日志 —— 否则运维无法察觉
// 这批告警的 problem_start 是同步时刻而非故障时刻。
//
// 反证：删掉 logTimeUnusable 调用，本条的红在「日志断言」上。
func TestSyncFromZabbix_LastChange缺失回落(t *testing.T) {
	db := newUpsertTestDB(t)

	// 捕获标准库 log 的输出；用例结束后复原。
	var buf bytes.Buffer
	oldOut := log.Default().Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(oldOut) })

	srv := fakeZabbixServer(t, "") // 不带 lastchange

	before := time.Now()
	svc := &IntegrationService{zabbix: NewZabbixClient(&config.ZabbixConfig{URL: srv.URL, User: "admin", Password: "p"}, nil)}
	n, _, _, err := svc.SyncFromZabbix(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	var row models.Alert
	require.NoError(t, db.Where("trigger_id = ?", "100").First(&row).Error)

	// 落库值仍必须非零（回落是 now，不是零值）—— 零值会让 SLA 窗口算不出来。
	assert.False(t, row.ProblemStart.IsZero(), "回落也要写 now，不能留零值")
	assert.WithinDuration(t, before, row.ProblemStart, time.Minute,
		"缺失 lastchange 时 problem_start 合理回落为同步时刻")

	assert.Contains(t, buf.String(), "lastchange",
		"回落必须打日志；静默回落正是 M26 要消除的失败模式。实际日志: %q", buf.String())
}

// TestSyncFromZabbix_本地已确认的告警不再重复插入 是 G-27 的靶心（M27/A 修的正是它）。
//
// 旧语义：预过滤只认 status='problem' → 本地已 ack 的行（status='acknowledged'）不算
// 「已存在」→ 同一 trigger 再插一行，运维每点一次「确认」就多一条待处理告警。
// 新语义：判据是「同一 trigger 的同一次故障发生」（trigger_id + problem_start），
// 与本地状态无关 —— ack 过的故障不该因为被确认而复活成新行。
//
// 本 fixture 的 trigger 不带 lastchange → 走**降级分支**（身份不可知 → 同 trigger 且
// 未解决即算已存在），所以它守的是 D-2/D-3 那条判据，不是 exact 判据。
func TestSyncFromZabbix_本地已确认的告警不再重复插入(t *testing.T) {
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

	n, _, _, err := svc.SyncFromZabbix(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "已 ack 的故障不该因为被确认而复活成新行（G-27）")

	var rows []models.Alert
	require.NoError(t, db.Where("trigger_id = ?", "100").Order("created_at").Find(&rows).Error)
	require.Len(t, rows, 1, "只能有原来的 ack 那一行 —— 多出来的那行就是 G-27 的重复告警")
	assert.Equal(t, "acknowledged", rows[0].Status, "ack 行必须原样保留（不得被覆盖成 problem）")
}

// TestSyncFromGLPI_两次同步不重复 守**预过滤**（existingSet）这条幂等路径。
//
// ⚠️ 它**不守 ON CONFLICT**（M26 起 external_id 已有部分唯一索引，ON CONFLICT 是合法仲裁）。
// 也不该被当成 ON CONFLICT 的防线：第二次同步时预过滤已把全部行剔除 → toUpsert 为空 →
// 在 `if len(toUpsert) == 0` 提前返回，**INSERT 根本不会执行**。所以把 ON CONFLICT 整段删掉，
// 本条依然全绿（假绿，T-47「守门人所在的那一轮根本没执行」的同类：断言没跑，不是没守住）。
// 真正守 ON CONFLICT 的是
// TestSyncFromGLPI_预查后漏进冲突行仍幂等 —— 那条构造了「预查之后才出现冲突行」的交错。
//
// 只喂 1 张工单：「一次同步多张票」由 TestSyncFromGLPI_一次同步多张工单不撞号 单独守
// （G-25，2026-09-10 已修）。两者刻意分开，任一侧红了都能一眼看出是哪条语义破了。
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

	n, _, _, err := svc.SyncFromGLPI(context.Background())
	require.NoError(t, err, "首次同步失败 —— 加回了 ON CONFLICT？tickets.external_id 没有唯一索引")
	require.Equal(t, 1, n)

	n, _, _, err = svc.SyncFromGLPI(context.Background())
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

	n, _, _, err := svc.SyncFromGLPI(context.Background())
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
	n, _, _, err = svc.SyncFromGLPI(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "已存在的工单应被跳过")

	var after int64
	require.NoError(t, db.Model(&models.Ticket{}).Count(&after).Error)
	assert.Equal(t, int64(3), after, "同步两次后仍应是 3 行")
}

// fakeGLPIServer 假 GLPI：只答 initSession 与 Ticket 列表。
// ticketsJSON 是 /Ticket 的响应体原文。外部 HTTP 依赖的替身，被测代码走真实路径。
func fakeGLPIServer(t *testing.T, ticketsJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/initSession"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"session_token":"sess-1"}`))
		case strings.Contains(r.URL.Path, "/Ticket"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(ticketsJSON))
		default:
			t.Errorf("未预期的 GLPI 请求: %s", r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func glpiSvc(srv *httptest.Server) *IntegrationService {
	return &IntegrationService{glpi: NewGLPIClient(&config.GLPIConfig{URL: srv.URL, AppToken: "a", UserToken: "u"}, nil)}
}

// TestSyncFromGLPI_CreatedAt来自GLPI 守 M26：created_at 取 GLPI 的 date，不是同步时刻。
//
// 本用例只管**管道**（glpi.date → ticket.created_at 这一路接上了没有）。
// 时区换算（.UTC()）的一半**不在**这里守：sqlite 会把偏移一起写进字符串再原样还原，
// 因而两种 Location 的写法在 sqlite 上都"对"—— 见 §4 基座注意与 T18（真 PG）。
//
// 反证：把 CreatedAt 改回 now → 本条必红。
func TestSyncFromGLPI_CreatedAt来自GLPI(t *testing.T) {
	db := newUpsertTestDB(t)

	srv := fakeGLPIServer(t, `[{"id":1,"name":"Disk full","content":"x","status":1,"priority":4,"date":"2026-09-09 10:00"}]`)
	svc := glpiSvc(srv)

	n, skipped, _, err := svc.SyncFromGLPI(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, 0, skipped)

	var row models.Ticket
	require.NoError(t, db.Where("external_id = ?", "1").First(&row).Error)
	assert.Equal(t, "2026-09-09 02:00:00", row.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
		"created_at 应取 GLPI 的 date=2026-09-09 10:00 (Asia/Shanghai) → 02:00 UTC；"+
			"若接近当前时间说明回落成了 now")
}

// TestSyncFromGLPI_ClosedAt不发明 守 M26/D-2（P0-2 的唯一防线）。
//
// 源没给 resolvedate/closedate 是**合法状态**（票还没解决/关闭），必须原样留 NULL。
// 回落 now 会伪造事件：diagnostic_service 只看指针非空、不看 status，
// 会凭空长出一条「工单已解决/已关闭」的时间线记录，并污染 dashboard 的 SLA 窗口。
//
// 反证：把 else 分支改成 `nt.ClosedAt = &now` → 本条必红。
func TestSyncFromGLPI_ClosedAt不发明(t *testing.T) {
	db := newUpsertTestDB(t)

	// status=5（已关闭）但两个时间字段都缺失 —— GLPI 允许这种组合
	srv := fakeGLPIServer(t, `[{"id":1,"name":"closed-no-date","content":"x","status":5,"priority":3,"date":"2026-09-01 09:00"}]`)
	svc := glpiSvc(srv)

	n, _, _, err := svc.SyncFromGLPI(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	var row models.Ticket
	require.NoError(t, db.Where("external_id = ?", "1").First(&row).Error)
	assert.Equal(t, "closed", row.Status)
	assert.Nil(t, row.ClosedAt, "源没给 closedate 时必须留 NULL —— 补 now 会伪造「工单已关闭」事件")
	assert.Nil(t, row.ResolvedAt, "源没给 solvedate 时必须留 NULL —— 同上")
}

// TestSyncFromGLPI_ClosedAt有值 守 D-2 的另一半：源给了就必须落库（且值正确）。
// 与上一条成对：只有「该 NULL 的 NULL、该有值的有值」同时成立，才叫不发明也不丢。
func TestSyncFromGLPI_ClosedAt有值(t *testing.T) {
	db := newUpsertTestDB(t)

	srv := fakeGLPIServer(t, `[{"id":1,"name":"closed","content":"x","status":5,"priority":3,
		"date":"2026-09-01 09:00","solvedate":"2026-09-02 11:30","closedate":"2026-09-03 16:45"}]`)
	svc := glpiSvc(srv)

	n, _, _, err := svc.SyncFromGLPI(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	var row models.Ticket
	require.NoError(t, db.Where("external_id = ?", "1").First(&row).Error)
	require.NotNil(t, row.ResolvedAt, "源给了 solvedate 就必须落库")
	require.NotNil(t, row.ClosedAt, "源给了 closedate 就必须落库")
	assert.Equal(t, "2026-09-02 03:30:00", row.ResolvedAt.UTC().Format("2006-01-02 15:04:05"))
	assert.Equal(t, "2026-09-03 08:45:00", row.ClosedAt.UTC().Format("2006-01-02 15:04:05"))
}

// TestSyncFromGLPI_越界跳过并计数 守 M26/D-1。
//
// 越界档位（GLPI 发了词表外的值）既不能兜底成某一档（静默改写事实），
// 也不能整批失败（一张脏票挡住其余全部）。处置是：跳过该票 + 计数 + 留日志。
//
// 反证：把越界分支改成 `continue` 前兜底（如 Status="open"）→ 入库数变了，本条必红。
func TestSyncFromGLPI_越界跳过并计数(t *testing.T) {
	db := newUpsertTestDB(t)

	srv := fakeGLPIServer(t, `[
		{"id":1,"name":"ok","content":"x","status":1,"priority":3,"date":"2026-09-09 10:00"},
		{"id":2,"name":"bad-status","content":"x","status":7,"priority":3,"date":"2026-09-09 10:01"},
		{"id":3,"name":"ok2","content":"x","status":2,"priority":0,"date":"2026-09-09 10:02"}
	]`)
	svc := glpiSvc(srv)

	n, skipped, _, err := svc.SyncFromGLPI(context.Background())
	require.NoError(t, err, "一张越界票不得让整批同步失败")
	assert.Equal(t, 2, n, "id=1 与 id=3 应入库；priority=0 是合法档位（M26/D-1 已补词表）")
	assert.Equal(t, 1, skipped, "status=7 越界，应恰好跳过 1 条")

	var ids []string
	require.NoError(t, db.Model(&models.Ticket{}).Order("external_id").Pluck("external_id", &ids).Error)
	assert.Equal(t, []string{"1", "3"}, ids, "越界的 id=2 不得入库")
}

// TestSyncFromGLPI_预查后漏进冲突行仍幂等 是 ON CONFLICT（D-4）**唯一**的真实防线。
//
// 为什么不能靠「同步两遍」那条用例：第二遍预过滤已全剔除 → INSERT 根本不执行 → 假绿。
// 必须构造「预查之后、插入之前出现冲突行」的交错，也就是 existingSet 预过滤的 TOCTOU ——
// 这正是 D-4 存在的唯一场景。用一个一次性 Query 钩子在预查返回后注入该行。
//
// 断言设计成**混合批次**（1 冲突 + 1 新），这样才能同时钉住两件事：
//   - 删掉 ON CONFLICT → 23505 整批失败 → 红；
//   - 把 synced 换回 res.RowsAffected → 它按 len(batch) 虚报成 2（实测 gorm v1.30 行为）→ 红。
//
// 反证（两条，都必须红）：见上。
func TestSyncFromGLPI_预查后漏进冲突行仍幂等(t *testing.T) {
	db := newUpsertTestDB(t)

	var once sync.Once
	require.NoError(t, db.Callback().Query().After("gorm:query").
		Register("m26:inject-conflict", func(tx *gorm.DB) {
			// 只在**第一次**查 tickets 时注入 —— 那正是 SyncFromGLPI 的预过滤 Find。
			// 之后的查询（事务里的 COUNT）不再注入，避免干扰计数。
			once.Do(func() {
				require.NoError(t, db.Create(&models.Ticket{
					ExternalID: "1", Source: "glpi", Status: "open", Priority: "low",
					TicketNumber: "GLPI-INJECTED", Title: "并发同步已插入",
				}).Error, "注入冲突行失败")
			})
		}))

	srv := fakeGLPIServer(t, `[
		{"id":1,"name":"already-there","content":"x","status":1,"priority":3,"date":"2026-09-09 10:00"},
		{"id":2,"name":"new","content":"x","status":1,"priority":3,"date":"2026-09-09 10:01"}
	]`)
	svc := glpiSvc(srv)

	n, skipped, _, err := svc.SyncFromGLPI(context.Background())
	require.NoError(t, err,
		"预查后漏进的冲突行必须被 ON CONFLICT 幂等跳过；报 23505 说明 ON CONFLICT 没了")
	require.Equal(t, 0, skipped)

	assert.Equal(t, 1, n,
		"只应新增 id=2 那一张。若报 2，说明 synced 用了 res.RowsAffected（按 len(batch) 虚报）")

	var total int64
	require.NoError(t, db.Model(&models.Ticket{}).Count(&total).Error)
	assert.Equal(t, int64(2), total, "注入行 + 新增 1 张 = 2 行，冲突行不得产生重复")
}
