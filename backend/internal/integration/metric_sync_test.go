// metric_sync_test.go: Zabbix 兜底 worker + SyncMetricsFromZabbix 单测 (v2.3)。
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/models"
)

// initSchema 建测试 DB（含 assets + metric_snapshots）。
// sqlite 不支持 uuid / gen_random_uuid()，走 raw SQL（与 diagnostic/topology handler test 同模式）。
func initSchema(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.New().String())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, s := range []string{
		// assets: 只用到 id + name（SyncMetricsFromZabbix 的 WHERE name IN ?）
		`CREATE TABLE assets (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			asset_tag TEXT, sn TEXT,
			asset_type TEXT, brand TEXT, model TEXT,
			site_id TEXT, site_name TEXT, rack_id TEXT, rack_name TEXT, rack_position TEXT,
			purchase_date DATETIME, warranty_end DATETIME, vendor TEXT, vendor_contact TEXT,
			status TEXT DEFAULT 'active', online_time DATETIME, offline_time DATETIME,
			business_unit TEXT, service_name TEXT, tags TEXT, custom_fields TEXT,
			net_box_id INTEGER, source TEXT, created_at DATETIME, updated_at DATETIME
		)`,
	} {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	// metric_snapshots 走标准 init.sql
	schema, err := os.ReadFile("../api/testdata/metric_snapshot/init.sql")
	if err != nil {
		t.Fatalf("read metric schema: %v", err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatalf("apply metric schema: %v", err)
	}
	return db
}

// fakeZabbix 模拟 Zabbix API：首次 login，返固定 item.get 结果。
// 用 itemsFn 让 caller 控制 item.get 的 response。
type fakeZabbix struct {
	calls        int32
	loginCount   int32
	itemGetCount int32
	auth         string
	itemsFn      func() []Item // 控制 item.get 返回
}

func newFakeZabbix(items []Item) *fakeZabbix {
	return &fakeZabbix{
		auth: "fake-auth-token",
		itemsFn: func() []Item {
			return items
		},
	}
}

func (f *fakeZabbix) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.calls, 1)
		body := make([]byte, 8192)
		n, _ := r.Body.Read(body)
		bodyStr := string(body[:n])

		// login
		if strings.Contains(bodyStr, `"user.login"`) {
			atomic.AddInt32(&f.loginCount, 1)
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"result":  f.auth,
				"id":      1,
			})
			return
		}

		// item.get
		if strings.Contains(bodyStr, `"item.get"`) {
			atomic.AddInt32(&f.itemGetCount, 1)
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"result":  f.itemsFn(),
				"id":      10,
			})
			return
		}

		// 未知方法 → 返 error 触发 caller 失败
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"error":   map[string]any{"code": -1, "message": "unknown method"},
			"id":      99,
		})
	})
}

func newZabbixWithFake(t *testing.T, f *fakeZabbix) (*ZabbixClient, func()) {
	srv := httptest.NewServer(f.handler())
	cfg := &config.ZabbixConfig{
		URL:      srv.URL,
		User:     "Admin",
		Password: "zabbix",
	}
	z := NewZabbixClient(cfg, nil)
	return z, srv.Close
}

// ==================== SyncMetricsFromZabbix 单测 ====================

// TestSyncMetricsFromZabbix_未配置返零 — URL 空 → 0, nil，零调用
func TestSyncMetricsFromZabbix_未配置返零(t *testing.T) {
	db := initSchema(t)
	z := NewZabbixClient(&config.ZabbixConfig{URL: "", User: "", Password: ""}, nil)
	n, err := SyncMetricsFromZabbix(context.Background(), z, db, 1000)
	if err != nil {
		t.Fatalf("want nil err got %v", err)
	}
	if n != 0 {
		t.Fatalf("want 0 inserted got %d", n)
	}
}

