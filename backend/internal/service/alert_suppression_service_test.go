package service

import (
	"context"
	"testing"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newSuppressionTestDB 建 alert_suppressions 表（其他表本测试不用）
// driver 注册已在 diagnostic_service_test.go 的 init() 完成
func newSuppressionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE alert_suppressions (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		severity_max INTEGER DEFAULT 3,
		host_pattern TEXT NOT NULL,
		time_window_seconds INTEGER DEFAULT 300,
		ttl_seconds INTEGER DEFAULT 0,
		enabled INTEGER DEFAULT 1,
		description TEXT,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error)
	return db
}

// createRule 辅助：插一条 enabled 规则
func createRule(t *testing.T, db *gorm.DB, name, pattern string, severityMax, windowSec int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	err := db.Exec(`INSERT INTO alert_suppressions
		(id, name, severity_max, host_pattern, time_window_seconds, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		id, name, severityMax, pattern, windowSec, now, now).Error
	require.NoError(t, err)
	return id
}

// ==================== CRUD ====================

func TestAlertSuppression_Create_成功(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)

	rule := &models.AlertSuppression{
		Name:              "test-rule",
		HostPattern:       "*",
		SeverityMax:       3,
		TimeWindowSeconds: 60,
	}
	err := svc.Create(context.Background(), rule)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, rule.ID)
}

func TestAlertSuppression_Create_空name报错(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)

	rule := &models.AlertSuppression{
		HostPattern:       "*",
		TimeWindowSeconds: 60,
	}
	err := svc.Create(context.Background(), rule)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

func TestAlertSuppression_Create_severity超界报错(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)

	rule := &models.AlertSuppression{
		Name:              "bad",
		HostPattern:       "*",
		SeverityMax:       6,
		TimeWindowSeconds: 60,
	}
	err := svc.Create(context.Background(), rule)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "severity_max")
}

func TestAlertSuppression_Create_window为0报错(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)

	rule := &models.AlertSuppression{
		Name: "bad", HostPattern: "*", SeverityMax: 3, TimeWindowSeconds: 0,
	}
	err := svc.Create(context.Background(), rule)
	assert.Error(t, err)
}

func TestAlertSuppression_List_返回所有规则(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	createRule(t, db, "r1", "*", 3, 60)
	createRule(t, db, "r2", "*-prod", 4, 120)

	rules, err := svc.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, rules, 2)
}

func TestAlertSuppression_Get_不存在返ErrNotFound(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	_, err := svc.Get(context.Background(), uuid.New())
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestAlertSuppression_Update_修改severity(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	id := createRule(t, db, "r1", "*", 3, 60)

	rule, err := svc.Update(context.Background(), id, map[string]interface{}{"severity_max": 5})
	require.NoError(t, err)
	assert.Equal(t, 5, rule.SeverityMax)
}

func TestAlertSuppression_Update_空map不报错(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	id := createRule(t, db, "r1", "*", 3, 60)

	rule, err := svc.Update(context.Background(), id, map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "r1", rule.Name)
}

func TestAlertSuppression_Update_severity超界报错(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	id := createRule(t, db, "r1", "*", 3, 60)

	_, err := svc.Update(context.Background(), id, map[string]interface{}{"severity_max": 10})
	assert.Error(t, err)
}

func TestAlertSuppression_Update_改name成功(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	id := createRule(t, db, "原名", "*", 3, 60)

	rule, err := svc.Update(context.Background(), id, map[string]interface{}{"name": "新名"})
	require.NoError(t, err)
	assert.Equal(t, "新名", rule.Name)
}

func TestAlertSuppression_Update_改window和host_pattern(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	id := createRule(t, db, "r1", "*", 3, 60)

	rule, err := svc.Update(context.Background(), id, map[string]interface{}{
		"time_window_seconds": 300,
		"host_pattern":        "web-*",
	})
	require.NoError(t, err)
	assert.Equal(t, 300, rule.TimeWindowSeconds)
	assert.Equal(t, "web-*", rule.HostPattern)
}

func TestAlertSuppression_Update_不存在ID返ErrNotFound(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)

	_, err := svc.Update(context.Background(), uuid.New(), map[string]interface{}{"name": "x"})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestAlertSuppression_Update_window为0报错(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	id := createRule(t, db, "r1", "*", 3, 60)

	_, err := svc.Update(context.Background(), id, map[string]interface{}{"time_window_seconds": 0})
	assert.Error(t, err)
}

func TestAlertSuppression_Delete_成功并清缓存(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	id := createRule(t, db, "r1", "*", 3, 60)

	// 先触发一次把缓存填上
	hostID := uuid.New()
	_, _ = svc.Evaluate(context.Background(), 3, hostID, "host-1")
	svc.mu.RLock()
	_, seen := svc.lastFired[id.String()+"|"+hostID.String()]
	svc.mu.RUnlock()
	assert.True(t, seen, "Evaluate 后应填缓存")

	err := svc.Delete(context.Background(), id)
	require.NoError(t, err)

	// 缓存应清空（只清自己 rule 的 key）
	svc.mu.RLock()
	_, seenAfter := svc.lastFired[id.String()+"|"+hostID.String()]
	svc.mu.RUnlock()
	assert.False(t, seenAfter, "Delete 后缓存应清空")
}

func TestAlertSuppression_Delete_不存在返ErrNotFound(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	err := svc.Delete(context.Background(), uuid.New())
	assert.ErrorIs(t, err, ErrNotFound)
}

// ==================== Evaluate 核心 ====================

func TestAlertSuppression_Evaluate_无规则直接放行(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	res, err := svc.Evaluate(context.Background(), 3, uuid.New(), "host-1")
	require.NoError(t, err)
	assert.False(t, res.Suppressed)
	assert.Contains(t, res.Reason, "无规则")
}

func TestAlertSuppression_Evaluate_severity高于阈值不放行(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	createRule(t, db, "warn-only", "*", 2, 60) // 严重级别 > 2 不抑制

	res, err := svc.Evaluate(context.Background(), 4, uuid.New(), "host-1")
	require.NoError(t, err)
	assert.False(t, res.Suppressed, "severity 4 > 阈值 2，不应被抑制")
}

func TestAlertSuppression_Evaluate_severity符合阈值不匹配host(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	createRule(t, db, "db-only", "db-*", 3, 60)

	res, err := svc.Evaluate(context.Background(), 3, uuid.New(), "web-01")
	require.NoError(t, err)
	assert.False(t, res.Suppressed, "host=web-01 不匹配 db-* 模式")
}

func TestAlertSuppression_Evaluate_首次评估放行并记录(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	createRule(t, db, "r1", "*", 3, 60)

	hostID := uuid.New()
	res, err := svc.Evaluate(context.Background(), 3, hostID, "host-1")
	require.NoError(t, err)
	assert.False(t, res.Suppressed, "首次评估应放行")
	assert.NotNil(t, res.MatchedRule)
	assert.Contains(t, res.Reason, "通过")
}

func TestAlertSuppression_Evaluate_窗口期内抑制(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	ruleID := createRule(t, db, "r1", "*", 3, 60) // 60s 窗口

	hostID := uuid.New()
	// 首次放行
	res1, _ := svc.Evaluate(context.Background(), 3, hostID, "host-1")
	require.False(t, res1.Suppressed)

	// 紧接第二次（窗口内）
	res2, err := svc.Evaluate(context.Background(), 3, hostID, "host-1")
	require.NoError(t, err)
	assert.True(t, res2.Suppressed, "60s 窗口内同 host 应被抑制")
	assert.NotNil(t, res2.WindowExpiresAt)
	assert.Equal(t, ruleID, *res2.MatchedRule)
}

func TestAlertSuppression_Evaluate_不同host各自独立(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	createRule(t, db, "r1", "*", 3, 60)

	hostA := uuid.New()
	hostB := uuid.New()
	_, _ = svc.Evaluate(context.Background(), 3, hostA, "host-a")

	resB, _ := svc.Evaluate(context.Background(), 3, hostB, "host-b")
	assert.False(t, resB.Suppressed, "不同 host 不应相互抑制")
}

func TestAlertSuppression_Evaluate_disabled规则不生效(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	id := createRule(t, db, "r1", "*", 3, 60)
	// 改成 disabled
	require.NoError(t, db.Exec(`UPDATE alert_suppressions SET enabled = 0 WHERE id = ?`, id).Error)

	res, err := svc.Evaluate(context.Background(), 3, uuid.New(), "host-1")
	require.NoError(t, err)
	assert.False(t, res.Suppressed, "disabled 规则不参与抑制")
}

func TestAlertSuppression_Evaluate_TTL过期规则不生效(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	id := createRule(t, db, "r1", "*", 3, 60)
	// 设 ttl=1 秒 + 把 updated_at 改到 10 秒前
	require.NoError(t, db.Exec(`UPDATE alert_suppressions SET ttl_seconds = 1, updated_at = ? WHERE id = ?`,
		time.Now().Add(-10*time.Second).UTC(), id).Error)

	res, err := svc.Evaluate(context.Background(), 3, uuid.New(), "host-1")
	require.NoError(t, err)
	assert.False(t, res.Suppressed, "TTL 过期规则不参与抑制")
}

func TestAlertSuppression_ResetWindow_清空缓存(t *testing.T) {
	db := newSuppressionTestDB(t)
	svc := NewAlertSuppressionService(db)
	createRule(t, db, "r1", "*", 3, 60)

	hostID := uuid.New()
	_, _ = svc.Evaluate(context.Background(), 3, hostID, "host-1")
	svc.ResetWindow()

	// 缓存清空 → 这次不被抑制
	res, _ := svc.Evaluate(context.Background(), 3, hostID, "host-1")
	assert.False(t, res.Suppressed, "ResetWindow 后首次应放行")
}

// ==================== matchHost helper ====================

func TestMatchHost_精确匹配(t *testing.T) {
	assert.True(t, matchHost("host-1", "host-1"))
	assert.False(t, matchHost("host-1", "host-2"))
}

func TestMatchHost_glob匹配(t *testing.T) {
	assert.True(t, matchHost("db-*", "db-01"))
	assert.True(t, matchHost("db-*", "db-01-prod"))
	assert.False(t, matchHost("db-*", "web-01"))
}

func TestMatchHost_空pattern不匹配(t *testing.T) {
	assert.False(t, matchHost("", "anything"))
}

func TestMatchHost_子串退化(t *testing.T) {
	// path.Match 在无效 glob 模式下退化为子串包含
	assert.True(t, matchHost("[invalid", "host-[invalid-foo"))
}
