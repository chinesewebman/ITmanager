package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newAuditSQLiteDB 复用 newAssetSQLiteDB 的 schema (asset_networks 列相同).
func newAuditSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	stmts := []string{
		`CREATE TABLE asset_networks (
			id TEXT PRIMARY KEY,
			asset_id TEXT NOT NULL,
			interface_name TEXT NOT NULL,
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
	}
	for _, s := range stmts {
		require.NoError(t, db.Exec(s).Error)
	}
	return db
}

// TestM70_Audit_查跨资产同IP_返所有冲突 — 核心用例.
// 三种数据混合: 同 IP 跨资产冲突 / 同资产多网卡(允许, 不冲突) / 不同 IP 不冲突 / 空串.
func TestM70_Audit_查跨资产同IP_返所有冲突(t *testing.T) {
	db := newAuditSQLiteDB(t)
	svc := NewAssetService(db).(*assetService)

	a1 := uuid.New()
	a2 := uuid.New()
	a3 := uuid.New()
	a4 := uuid.New() // 第四台, 不参与冲突

	// a1 + a2 + a3 共享 10.0.0.5 (3 资产冲突)
	// a1 + a2 共享 2001:db8::1 (2 资产冲突)
	// a1 自己两网卡 (10.0.0.6 + 10.0.0.7) — 同一资产, 不算冲突
	// a4 单独 10.0.0.99 — 不冲突
	networks := []struct {
		ID         string
		AssetID    string
		IPv4       string
		IPv6       string
		Interface  string
	}{
		{uuid.New().String(), a1.String(), "10.0.0.5", "2001:db8::1", "eth0"},
		{uuid.New().String(), a1.String(), "10.0.0.6", "", "eth1"},
		{uuid.New().String(), a1.String(), "10.0.0.7", "", "eth2"},
		{uuid.New().String(), a2.String(), "10.0.0.5", "2001:db8::1", "eth0"},
		{uuid.New().String(), a3.String(), "10.0.0.5", "", "eth0"},
		{uuid.New().String(), a4.String(), "10.0.0.99", "", "eth0"},
		// 空 IPv4 + IPv6 不参与 (守卫隐含语义)
		{uuid.New().String(), a1.String(), "", "", "eth9"},
	}
	for _, n := range networks {
		require.NoError(t, db.Exec(
			`INSERT INTO asset_networks (id, asset_id, interface_name, ipv4_address, ipv6_address) VALUES (?, ?, ?, ?, ?)`,
			n.ID, n.AssetID, n.Interface, n.IPv4, n.IPv6,
		).Error)
	}

	ctx := context.Background()
	rows, err := svc.AuditIPConflicts(ctx)
	require.NoError(t, err)

	// 期望 2 条冲突: 10.0.0.5 (v4, 跨 3 资产) + 2001:db8::1 (v6, 跨 2 资产)
	// 注意同资产多网卡 (10.0.0.6, 10.0.0.7) 各自只 1 行, 不算冲突
	require.Equal(t, 2, len(rows), "expected 2 conflicts, got %d: %+v", len(rows), rows)

	// 排序: v4 count DESC, v6 次之
	assert.Equal(t, "ipv4", rows[0].Kind)
	assert.Equal(t, "10.0.0.5", rows[0].IP)
	assert.Equal(t, 3, rows[0].Count)
	assert.Equal(t, 3, len(rows[0].AssetIDs))
	// assetIDs 经 sort.Strings 排序 —— 直接验证集合相等 (顺序不固定)
	expectedAssetSet := map[string]bool{a1.String(): true, a2.String(): true, a3.String(): true}
	for _, got := range rows[0].AssetIDs {
		assert.True(t, expectedAssetSet[got], "unexpected assetID %s", got)
		delete(expectedAssetSet, got)
	}
	assert.Empty(t, expectedAssetSet, "missing assetIDs")

	assert.Equal(t, "ipv6", rows[1].Kind)
	assert.Equal(t, "2001:db8::1", rows[1].IP)
	assert.Equal(t, 2, rows[1].Count)
	assert.Equal(t, 2, len(rows[1].AssetIDs))
	v6Set := map[string]bool{a1.String(): true, a2.String(): true}
	for _, got := range rows[1].AssetIDs {
		assert.True(t, v6Set[got])
		delete(v6Set, got)
	}
	assert.Empty(t, v6Set)
}

// TestM70_Audit_空表_返空切片
func TestM70_Audit_空表_返空切片(t *testing.T) {
	db := newAuditSQLiteDB(t)
	svc := NewAssetService(db).(*assetService)
	rows, err := svc.AuditIPConflicts(context.Background())
	require.NoError(t, err)
	assert.Empty(t, rows)
}

// TestM70_Audit_全唯一IP_返空切片
func TestM70_Audit_全唯一IP_返空切片(t *testing.T) {
	db := newAuditSQLiteDB(t)
	svc := NewAssetService(db).(*assetService)

	for i := 0; i < 5; i++ {
		id := uuid.New()
		require.NoError(t, db.Exec(
			`INSERT INTO asset_networks (id, asset_id, interface_name, ipv4_address) VALUES (?, ?, ?, ?)`,
			uuid.New().String(), id.String(), "eth0", uuid.New().String(),
		).Error)
	}

	rows, err := svc.AuditIPConflicts(context.Background())
	require.NoError(t, err)
	assert.Empty(t, rows, "all unique IPs, expected no conflicts")
}

// TestM70_Audit_空串不参与 — IPv4 = '' 的行不进 GROUP BY.
func TestM70_Audit_空串不参与(t *testing.T) {
	db := newAuditSQLiteDB(t)
	svc := NewAssetService(db).(*assetService)

	// 三行 IPv4 都是 '' — 不应触发 GROUP BY (HAVING >= 2 在 WHERE '' 排掉之后)
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Exec(
			`INSERT INTO asset_networks (id, asset_id, interface_name, ipv4_address) VALUES (?, ?, ?, '')`,
			uuid.New().String(), uuid.New().String(), "eth0",
		).Error)
	}

	rows, err := svc.AuditIPConflicts(context.Background())
	require.NoError(t, err)
	assert.Empty(t, rows, "empty IP should not trigger conflicts")
}