// TestSyncMetricsFromZabbix_正常写入 — host 名匹配 → 全部写入
func TestSyncMetricsFromZabbix_正常写入(t *testing.T) {
	db := initSchema(t)
	// 1) 准备 2 个 assets
	a1 := models.Asset{ID: uuid.New(), Name: "host-01"}
	a2 := models.Asset{ID: uuid.New(), Name: "host-02"}
	if err := db.Create(&a1).Error; err != nil {
		t.Fatalf("create a1: %v", err)
	}
	if err := db.Create(&a2).Error; err != nil {
		t.Fatalf("create a2: %v", err)
	}

	// 2) Zabbix 返回 3 个 item，2 个 host 匹配，1 个数值类型不匹配被跳过
	fz := newFakeZabbix([]Item{
		{ItemID: "i1", Key: "cpu.user", LastValue: "45.2",
			Hosts: []Host{{HostID: "h1", Host: "host-01"}}},
		{ItemID: "i2", Key: "mem.used", LastValue: "1024",
			Hosts: []Host{{HostID: "h2", Host: "host-02"}}},
		{ItemID: "i3", Key: "disk.io", LastValue: "abc-not-a-number",
			Hosts: []Host{{HostID: "h1", Host: "host-01"}}}, // ParseFloat 失败 → skipped
	})
	z, cleanup := newZabbixWithFake(t, fz)
	defer cleanup()

	n, err := SyncMetricsFromZabbix(context.Background(), z, db, 1000)
	if err != nil {
		t.Fatalf("sync err: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 inserted got %d", n)
	}

	// 验证：DB 里 2 条 metric_snapshots
	var snaps []models.MetricSnapshot
	if err := db.Find(&snaps).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("want 2 rows in DB got %d", len(snaps))
	}
	// 验证：login + item.get 各调 1 次
	if atomic.LoadInt32(&fz.loginCount) != 1 {
		t.Fatalf("want 1 login call got %d", fz.loginCount)
	}
	if atomic.LoadInt32(&fz.itemGetCount) != 1 {
		t.Fatalf("want 1 item.get call got %d", fz.itemGetCount)
	}
}

// TestSyncMetricsFromZabbix_Host不匹配跳过 — host 名不在 assets 表 → 0 written
func TestSyncMetricsFromZabbix_Host不匹配跳过(t *testing.T) {
	db := initSchema(t)
	// 没建任何 asset

	fz := newFakeZabbix([]Item{
		{ItemID: "i1", Key: "cpu.user", LastValue: "45.2",
			Hosts: []Host{{HostID: "h1", Host: "orphan-host"}}},
	})
	z, cleanup := newZabbixWithFake(t, fz)
	defer cleanup()

	n, err := SyncMetricsFromZabbix(context.Background(), z, db, 1000)
	if err != nil {
		t.Fatalf("sync err: %v", err)
	}
	if n != 0 {
		t.Fatalf("want 0 (host unmatched) got %d", n)
	}
}

// TestSyncMetricsFromZabbix_ItemsEmpty — item.get 返空 → 0, nil
func TestSyncMetricsFromZabbix_ItemsEmpty(t *testing.T) {
	db := initSchema(t)
	fz := newFakeZabbix([]Item{})
	z, cleanup := newZabbixWithFake(t, fz)
	defer cleanup()

	n, err := SyncMetricsFromZabbix(context.Background(), z, db, 1000)
	if err != nil {
		t.Fatalf("sync err: %v", err)
	}
	if n != 0 {
		t.Fatalf("want 0 got %d", n)
	}
}

// TestSyncMetricsFromZabbix_LastValue空跳过 — LastValue 空字符串 → skipped
func TestSyncMetricsFromZabbix_LastValue空跳过(t *testing.T) {
	db := initSchema(t)
	a1 := models.Asset{ID: uuid.New(), Name: "host-01"}
	if err := db.Create(&a1).Error; err != nil {
		t.Fatalf("create a1: %v", err)
	}

	fz := newFakeZabbix([]Item{
		{ItemID: "i1", Key: "cpu.user", LastValue: "",
			Hosts: []Host{{HostID: "h1", Host: "host-01"}}}, // 空值 → skipped
	})
	z, cleanup := newZabbixWithFake(t, fz)
	defer cleanup()

	n, err := SyncMetricsFromZabbix(context.Background(), z, db, 1000)
	if err != nil {
		t.Fatalf("sync err: %v", err)
	}
	if n != 0 {
		t.Fatalf("want 0 (empty lastvalue) got %d", n)
	}
}

