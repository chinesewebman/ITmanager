package models

import (
	"time"

	"github.com/google/uuid"
)

// TopologyNode 拓扑节点
type TopologyNode struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	AssetTag        string    `json:"asset_tag"`
	AssetType       string    `json:"asset_type"`
	Brand           string    `json:"brand"`
	Status          string    `json:"status"`
	OpenAlerts      int64     `json:"open_alerts"`
	IsVirtual       bool      `json:"is_virtual"` // true = connected_to 字段引用但不在 assets 表
	ConnectedAssets int       `json:"connected_assets"`
	PositionX       float64   `json:"position_x"` // 建议位置（auto-layout 用，前端可覆盖）
	PositionY       float64   `json:"position_y"`
}

// TopologyEdge 拓扑边（物理链路）
type TopologyEdge struct {
	ID            uuid.UUID `json:"id"`
	Source        string    `json:"source"` // 节点 ID（string 以便虚拟节点用 host:port 命名）
	Target        string    `json:"target"`
	InterfaceName string    `json:"interface_name"`
	Status        string    `json:"status"` // up / down
	Speed         int       `json:"speed"`  // Mbps
	Purpose       string    `json:"purpose"`
}

// TopologyGraph 拓扑图响应
type TopologyGraph struct {
	Nodes       []TopologyNode `json:"nodes"`
	Edges       []TopologyEdge `json:"edges"`
	Stats       TopologyStats  `json:"stats"`
	GeneratedAt time.Time      `json:"generated_at"`
}

// TopologyStats 拓扑统计
type TopologyStats struct {
	TotalNodes     int   `json:"total_nodes"`
	TotalEdges     int   `json:"total_edges"`
	NodesWithAlert int64 `json:"nodes_with_alert"`
	DownEdges      int64 `json:"down_edges"`
	VirtualNodes   int   `json:"virtual_nodes"`
	WindowDays     int   `json:"window_days"`
}
