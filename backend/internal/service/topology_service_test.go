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

// newTopologyTestDB 建 3 张表：assets / asset_networks / alerts
// 复用 sqlite uuid driver（已在 diagnostic init 注册）
func newTopologyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, s := range []string{
		`CREATE TABLE assets (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, asset_tag TEXT, sn TEXT,
			asset_type TEXT, brand TEXT, model TEXT,
			site_id TEXT, site_name TEXT, rack_id TEXT, rack_name TEXT, rack_position TEXT,
			purchase_date DATETIME, warranty_end DATETIME, vendor TEXT, vendor_contact TEXT,
			status TEXT DEFAULT 'active', online_time DATETIME, offline_time DATETIME,
			business_unit TEXT, service_name TEXT, tags TEXT, custom_fields TEXT,
			net_box_id INTEGER, source TEXT, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE asset_networks (
			id TEXT PRIMARY KEY, asset_id TEXT, interface_name TEXT, interface_type TEXT,
			mac_address TEXT, ipv4_address TEXT, ipv4_netmask TEXT, ipv_address TEXT,
			speed INTEGER, duplex TEXT, status TEXT, connected_to TEXT, connected_port TEXT,
			purpose TEXT, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE alerts (
			id TEXT PRIMARY KEY, alert_id TEXT, host_id TEXT, host_name TEXT, host_ip TEXT,
			trigger_name TEXT, trigger_id TEXT, severity INTEGER, severity_name TEXT,
			problem TEXT, problem_start DATETIME, problem_end DATETIME, duration INTEGER,
			status TEXT DEFAULT 'problem', ack_time DATETIME, ack_user TEXT,
			resolve_time DATETIME, resolve_user TEXT, ticket_id TEXT, asset_id TEXT,
			source TEXT, repeat_count INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
	} {
		require.NoError(t, db.Exec(s).Error)
	}
	return db
}

func seedTopoAsset(t *testing.T, db *gorm.DB, name, assetType string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	require.NoError(t, db.Exec(`INSERT INTO assets
		(id, name, asset_type, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, assetType, "active", now, now).Error)
	return id
}

func seedTopoLink(t *testing.T, db *gorm.DB, assetID uuid.UUID, ifName, status, connectedTo string) {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	require.NoError(t, db.Exec(`INSERT INTO asset_networks
		(id, asset_id, interface_name, status, connected_to, updated_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, assetID, ifName, status, connectedTo, now, now).Error)
}

func seedTopoAlert(t *testing.T, db *gorm.DB, hostID uuid.UUID) {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	require.NoError(t, db.Exec(`INSERT INTO alerts
		(id, host_id, host_name, trigger_name, severity, severity_name, problem,
		 problem_start, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, hostID, "host", "trig", 3, "Warning", "problem",
		now, "problem", now, now).Error)
}

// ==================== TopologyService 测试 ====================

func TestTopologyService_GetTopology_空DB返回空图(t *testing.T) {
	db := newTopologyTestDB(t)
	svc := NewTopologyService(db)

	got, err := svc.GetTopology(context.Background(), TopologyFilter{})
	require.NoError(t, err)
	assert.Equal(t, 0, got.Stats.TotalNodes)
	assert.Equal(t, 0, got.Stats.TotalEdges)
	assert.Empty(t, got.Nodes)
	assert.Empty(t, got.Edges)
	assert.Equal(t, 30, got.Stats.WindowDays, "默认 30 天")
}

func TestTopologyService_GetTopology_3资产2边_形成链路(t *testing.T) {
	db := newTopologyTestDB(t)
	svc := NewTopologyService(db)

	// 3 个资产：switch-core + server-01 + server-02
	switchID := seedTopoAsset(t, db, "switch-core", "switch")
	server1 := seedTopoAsset(t, db, "server-01", "server")
	server2 := seedTopoAsset(t, db, "server-02", "server")
	// 2 条边：server-01 → switch-core, server-02 → switch-core
	seedTopoLink(t, db, server1, "eth0", "up", "switch-core")
	seedTopoLink(t, db, server2, "eth0", "up", "switch-core")

	got, err := svc.GetTopology(context.Background(), TopologyFilter{})
	require.NoError(t, err)
	assert.Equal(t, 3, got.Stats.TotalNodes, "应有 3 个节点")
	assert.Equal(t, 2, got.Stats.TotalEdges, "应有 2 条边")
	assert.Equal(t, 0, got.Stats.VirtualNodes)

	// 边指向的 target 应该是 switch-core 的真实 ID
	for _, e := range got.Edges {
		assert.Equal(t, switchID.String(), e.Target, "边应指向 switch-core")
	}
}

func TestTopologyService_GetTopology_连接外部设备生成虚拟节点(t *testing.T) {
	db := newTopologyTestDB(t)
	svc := NewTopologyService(db)

	serverID := seedTopoAsset(t, db, "server-01", "server")
	// 连接到不存在的 external-router
	seedTopoLink(t, db, serverID, "eth0", "up", "external-router:Gi0/1")

	got, err := svc.GetTopology(context.Background(), TopologyFilter{})
	require.NoError(t, err)
	assert.Equal(t, 2, got.Stats.TotalNodes, "1 真实 + 1 虚拟")
	assert.Equal(t, 1, got.Stats.VirtualNodes)

	// 找到虚拟节点
	var virtual *models.TopologyNode
	for i := range got.Nodes {
		if got.Nodes[i].IsVirtual {
			virtual = &got.Nodes[i]
			break
		}
	}
	require.NotNil(t, virtual)
	assert.Equal(t, "external-router", virtual.Name)
	assert.Equal(t, "external", virtual.Status)
}

func TestTopologyService_GetTopology_告警数统计正确(t *testing.T) {
	db := newTopologyTestDB(t)
	svc := NewTopologyService(db)

	serverID := seedTopoAsset(t, db, "server-01", "server")
	switchID := seedTopoAsset(t, db, "switch-core", "switch")
	// server 有 2 条 open alert，switch 有 1 条
	seedTopoAlert(t, db, serverID)
	seedTopoAlert(t, db, serverID)
	seedTopoAlert(t, db, switchID)

	got, err := svc.GetTopology(context.Background(), TopologyFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), got.Stats.NodesWithAlert)

	// 找每个节点的 OpenAlerts
	for _, n := range got.Nodes {
		if n.Name == "server-01" {
			assert.Equal(t, int64(2), n.OpenAlerts)
		}
		if n.Name == "switch-core" {
			assert.Equal(t, int64(1), n.OpenAlerts)
		}
	}
}