// TestSyncMetricsFromZabbix_无Hosts字段跳过 — Hosts 字段空 → skipped
func TestSyncMetricsFromZabbix_无Hosts字段跳过(t *testing.T) {
	db := initSchema(t)
	a1 := models.Asset{ID: uuid.New(), Name: "host-01"}
	if err := db.Create(&a1).Error; err != nil {
		t.Fatalf("create a1: %v", err)
	}

	fz := newFakeZabbix([]Item{
		{ItemID: "i1", Key: "cpu.user", LastValue: "45.2", Hosts: nil}, // 无 host
	})
	z, cleanup := newZabbixWithFake(t, fz)
	defer cleanup()

	n, err := SyncMetricsFromZabbix(context.Background(), z, db, 1000)
	if err != nil {
		t.Fatalf("sync err: %v", err)
	}
	if n != 0 {
		t.Fatalf("want 0 (no hosts) got %d", n)
	}
}

// ==================== MetricSyncWorker 生命周期 ====================

// TestMetricSyncWorker_StartStop幂等 — Start 重复调用 no-op；Stop 后再 Stop no-op
func TestMetricSyncWorker_StartStop幂等(t *testing.T) {
	db := initSchema(t)
	a1 := models.Asset{ID: uuid.New(), Name: "host-01"}
	if err := db.Create(&a1).Error; err != nil {
		t.Fatalf("create a1: %v", err)
	}

	fz := newFakeZabbix([]Item{
		{ItemID: "i1", Key: "cpu.user", LastValue: "45.2",
			Hosts: []Host{{HostID: "h1", Host: "host-01"}}},
	})
	z, cleanup := newZabbixWithFake(t, fz)
	defer cleanup()

	svc := NewIntegrationService(&config.Config{}, nil)
	svc.zabbix = z // 直接注入 fake 客户端（构造 zabbix 字段未导出，仅同包可写）

	w := NewMetricSyncWorker(svc, db, MetricSyncConfig{Tick: 50 * time.Millisecond})

	w.Start(context.Background())
	w.Start(context.Background()) // 二次 Start 必 no-op，不应 panic 不应泄漏 goroutine

	// 等 2 个 tick（确保触发过同步）
	time.Sleep(150 * time.Millisecond)

	w.Stop()
	w.Stop() // 二次 Stop 不 panic

	// 验证：DB 有写入（说明 tick 至少触发过一次）
	var count int64
	db.Model(&models.MetricSnapshot{}).Count(&count)
	if count == 0 {
		t.Fatalf("want at least 1 row after worker ticks, got 0")
	}
}

// TestMetricSyncWorker_Stop未启动 — 没 Start 直接 Stop 不 panic
func TestMetricSyncWorker_Stop未启动(t *testing.T) {
	db := initSchema(t)
	svc := NewIntegrationService(&config.Config{}, nil)
	w := NewMetricSyncWorker(svc, db, MetricSyncConfig{Tick: 1 * time.Second})

	// 不 Start
	w.Stop() // 不应 panic
}

// TestMetricSyncWorker_CtxCancel — ctx cancel 后 worker 退出
func TestMetricSyncWorker_CtxCancel(t *testing.T) {
	db := initSchema(t)
	a1 := models.Asset{ID: uuid.New(), Name: "host-01"}
	db.Create(&a1)

	fz := newFakeZabbix([]Item{
		{ItemID: "i1", Key: "cpu.user", LastValue: "45.2",
			Hosts: []Host{{HostID: "h1", Host: "host-01"}}},
	})
	z, cleanup := newZabbixWithFake(t, fz)
	defer cleanup()

	svc := NewIntegrationService(&config.Config{}, nil)
	svc.zabbix = z

	w := NewMetricSyncWorker(svc, db, MetricSyncConfig{Tick: 30 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)
	time.Sleep(50 * time.Millisecond) // 至少跑 1 tick
	cancel()
	w.Stop() // ctx cancel + Stop 双重保护，wg.Wait 应立即返
}
