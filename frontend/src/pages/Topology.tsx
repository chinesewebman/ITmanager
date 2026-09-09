// 网络拓扑图（P1-1）
// 0 依赖 SVG 渲染节点 + 边，自动布局（后端算 position）
// 故障节点高亮（红色 + badge），down 边变红

// W1：`:84` `?? MOCK_GRAPH` 与 `catch { return MOCK_GRAPH }` 双重兜底已删除。
// 原写法让 isError 恒 false —— 接口挂了页面照常画出 5 个虚构节点和 4 条虚构链路，
// 运维会对着不存在的拓扑排查「db-01 为什么 down」。

import { useState } from 'react'
import { Card, Skeleton, Space, Statistic, Switch, Typography } from 'antd'
import { useApiQuery } from '../hooks/useApiQuery'
import api from '../services/api'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { PageHeader } from '../components/PageHeader'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

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

const EMPTY_STATS: TopologyGraph['stats'] = {
  total_nodes: 0,
  total_edges: 0,
  nodes_with_alert: 0,
  down_edges: 0,
  virtual_nodes: 0,
  window_days: 0,
}

/**
 * 接口形状归一：nodes/edges 非数组、stats 缺失一律当空，不回落 mock。
 * 删掉 `?? MOCK_GRAPH` / `catch { return MOCK_GRAPH }` 后，「200 + 空 data」
 * 会让 `graph.nodes.length` 直接 TypeError 白屏，所以这里必须兜住 undefined。
 */
function normalizeGraph(v: unknown): TopologyGraph {
  const g = (v ?? {}) as Partial<TopologyGraph>
  return {
    nodes: Array.isArray(g.nodes) ? g.nodes : [],
    edges: Array.isArray(g.edges) ? g.edges : [],
    // 对象展开 undefined/null 是 no-op，所以 stats 缺失时自然落到全 0
    stats: { ...EMPTY_STATS, ...g.stats },
  }
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
  useDocumentTitle('网络拓扑')
  const [onlyWithAlerts, setOnlyWithAlerts] = useState(false)

  const { data, isLoading, isError, error, refetch } = useApiQuery<TopologyGraph>(
    ['topology', onlyWithAlerts] as const,
    async () => {
      const res = await api.get('/topology', {
        params: onlyWithAlerts ? { only_with_alerts: true } : {},
      })
      return normalizeGraph(res.data?.data)
    },
  )

  const graph = normalizeGraph(data)
  // 把后端 position 映射到 viewbox
  const nodeMap = new Map(graph.nodes.map((n) => [n.id, n]))

  return (
    <div>
      {/* M1：标题体系统一——原无可见标题（仅 useDocumentTitle），筛选开关悬空 */}
      <PageHeader title="网络拓扑" />
      <Space style={{ marginBottom: 16 }}>
        <Text>仅显示告警节点：</Text>
        <Switch checked={onlyWithAlerts} onChange={setOnlyWithAlerts} />
      </Space>

      {isLoading ? (
        <Skeleton active />
      ) : isError ? (
        <ErrorState error={error} onRetry={refetch} />
      ) : graph.nodes.length === 0 ? (
        <Card title="网络拓扑">
          <EmptyState
            title="暂无拓扑节点"
            description="导入资产后会自动绘制拓扑图"
          />
        </Card>
      ) : (
        <>
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
        </>
      )}
    </div>
  )
}

export default Topology
