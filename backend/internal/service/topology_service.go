package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TopologyService 网络拓扑服务（P1-1）
//
// 数据源：
//   - assets: 节点
//   - asset_networks: 边（asset_id → connected_to）
//   - alerts: 节点告警数（status=problem）
//
// 算法：
//  1. 拉所有 assets（窗口期外也保留，节点本身）
//  2. 拉所有 asset_networks（窗口期内 updated_at）
//  3. 拉所有 status=problem alerts（窗口期内）
//  4. 构造节点 / 边 / 统计
//  5. 给每个节点自动布局：环形排布（简单但稳定）
type TopologyService struct {
	db *gorm.DB
}

func NewTopologyService(db *gorm.DB) *TopologyService {
	return &TopologyService{db: db}
}

// TopologyFilter 拓扑查询参数
type TopologyFilter struct {
	// Days 查询窗口（天），0=默认 30，最大 365
	Days int
	// OnlyWithAlerts 只显示有告警的节点（incident focus 模式）
	OnlyWithAlerts bool
	// AssetTypes 过滤资产类型（空 = 全部）
	AssetTypes []string
}

// GetTopology 返回拓扑图
func (s *TopologyService) GetTopology(ctx context.Context, filter TopologyFilter) (*models.TopologyGraph, error) {
	days := filter.Days
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}
	windowStart := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)

	// 1) 节点：所有 assets（按 type 过滤）
	type assetRow struct {
		ID        uuid.UUID
		Name      string
		AssetTag  string
		AssetType string
		Brand     string
		Status    string
	}
	assetQ := s.db.WithContext(ctx).Table("assets").
		Select("id, name, asset_tag, asset_type, brand, status")
	if len(filter.AssetTypes) > 0 {
		assetQ = assetQ.Where("asset_type IN ?", filter.AssetTypes)
	}
	var assetRows []assetRow
	if err := assetQ.Scan(&assetRows).Error; err != nil {
		return nil, err
	}

	// 2) 边：window 期内有更新的 asset_networks
	type netRow struct {
		ID            uuid.UUID
		AssetID       uuid.UUID
		InterfaceName string
		Status        string
		Speed         int
		Purpose       string
		ConnectedTo   string
	}
	var netRows []netRow
	if err := s.db.WithContext(ctx).
		Table("asset_networks").
		Select("id, asset_id, interface_name, status, speed, purpose, connected_to").
		Where("updated_at >= ?", windowStart).
		Scan(&netRows).Error; err != nil {
		return nil, err
	}

	// 3) open alerts: 按 host_id 分组数
	type alertCountRow struct {
		HostID uuid.UUID
		Cnt    int64
	}
	var alertRows []alertCountRow
	if err := s.db.WithContext(ctx).
		Table("alerts").
		Select("host_id, COUNT(*) as cnt").
		Where("status = ? AND problem_start >= ?", "problem", windowStart).
		Group("host_id").
		Scan(&alertRows).Error; err != nil {
		return nil, err
	}
	alertMap := make(map[uuid.UUID]int64, len(alertRows))
	for _, a := range alertRows {
		alertMap[a.HostID] = a.Cnt
	}

	// 4) 构造节点
	assetByName := make(map[string]uuid.UUID, len(assetRows))
	nodes := make([]models.TopologyNode, 0, len(assetRows))
	for i := range assetRows {
		a := &assetRows[i]
		assetByName[a.Name] = a.ID
		openAlerts := alertMap[a.ID]
		// 环形布局：i / N × 2π
		angle := float64(i) / math.Max(float64(len(assetRows)), 1) * 2 * math.Pi
		nodes = append(nodes, models.TopologyNode{
			ID:         a.ID,
			Name:       a.Name,
			AssetTag:   a.AssetTag,
			AssetType:  a.AssetType,
			Brand:      a.Brand,
			Status:     a.Status,
			OpenAlerts: openAlerts,
			IsVirtual:  false,
			PositionX:  math.Cos(angle) * 300,
			PositionY:  math.Sin(angle) * 300,
		})
	}

	// 5) 构造边 + 虚拟节点
	virtualNodes := make(map[string]*models.TopologyNode)
	edges := make([]models.TopologyEdge, 0, len(netRows))
	connectedCount := make(map[string]int, len(assetRows))
	var downEdges int64
	for _, n := range netRows {
		targetID, _ := s.resolveTarget(n.ConnectedTo, assetByName, virtualNodes)
		if n.Status == "down" {
			downEdges++
		}
		// 用 string key 避免 uuid.MustParse panic
		connectedCount[n.AssetID.String()]++
		if targetID != "" {
			connectedCount[targetID]++
		}
		edges = append(edges, models.TopologyEdge{
			ID:            n.ID,
			Source:        n.AssetID.String(),
			Target:        targetID,
			InterfaceName: n.InterfaceName,
			Status:        n.Status,
			Speed:         n.Speed,
			Purpose:       n.Purpose,
		})
	}

	// 6) 把虚拟节点追加到 nodes
	for _, v := range virtualNodes {
		nodes = append(nodes, *v)
	}

	// 7) 更新节点的 connected_assets 计数
	for i := range nodes {
		if !nodes[i].IsVirtual {
			nodes[i].ConnectedAssets = connectedCount[nodes[i].ID.String()]
		}
	}

	// 8) 应用 OnlyWithAlerts 过滤
	if filter.OnlyWithAlerts {
		filtered := nodes[:0]
		for _, n := range nodes {
			if n.OpenAlerts > 0 {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}

	// 9) 排序（id 稳定排序，方便 diff）
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID.String() < nodes[j].ID.String() })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID.String() < edges[j].ID.String() })

	// 10) 统计
	var withAlert int64
	for _, n := range nodes {
		if n.OpenAlerts > 0 {
			withAlert++
		}
	}
	return &models.TopologyGraph{
		Nodes: nodes,
		Edges: edges,
		Stats: models.TopologyStats{
			TotalNodes:     len(nodes),
			TotalEdges:     len(edges),
			NodesWithAlert: withAlert,
			DownEdges:      downEdges,
			VirtualNodes:   len(virtualNodes),
			WindowDays:     days,
		},
		GeneratedAt: time.Now().UTC(),
	}, nil
}

