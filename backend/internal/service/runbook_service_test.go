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

// newRunbookTestDB 跑测试用 sqlite :memory: + 手写 schema（避开 gorm AutoMigrate 的 gen_random_uuid()）
// 唯一 db 名（cache=shared）以便测试间隔离
func newRunbookTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.New().String())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	schema, err := os.ReadFile("../api/testdata/runbook/init.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return db
}

// seedRunbook 直写 sqlite（绕开 gorm v2 bool=false 被吞的 bug）
func seedRunbook(t *testing.T, db *gorm.DB, rb models.Runbook) models.Runbook {
	t.Helper()
	if rb.ID == uuid.Nil {
		rb.ID = uuid.New()
	}
	now := time.Now()
	if rb.CreatedAt.IsZero() {
		rb.CreatedAt = now
	}
	if rb.UpdatedAt.IsZero() {
		rb.UpdatedAt = now
	}
	// map[string]any 写 false 也进去
	err := db.Table("runbooks").Create(map[string]any{
		"id":         rb.ID.String(),
		"title":      rb.Title,
		"asset_type": rb.AssetType,
		"summary":    rb.Summary,
		"content_md": rb.ContentMD,
		"steps":      rb.Steps,
		"tags":       rb.Tags,
		"severity":   rb.Severity,
		"enabled":    rb.Enabled,
		"created_at": rb.CreatedAt,
		"updated_at": rb.UpdatedAt,
	}).Error
	if err != nil {
		t.Fatalf("seed runbook: %v", err)
	}
	return rb
}

func TestRunbookService_Create_正常返成功(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	rb := &models.Runbook{Title: "DB 故障", AssetType: "server", Severity: 4, Enabled: true}
	if err := svc.Create(context.Background(), rb); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if rb.ID == uuid.Nil {
		t.Fatal("ID 未生成")
	}
	if rb.CreatedAt.IsZero() {
		t.Fatal("CreatedAt 未设置")
	}
}

func TestRunbookService_Create_空title返ErrInvalidInput(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	rb := &models.Runbook{Title: "  ", AssetType: "server"}
	err := svc.Create(context.Background(), rb)
	if err == nil {
		t.Fatal("expected error")
	}
	if !isInvalidInput(err) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
}

func TestRunbookService_Create_空assetType返ErrInvalidInput(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	rb := &models.Runbook{Title: "x", AssetType: ""}
	err := svc.Create(context.Background(), rb)
	if !isInvalidInput(err) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
}

func TestRunbookService_Create_非法的stepsJSON返ErrInvalidInput(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	rb := &models.Runbook{Title: "x", AssetType: "server", Steps: "not-json"}
	if err := svc.Create(context.Background(), rb); !isInvalidInput(err) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
}

func TestRunbookService_Get_已存在返单条(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	rb := &models.Runbook{Title: "x", AssetType: "server"}
	_ = svc.Create(context.Background(), rb)
	got, err := svc.Get(context.Background(), rb.ID.String())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "x" {
		t.Fatalf("want x got %s", got.Title)
	}
}

