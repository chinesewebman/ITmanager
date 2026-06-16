// 网络拓扑图（P1-1）
// 0 依赖 SVG 渲染节点 + 边，自动布局（后端算 position）
// 故障节点高亮（红色 + badge），down 边变红

import { useState } from 'react'
import { Card, Empty, Skeleton, Space, Statistic, Switch, Typography } from 'antd'
import { useApiQuery } from '../hooks/useApiQuery'

const { Text } = Typography

interface TopologyNode {
  id: string
  name: string
  asset_type?: string
  status?: string
  open_alerts: number
  is_virtual: boolean
  position_x: number
  position_y: number
}

interface TopologyEdge {
  id: string
  source: string
  target: string
  interface_name: string
  status: string
}

interface TopologyGraph {
  nodes: TopologyNode[]
  edges: TopologyEdge[]
  stats: {
    total_nodes: number
    total_edges: number
    nodes_with_alert: number
    down_edges: number
    virtual_nodes: number
    window_days: number
  }
}

const MOCK_GRAPH: TopologyGraph = {
  nodes: [
    { id: 'n1', name: 'switch-core', asset_type: 'switch', status: 'active', open_alerts: 0, is_virtual: false, position_x: 0, position_y: 0 },
    { id: 'n2', name: 'web-01', asset_type: 'server', status: 'active', open_alerts: 2, is_virtual: false, position_x: 300, position_y: 0 },
    { id: 'n3', name: 'web-02', asset_type: 'server', status: 'active', open_alerts: 0, is_virtual: false, position_x: -300, position_y: 0 },
    { id: 'n4', name: 'db-01', asset_type: 'server', status: 'active', open_alerts: 0, is_virtual: false, position_x: 0, position_y: 300 },
    { id: 'n5', name: 'external-router', asset_type: 'router', status: 'external', open_alerts: 0, is_virtual: true, position_x: 0, position_y: -300 },
  ],
  edges: [
    { id: 'e1', source: 'n1', target: 'n2', interface_name: 'Gi0/1', status: 'up' },
    { id: 'e2', source: 'n1', target: 'n3', interface_name: 'Gi0/2', status: 'up' },
    { id: 'e3', source: 'n1', target: 'n4', interface_name: 'Gi0/3', status: 'down' },
    { id: 'e4', source: 'n1', target: 'n5', interface_name: 'Gi0/4', status: 'up' },
  ],
  stats: { total_nodes: 5, total_edges: 4, nodes_with_alert: 1, down_edges: 1, virtual_nodes: 1, window_days: 30 },
}

const VIEWBOX = 800
const CENTER = VIEWBOX / 2

// node 颜色：告警>0 红 / virtual 紫 / 正常 绿
function nodeColor(n: TopologyNode): string {
  if (n.open_alerts > 0) return '#cf1322'
  if (n.is_virtual) return '#722ed1'
  return '#52c41a'
}

export function Topology() {
  const [onlyWithAlerts, setOnlyWithAlerts] = useState(false)

  const { data, isLoading } = useApiQuery<TopologyGraph>(
    ['topology', onlyWithAlerts] as const,
    async () => {
      const token = localStorage.getItem('token') ?? ''
      const url = new URL('/api/v1/topology', window.location.origin)
      if (onlyWithAlerts) url.searchParams.set('only_with_alerts', 'true')
      const res = await fetch(url.toString(), { headers: { Authorization: `Bearer ${token}` } })
      if (!res.ok) return MOCK_GRAPH
      const json: any = await res.json()
      return json?.data ?? MOCK_GRAPH
    },
  )

  if (isLoading) return <Skeleton active />

  const graph = data ?? MOCK_GRAPH
  if (graph.nodes.length === 0) {
    return (
      <Card title="网络拓扑">
        <Empty description="暂无节点" />
      </Card>
    )
  }

  // 把后端 position 映射到 viewbox
  const nodeMap = new Map(graph.nodes.map((n) => [n.id, n]))

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Text>仅显示告警节点：</Text>
        <Switch checked={onlyWithAlerts} onChange={setOnlyWithAlerts} />
      </Space>

      <Space size="large" style={{ marginBottom: 16, width: '100%' }} wrap>
        <Card size="small"><Statistic title="节点总数" value={graph.stats.total_nodes} /></Card>
        <Card size="small"><Statistic title="边总数" value={graph.stats.total_edges} /></Card>
        <Card size="small"><Statistic title="告警节点" value={graph.stats.nodes_with_alert} valueStyle={{ color: '#cf1322' }} /></Card>
        <Card size="small"><Statistic title="Down 边" value={graph.stats.down_edges} valueStyle={{ color: '#fa8c16' }} /></Card>
        <Card size="small"><Statistic title="虚拟节点" value={graph.stats.virtual_nodes} valueStyle={{ color: '#722ed1' }} /></Card>
        <Card size="small"><Statistic title="窗口" value={`${graph.stats.window_days} 天`} /></Card>
      </Space>

      <Card title="网络拓扑图" size="small">
        <svg
          viewBox={`0 0 ${VIEWBOX} ${VIEWBOX}`}
          style={{ width: '100%', height: 600, background: '#fafafa', borderRadius: 6 }}
        >
          {/* 边 */}
          {graph.edges.map((e) => {
            const s = nodeMap.get(e.source)
            const t = nodeMap.get(e.target)
            if (!s || !t) return null
            const sx = CENTER + s.position_x
            const sy = CENTER + s.position_y
            const tx = CENTER + t.position_x
            const ty = CENTER + t.position_y
            const color = e.status === 'down' ? '#fa8c16' : '#8c8c8c'
            const width = e.status === 'down' ? 3 : 2
            return (
              <g key={e.id}>
                <line
                  x1={sx} y1={sy} x2={tx} y2={ty}
                  stroke={color} strokeWidth={width} strokeDasharray={e.status === 'down' ? '5,5' : undefined}
                />
                <text
                  x={(sx + tx) / 2} y={(sy + ty) / 2}
                  fontSize="11" fill="#595959" textAnchor="middle"
                  style={{ paintOrder: 'stroke', stroke: '#fafafa', strokeWidth: 3 }}
                >
                  {e.interface_name}
                </text>
              </g>
            )
          })}

          {/* 节点 */}
          {graph.nodes.map((n) => {
            const cx = CENTER + n.position_x
            const cy = CENTER + n.position_y
            const color = nodeColor(n)
            const r = n.open_alerts > 0 ? 32 : 26
            return (
              <g key={n.id}>
                <circle
                  cx={cx} cy={cy} r={r}
                  fill={color} fillOpacity={0.15}
                  stroke={color} strokeWidth={2.5}
                  style={{ cursor: 'pointer' }}
                />
                {n.open_alerts > 0 && (
                  <g>
                    <circle cx={cx + r * 0.7} cy={cy - r * 0.7} r="10" fill="#cf1322" />
                    <text x={cx + r * 0.7} y={cy - r * 0.7 + 4} fontSize="11" fill="#fff" textAnchor="middle" fontWeight="bold">
                      {n.open_alerts}
                    </text>
                  </g>
                )}
                <text x={cx} y={cy + 4} fontSize="12" fill="#262626" textAnchor="middle" fontWeight="600">
                  {n.name}
                </text>
                <text x={cx} y={cy + r + 14} fontSize="10" fill="#8c8c8c" textAnchor="middle">
                  {n.asset_type ?? ''}{n.is_virtual ? ' · 虚拟' : ''}
                </text>
              </g>
            )
          })}
        </svg>
      </Card>
    </div>
  )
}

export default Topology