// resolveTarget 解析 connected_to 字符串，返回 target 节点 ID
// 解析规则：
//  1. "<asset_name>:<port>" → 找 assets.name 匹配
//  2. "<asset_name>" → 找 assets.name 匹配
//  3. 找不到 → 生成虚拟节点
func (s *TopologyService) resolveTarget(connectedTo string, assetByName map[string]uuid.UUID, virtualNodes map[string]*models.TopologyNode) (string, bool) {
	if connectedTo == "" {
		return "", false
	}
	// 分割 port
	targetName := connectedTo
	for i := 0; i < len(connectedTo); i++ {
		if connectedTo[i] == ':' {
			targetName = connectedTo[:i]
			break
		}
	}
	if id, ok := assetByName[targetName]; ok {
		return id.String(), false
	}
	// 虚拟节点：用稳定 hash 生成 UUID（保证多次调用同输入同 ID）
	id := virtualIDFor(targetName)
	virt := &models.TopologyNode{
		ID:        id,
		Name:      targetName,
		Status:    "external",
		IsVirtual: true,
		// 虚拟节点放在外围
		PositionX: 400,
		PositionY: 0,
	}
	virtualNodes[targetName] = virt
	return id.String(), true
}

func virtualIDFor(name string) uuid.UUID {
	h := fnv.New32a()
	_, _ = h.Write([]byte("virtual|" + name))
	// 用 hash 生成一个稳定 UUID（不是真随机但对前端渲染够用）
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("virtual-node-%s-%d", name, h.Sum32())))
}