func TestTopologyService_GetTopology_OnlyWithAlerts_只返回有告警的节点(t *testing.T) {
	db := newTopologyTestDB(t)
	svc := NewTopologyService(db)

	healthy := seedTopoAsset(t, db, "healthy-server", "server")
	problemID := seedTopoAsset(t, db, "problem-server", "server")
	seedTopoAlert(t, db, problemID)
	_ = healthy

	got, err := svc.GetTopology(context.Background(), TopologyFilter{OnlyWithAlerts: true})
	require.NoError(t, err)
	assert.Len(t, got.Nodes, 1, "OnlyWithAlerts=true 应过滤掉无告警节点")
	assert.Equal(t, "problem-server", got.Nodes[0].Name)
}

func TestTopologyService_GetTopology_AssetTypes过滤生效(t *testing.T) {
	db := newTopologyTestDB(t)
	svc := NewTopologyService(db)

	seedTopoAsset(t, db, "switch-1", "switch")
	seedTopoAsset(t, db, "server-1", "server")
	seedTopoAsset(t, db, "server-2", "server")

	got, err := svc.GetTopology(context.Background(), TopologyFilter{AssetTypes: []string{"server"}})
	require.NoError(t, err)
	assert.Len(t, got.Nodes, 2, "过滤 switch 类型后只留 2 个 server")
}

func TestTopologyService_GetTopology_节点ID稳定排序(t *testing.T) {
	db := newTopologyTestDB(t)
	svc := NewTopologyService(db)

	idA := seedTopoAsset(t, db, "a-server", "server")
	idB := seedTopoAsset(t, db, "b-server", "server")
	idC := seedTopoAsset(t, db, "c-server", "server")
	_ = idA
	_ = idB
	_ = idC

	got, err := svc.GetTopology(context.Background(), TopologyFilter{})
	require.NoError(t, err)
	require.Len(t, got.Nodes, 3)
	// uuid 字符串排序
	assert.True(t, got.Nodes[0].ID.String() < got.Nodes[1].ID.String())
	assert.True(t, got.Nodes[1].ID.String() < got.Nodes[2].ID.String())
}

func TestTopologyService_GetTopology_节点位置自动布局(t *testing.T) {
	db := newTopologyTestDB(t)
	svc := NewTopologyService(db)

	// 1 个节点
	seedTopoAsset(t, db, "node-1", "server")
	got, err := svc.GetTopology(context.Background(), TopologyFilter{})
	require.NoError(t, err)
	require.Len(t, got.Nodes, 1)
	// 1 个节点时 cos(0)*300 = 300, sin(0)*300 = 0
	assert.InDelta(t, 300.0, got.Nodes[0].PositionX, 0.01)
	assert.InDelta(t, 0.0, got.Nodes[0].PositionY, 0.01)
}

func TestTopologyService_GetTopology_Down边计数(t *testing.T) {
	db := newTopologyTestDB(t)
	svc := NewTopologyService(db)

	a := seedTopoAsset(t, db, "a", "server")
	b := seedTopoAsset(t, db, "b", "server")
	seedTopoLink(t, db, a, "eth0", "up", "b")
	seedTopoLink(t, db, a, "eth1", "down", "b")
	_ = b

	got, err := svc.GetTopology(context.Background(), TopologyFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), got.Stats.DownEdges, "1 条 down 边")
}

func TestTopologyService_GetTopology_ConnectedAssets计数(t *testing.T) {
	db := newTopologyTestDB(t)
	svc := NewTopologyService(db)

	switchID := seedTopoAsset(t, db, "switch", "switch")
	s1 := seedTopoAsset(t, db, "s1", "server")
	s2 := seedTopoAsset(t, db, "s2", "server")
	// 2 个 server 都连到 switch
	seedTopoLink(t, db, s1, "eth0", "up", "switch")
	seedTopoLink(t, db, s2, "eth0", "up", "switch")

	got, err := svc.GetTopology(context.Background(), TopologyFilter{})
	require.NoError(t, err)
	// 找 switch 节点
	for _, n := range got.Nodes {
		if n.Name == "switch" {
			assert.Equal(t, 2, n.ConnectedAssets, "switch 应被 2 个 server 引用")
		}
	}
	_ = switchID
}

func TestTopologyService_GetTopology_参数clamp到365天(t *testing.T) {
	db := newTopologyTestDB(t)
	svc := NewTopologyService(db)

	got, err := svc.GetTopology(context.Background(), TopologyFilter{Days: 99999})
	require.NoError(t, err)
	assert.Equal(t, 365, got.Stats.WindowDays)
}
