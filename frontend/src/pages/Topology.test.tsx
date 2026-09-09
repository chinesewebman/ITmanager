// Topology.test.tsx — 网络拓扑图（P1-1）
// W1：此前 queryFn 内 `?? MOCK_GRAPH` + `catch { return MOCK_GRAPH }` 双重兜底，
// isError 恒 false —— 接口挂了页面照常画出 5 个虚构节点和 4 条虚构链路，
// 运维会对着不存在的拓扑排查「db-01 为什么 down」。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, within, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

// vi.hoisted：mock 工厂在 import 期被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  override: {} as Record<string, unknown>,
  refetch: vi.fn(),
}))

// 节点名刻意与已删除的 MOCK_GRAPH 不重名（mock 里是 switch-core/web-01/db-01/…），
// 这样「假拓扑必须消失」的断言才有区分度。
const GRAPH = {
  nodes: [
    { id: 'n1', name: 'sw-core', asset_type: 'switch', status: 'active', open_alerts: 0, is_virtual: false, position_x: 0, position_y: 0 },
    { id: 'n2', name: 'app-01', asset_type: 'server', status: 'active', open_alerts: 3, is_virtual: false, position_x: 300, position_y: 0 },
    { id: 'n3', name: 'app-02', asset_type: 'server', status: 'active', open_alerts: 0, is_virtual: false, position_x: -300, position_y: 0 },
    { id: 'n4', name: 'rtr-wan', asset_type: 'router', status: 'external', open_alerts: 0, is_virtual: true, position_x: 0, position_y: -300 },
  ],
  edges: [
    { id: 'e1', source: 'n1', target: 'n2', interface_name: 'Gi0/1', status: 'up' },
    { id: 'e2', source: 'n1', target: 'n3', interface_name: 'Gi0/2', status: 'down' },
    { id: 'e3', source: 'n1', target: 'n4', interface_name: 'Gi0/3', status: 'up' },
  ],
  stats: { total_nodes: 4, total_edges: 3, nodes_with_alert: 1, down_edges: 1, virtual_nodes: 1, window_days: 30 },
}

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: () => ({
    data: GRAPH,
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: h.refetch,
    ...h.override,
  }),
  queryKeys: {},
}))

import { Topology } from './Topology'

function renderPage() {
  return render(
    <MemoryRouter>
      <Topology />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  h.override = {}
  h.refetch.mockClear()
})

describe('Topology', () => {
  it('渲染统计卡片 + 标题', () => {
    renderPage()
    expect(screen.getByText('网络拓扑图')).toBeInTheDocument()
    expect(screen.getByText('节点总数')).toBeInTheDocument()
    expect(screen.getByText('边总数')).toBeInTheDocument()
    expect(screen.getByText('告警节点')).toBeInTheDocument()
    expect(screen.getByText('Down 边')).toBeInTheDocument()
    expect(screen.getByText('虚拟节点')).toBeInTheDocument()
    expect(screen.getByText('30 天')).toBeInTheDocument()
  })

  it('渲染所有节点名', () => {
    const { container } = renderPage()
    const svg = container.querySelector('svg') as unknown as HTMLElement
    expect(within(svg).getAllByText(/sw-core|app-01|app-02|rtr-wan/)).toHaveLength(4)
  })

  it('告警节点显示 badge 数字', () => {
    const { container } = renderPage()
    const svg = container.querySelector('svg') as unknown as HTMLElement
    // 3 = app-01 的告警数
    expect(within(svg).getByText('3')).toBeInTheDocument()
  })

  it('显示边接口名', () => {
    const { container } = renderPage()
    const svg = container.querySelector('svg') as unknown as HTMLElement
    expect(within(svg).getByText('Gi0/1')).toBeInTheDocument()
    expect(within(svg).getByText('Gi0/2')).toBeInTheDocument()
  })

  it('显示告警过滤 switch', () => {
    renderPage()
    expect(screen.getByText(/仅显示告警节点/)).toBeInTheDocument()
  })

  it('W1：接口失败显示错误态 + 重试，不回落 MOCK_GRAPH', () => {
    h.override = { data: undefined, isError: true, error: { response: { status: 500 } } }
    const { container } = renderPage()
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    // MOCK_GRAPH 里的虚构拓扑必须消失（否则运维会去查一条不存在的链路）
    // 注意 ErrorState 自己也有 SVG 图标，所以按拓扑图的 viewBox 精确匹配
    expect(container.querySelector('svg[viewBox="0 0 800 800"]')).toBeNull()
    expect(screen.queryByText('switch-core')).toBeNull()
    expect(screen.queryByText('db-01')).toBeNull()
    expect(screen.queryByText('external-router')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch).toHaveBeenCalled()
  })

  it('W1：nodes 为空数组时显示空态', () => {
    h.override = { data: { nodes: [], edges: [], stats: GRAPH.stats } }
    renderPage()
    expect(screen.getByText('暂无拓扑节点')).toBeInTheDocument()
  })

  it('W1：200 + 空 data 不白屏（undefined 守卫），走空态', () => {
    h.override = { data: undefined }
    expect(() => renderPage()).not.toThrow()
    expect(screen.getByText('暂无拓扑节点')).toBeInTheDocument()
  })

  it('W1：nodes 字段缺失时按空处理，不 TypeError', () => {
    h.override = { data: { stats: GRAPH.stats } }
    expect(() => renderPage()).not.toThrow()
    expect(screen.getByText('暂无拓扑节点')).toBeInTheDocument()
  })

  it('W1：stats 缺失时统计卡归零而不是崩溃', () => {
    h.override = { data: { nodes: GRAPH.nodes, edges: GRAPH.edges } }
    expect(() => renderPage()).not.toThrow()
    expect(screen.getByText('网络拓扑图')).toBeInTheDocument()
    const values = Array.from(
      document.querySelectorAll('.ant-statistic-content-value'),
    ).map((el) => el.textContent)
    expect(values).toEqual(['0', '0', '0', '0', '0', '0 天'])
  })
})
