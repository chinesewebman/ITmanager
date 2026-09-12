// Package integration - alert_rule_mapper 单测 (M38-B Round 8)
//
// 测试目标：
//   - hit：triggerid 存在映射 + source 匹配 → 返回 RuleID
//   - miss：triggerid 不存在映射 → 返回 nil, no err
//   - rule-deleted：FK ON DELETE CASCADE 已生效 → 不返回旧 RuleID
//   - source 过滤：同 triggerid 不同 source 视为独立行
//
// 测试策略：
//   - 用 SQLite in-memory + 手动建表（不开 GORM AutoMigrate，避开外键约束差异）
//   - 注入真实 AlertRuleMapper，用 SqlxDB 走 SQL 真查
//   - 删 rule 时直接 delete, FK CASCADE 应自动清映射表

package integration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupMapperDB 建一张 alert_rules + alert_rule_trigger_map 表 + 注入 GORM DB
// 返回 gorm.DB 指针 (给 mapper 用) + cleanup 函数
func setupMapperDB(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath+"?_pragma=foreign_keys(1)"), &gorm.Config{})
	require.NoError(t, err)
	// 手工建表, 不依赖 AutoMigrate, 避免和迁移文件漂移
	require.NoError(t, db.Exec(`
		CREATE TABLE alert_rules (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT ''
		)
	`).Error)
	require.NoError(t, db.Exec(`
		CREATE TABLE alert_rule_trigger_map (
			id TEXT PRIMARY KEY,
			rule_id TEXT NOT NULL,
			triggerid TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'zabbix',
			host_id TEXT,
			created_at DATETIME,
			UNIQUE(triggerid, source),
			FOREIGN KEY(rule_id) REFERENCES alert_rules(id) ON DELETE CASCADE
		)
	`).Error)
	return db, func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	}
}

// insertRule helper
func insertRule(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	require.NoError(t, db.Exec(`INSERT INTO alert_rules(id, name) VALUES (?, ?)`, id, "test-rule").Error)
}

// insertMapping helper
func insertMapping(t *testing.T, db *gorm.DB, mappingID, ruleID, triggerid, source string) {
	t.Helper()
	require.NoError(t, db.Exec(
		`INSERT INTO alert_rule_trigger_map(id, rule_id, triggerid, source, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		mappingID, ruleID, triggerid, source,
	).Error)
}

func TestAlertRuleMapper_Lookup_Hit(t *testing.T) {
	db, cleanup := setupMapperDB(t)
	defer cleanup()

	ruleID := uuid.NewString()
	insertRule(t, db, ruleID)
	mappingID := uuid.NewString()
	insertMapping(t, db, mappingID, ruleID, "trig-001", "zabbix")

	mapper := NewAlertRuleMapper(db)
	got, err := mapper.LookupRuleIDByTriggerID(context.Background(), "trig-001", "zabbix")
	require.NoError(t, err)
	require.NotNil(t, got, "hit expected: 映射存在")
	require.Equal(t, ruleID, got.String(), "rule_id must match")
}

func TestAlertRuleMapper_Lookup_Miss(t *testing.T) {
	db, cleanup := setupMapperDB(t)
	defer cleanup()

	mapper := NewAlertRuleMapper(db)
	got, err := mapper.LookupRuleIDByTriggerID(context.Background(), "trig-missing", "zabbix")
	require.NoError(t, err)
	require.Nil(t, got, "miss expected: 映射不存在 → 返回 nil 不是空 UUID")
}

func TestAlertRuleMapper_Lookup_RuleDeleted(t *testing.T) {
	db, cleanup := setupMapperDB(t)
	defer cleanup()

	// 显式开启 SQLite 外键 (默认关闭) — pragma 串也设了, 但 Belt+Braces 写一次
	sqlDB, err := db.DB()
	require.NoError(t, err)
	_, err = sqlDB.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	ruleID := uuid.NewString()
	insertRule(t, db, ruleID)
	mappingID := uuid.NewString()
	insertMapping(t, db, mappingID, ruleID, "trig-002", "zabbix")

	// 删 rule, FK CASCADE 应自动清映射
	require.NoError(t, db.Exec(`DELETE FROM alert_rules WHERE id = ?`, ruleID).Error)

	// 验证映射表也被清掉
	var n int64
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM alert_rule_trigger_map WHERE rule_id = ?`, ruleID).Scan(&n).Error)
	require.Equal(t, int64(0), n, "FK ON DELETE CASCADE 应清空映射行")

	mapper := NewAlertRuleMapper(db)
	got, err := mapper.LookupRuleIDByTriggerID(context.Background(), "trig-002", "zabbix")
	require.NoError(t, err)
	require.Nil(t, got, "rule 已删, 映射被 CASCADE 清掉 → 返回 nil")
}

func TestAlertRuleMapper_Lookup_SourceIsolation(t *testing.T) {
	// 同 triggerid 不同 source 视为独立行 (UNIQUE(triggerid, source) 设计)
	db, cleanup := setupMapperDB(t)
	defer cleanup()

	ruleZ := uuid.NewString()
	ruleM := uuid.NewString()
	insertRule(t, db, ruleZ)
	insertRule(t, db, ruleM)
	insertMapping(t, db, uuid.NewString(), ruleZ, "shared-trig", "zabbix")
	insertMapping(t, db, uuid.NewString(), ruleM, "shared-trig", "manual")

	mapper := NewAlertRuleMapper(db)

	gotZ, err := mapper.LookupRuleIDByTriggerID(context.Background(), "shared-trig", "zabbix")
	require.NoError(t, err)
	require.NotNil(t, gotZ)
	require.Equal(t, ruleZ, gotZ.String())

	gotM, err := mapper.LookupRuleIDByTriggerID(context.Background(), "shared-trig", "manual")
	require.NoError(t, err)
	require.NotNil(t, gotM)
	require.Equal(t, ruleM, gotM.String())

	gotDefault, err := mapper.LookupRuleIDByTriggerID(context.Background(), "shared-trig", "")
	require.NoError(t, err)
	require.NotNil(t, gotDefault, "source='' 不加 source 过滤, 任意一行命中 → 返回非 nil")
}