func TestRunbookService_Get_不存在返ErrNotFound(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	_, err := svc.Get(context.Background(), uuid.New().String())
	if !isNotFound(err) {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}

func TestRunbookService_Update_正常返成功(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	rb := &models.Runbook{Title: "old", AssetType: "server", Severity: 3}
	_ = svc.Create(context.Background(), rb)
	rb.Title = "new"
	rb.Severity = 5
	if err := svc.Update(context.Background(), rb); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := svc.Get(context.Background(), rb.ID.String())
	if got.Title != "new" || got.Severity != 5 {
		t.Fatalf("update not applied: %+v", got)
	}
}

func TestRunbookService_Update_空title返ErrInvalidInput(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	rb := &models.Runbook{Title: "x", AssetType: "server"}
	_ = svc.Create(context.Background(), rb)
	rb.Title = ""
	if err := svc.Update(context.Background(), rb); !isInvalidInput(err) {
		t.Fatalf("want ErrInvalidInput got %v", err)
	}
}

func TestRunbookService_Update_不存在返ErrNotFound(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	rb := &models.Runbook{Title: "x", AssetType: "server"}
	rb.ID = uuid.New()
	if err := svc.Update(context.Background(), rb); !isNotFound(err) {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}

func TestRunbookService_Delete_存在返成功(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	rb := &models.Runbook{Title: "x", AssetType: "server"}
	_ = svc.Create(context.Background(), rb)
	if err := svc.Delete(context.Background(), rb.ID.String()); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestRunbookService_Delete_不存在返ErrNotFound(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	if err := svc.Delete(context.Background(), uuid.New().String()); !isNotFound(err) {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}

func TestRunbookService_List_过滤生效(t *testing.T) {
	db := newRunbookTestDB(t)
	svc := NewRunbookService(db)
	ctx := context.Background()

	seedRunbook(t, db, models.Runbook{Title: "DB 慢查询", AssetType: "server", Severity: 4, Tags: "db,perf", Enabled: true})
	seedRunbook(t, db, models.Runbook{Title: "网络丢包", AssetType: "switch", Severity: 5, Tags: "network", Enabled: true})
	seedRunbook(t, db, models.Runbook{Title: "风扇故障", AssetType: "server", Severity: 3, Tags: "hw", Enabled: false})

	// 按 asset_type 过滤
	items, total, _ := svc.List(ctx, RunbookListOptions{AssetType: "server"})
	if total != 2 || len(items) != 2 {
		t.Fatalf("server filter want 2 got %d", total)
	}

	// 按 severity 过滤（severity=5 → 1 条）
	items, total, _ = svc.List(ctx, RunbookListOptions{Severity: 5})
	if total != 1 {
		t.Fatalf("severity filter want 1 got %d", total)
	}

	// 按 enabled=false 过滤（仅"风扇故障"）
	dis := false
	items, total, _ = svc.List(ctx, RunbookListOptions{Enabled: &dis})
	if total != 1 {
		t.Fatalf("enabled=false want 1 got %d", total)
	}

	// 关键词
	items, total, _ = svc.List(ctx, RunbookListOptions{Keyword: "丢包"})
	if total != 1 || items[0].Title != "网络丢包" {
		t.Fatalf("keyword filter fail")
	}
}

func TestRunbookService_List_分页limit和offset(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_ = svc.Create(ctx, &models.Runbook{Title: "x", AssetType: "server"})
	}
	items, total, _ := svc.List(ctx, RunbookListOptions{Limit: 2, Offset: 0})
	if total != 5 || len(items) != 2 {
		t.Fatalf("limit 2 want 2/5 got %d/%d", len(items), total)
	}
	items, _, _ = svc.List(ctx, RunbookListOptions{Limit: 2, Offset: 4})
	if len(items) != 1 {
		t.Fatalf("offset 4 limit 2 want 1 got %d", len(items))
	}
}

func TestRunbookService_ListForAssetTypeAndSeverity_只返enabled(t *testing.T) {
	db := newRunbookTestDB(t)
	svc := NewRunbookService(db)
	ctx := context.Background()

	seedRunbook(t, db, models.Runbook{Title: "a", AssetType: "server", Severity: 4, Enabled: true})
	seedRunbook(t, db, models.Runbook{Title: "b", AssetType: "server", Severity: 4, Enabled: false})
	seedRunbook(t, db, models.Runbook{Title: "c", AssetType: "switch", Severity: 4, Enabled: true})

	items, _ := svc.ListForAssetTypeAndSeverity(ctx, "server", 4)
	if len(items) != 1 || items[0].Title != "a" {
		t.Fatalf("want 1 item 'a', got %d: %+v", len(items), items)
	}
}

func TestRunbookService_ListForAssetTypeAndSeverity_不指定severity返severity降序(t *testing.T) {
	db := newRunbookTestDB(t)
	svc := NewRunbookService(db)
	ctx := context.Background()

	seedRunbook(t, db, models.Runbook{Title: "low", AssetType: "server", Severity: 1, Enabled: true})
	seedRunbook(t, db, models.Runbook{Title: "high", AssetType: "server", Severity: 5, Enabled: true})
	seedRunbook(t, db, models.Runbook{Title: "all", AssetType: "server", Severity: 0, Enabled: true})

	items, _ := svc.ListForAssetTypeAndSeverity(ctx, "server", 3) // severity=3 → 匹配 severity=3 OR severity=0
	// low=1, high=5 不匹配; all=0 匹配 → 期望 1 条
	if len(items) != 1 || items[0].Title != "all" {
		t.Fatalf("want 1 (severity=0 即 'all'), got %d: %+v", len(items), items)
	}
}

func TestRunbookService_List_特殊keyword含百分号(t *testing.T) {
	svc := NewRunbookService(newRunbookTestDB(t))
	ctx := context.Background()
	_ = svc.Create(ctx, &models.Runbook{Title: "cpu 100%", AssetType: "server"})
	_ = svc.Create(ctx, &models.Runbook{Title: "mem leak", AssetType: "server"})

	items, total, _ := svc.List(ctx, RunbookListOptions{Keyword: "100%"})
	if total != 1 || items[0].Title != "cpu 100%" {
		t.Fatalf("keyword '100%%' want 1, got %d: %+v", total, items)
	}
}

// 复用 service 包的 sentinel
func isInvalidInput(e error) bool {
	return e != nil && (e == ErrInvalidInput || errorContains(e.Error(), ErrInvalidInput.Error()))
}
func isNotFound(e error) bool {
	return e != nil && (e == ErrNotFound || errorContains(e.Error(), ErrNotFound.Error()))
}
func errorContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
