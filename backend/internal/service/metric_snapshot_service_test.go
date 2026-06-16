package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"network-monitor-platform/internal/models"
)

func newMetricTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.New().String())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	schema, err := os.ReadFile("../api/testdata/metric_snapshot/init.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return db
}

func TestMetricSnapshot_BulkInsert_正常返成功(t *testing.T) {
	svc := NewMetricSnapshotService(newMetricTestDB(t))
	snaps := []models.MetricSnapshot{
		{AssetID: uuid.New(), Key: "cpu.user", Value: 45.0, TS: time.Now()},
		{AssetID: uuid.New(), Key: "cpu.user", Value: 50.0, TS: time.Now()},
	}
	if err := svc.BulkInsert(context.Background(), snaps); err != nil {
		t.Fatalf("insert: %v", err)
	}
	for i := range snaps {
		if snaps[i].ID == uuid.Nil {
			t.Fatalf("snap[%d] ID 未生成", i)
		}
	}
}

func TestMetricSnapshot_BulkInsert_空返ErrInvalidInput(t *testing.T) {
	svc := NewMetricSnapshotService(newMetricTestDB(t))
	if err := svc.BulkInsert(context.Background(), nil); !isInvalidInput(err) {
		t.Fatalf("want ErrInvalidInput got %v", err)
	}
}

func TestMetricSnapshot_BulkInsert_超1000返ErrTooManyItems(t *testing.T) {
	svc := NewMetricSnapshotService(newMetricTestDB(t))
	snaps := make([]models.MetricSnapshot, 1001)
	for i := range snaps {
		snaps[i] = models.MetricSnapshot{AssetID: uuid.New(), Key: "x", Value: 1, TS: time.Now()}
	}
	if err := svc.BulkInsert(context.Background(), snaps); !isTooMany(err) {
		t.Fatalf("want ErrTooManyItems got %v", err)
	}
}

func TestMetricSnapshot_Query_按assetId过滤(t *testing.T) {
	db := newMetricTestDB(t)
	svc := NewMetricSnapshotService(db)
	assetA := uuid.New()
	assetB := uuid.New()
	now := time.Now().UTC()
	insertMetrics(t, db, []models.MetricSnapshot{
		{AssetID: assetA, Key: "cpu.user", Value: 10, TS: now},
		{AssetID: assetA, Key: "cpu.user", Value: 20, TS: now.Add(time.Minute)},
		{AssetID: assetB, Key: "cpu.user", Value: 30, TS: now},
	})
	items, err := svc.Query(context.Background(), QueryFilter{AssetID: assetA.String()})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 got %d", len(items))
	}
}

func TestMetricSnapshot_Query_按时间范围过滤(t *testing.T) {
	db := newMetricTestDB(t)
	svc := NewMetricSnapshotService(db)
	now := time.Now().UTC()
	insertMetrics(t, db, []models.MetricSnapshot{
		{AssetID: uuid.New(), Key: "k", Value: 1, TS: now.Add(-2 * time.Hour)},
		{AssetID: uuid.New(), Key: "k", Value: 2, TS: now.Add(-1 * time.Hour)},
		{AssetID: uuid.New(), Key: "k", Value: 3, TS: now},
	})
	items, _ := svc.Query(context.Background(), QueryFilter{From: now.Add(-90 * time.Minute), To: now.Add(1 * time.Minute)})
	if len(items) != 2 {
		t.Fatalf("want 2 (中间 + 当前), got %d", len(items))
	}
}

func TestMetricSnapshot_Query_按key过滤(t *testing.T) {
	db := newMetricTestDB(t)
	svc := NewMetricSnapshotService(db)
	now := time.Now().UTC()
	insertMetrics(t, db, []models.MetricSnapshot{
		{AssetID: uuid.New(), Key: "cpu", Value: 1, TS: now},
		{AssetID: uuid.New(), Key: "mem", Value: 2, TS: now},
	})
	items, _ := svc.Query(context.Background(), QueryFilter{Key: "cpu"})
	if len(items) != 1 {
		t.Fatalf("want 1 got %d", len(items))
	}
}

func TestMetricSnapshot_Latest_返时间倒序的n个点(t *testing.T) {
	db := newMetricTestDB(t)
	svc := NewMetricSnapshotService(db)
	assetID := uuid.New()
	now := time.Now().UTC()
	insertMetrics(t, db, []models.MetricSnapshot{
		{AssetID: assetID, Key: "cpu", Value: 1, TS: now.Add(-3 * time.Hour)},
		{AssetID: assetID, Key: "cpu", Value: 2, TS: now.Add(-2 * time.Hour)},
		{AssetID: assetID, Key: "cpu", Value: 3, TS: now.Add(-1 * time.Hour)},
		{AssetID: uuid.New(), Key: "cpu", Value: 4, TS: now}, // 不同 asset
	})
	items, _ := svc.LatestByAssetAndKey(context.Background(), assetID.String(), "cpu", 2)
	if len(items) != 2 || items[0].Value != 3 {
		t.Fatalf("want 2 latest, got %d: %+v", len(items), items)
	}
}

func TestMetricSnapshot_Latest_缺参数返ErrInvalidInput(t *testing.T) {
	svc := NewMetricSnapshotService(newMetricTestDB(t))
	if _, err := svc.LatestByAssetAndKey(context.Background(), "", "k", 10); !isInvalidInput(err) {
		t.Fatalf("want ErrInvalidInput got %v", err)
	}
	if _, err := svc.LatestByAssetAndKey(context.Background(), "a", "", 10); !isInvalidInput(err) {
		t.Fatalf("want ErrInvalidInput got %v", err)
	}
}

func TestMetricSnapshot_Query_默认limit1000(t *testing.T) {
	db := newMetricTestDB(t)
	svc := NewMetricSnapshotService(db)
	now := time.Now().UTC()
	snaps := make([]models.MetricSnapshot, 1500)
	for i := range snaps {
		snaps[i] = models.MetricSnapshot{AssetID: uuid.New(), Key: "k", Value: float64(i), TS: now}
	}
	insertMetrics(t, db, snaps)
	items, _ := svc.Query(context.Background(), QueryFilter{})
	if len(items) != 1000 {
		t.Fatalf("want default 1000, got %d", len(items))
	}
}

func insertMetrics(t *testing.T, db *gorm.DB, snaps []models.MetricSnapshot) {
	t.Helper()
	now := time.Now()
	for i := range snaps {
		if snaps[i].ID == uuid.Nil {
			snaps[i].ID = uuid.New()
		}
		if snaps[i].CreatedAt.IsZero() {
			snaps[i].CreatedAt = now
		}
	}
	if err := db.Table("metric_snapshots").Create(&snaps).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func isTooMany(e error) bool {
	return e != nil && (e == ErrTooManyItems || errorContains(e.Error(), ErrTooManyItems.Error()))
}
