// Topology.test.tsx — 网络拓扑图（P1-1）
// W1：此前 queryFn 内 `?? MOCK_GRAPH` + `catch { return MOCK_GRAPH }` 双重兜底，
// isError 恒 false —— 接口挂了页面照常画出 5 个虚构节点和 4 条虚构链路，
// 运维会对着不存在的拓扑排查「db-01 为什么 down」。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, within, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type * as RouterDom from 'react-router-dom'

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

// M48：节点点击 → navigate 到诊断页，用 spy 的 useNavigate 断言（同 CommandPalette
// 测试的写法：importActual 之后再覆盖单点，MemoryRouter 等真实实现保持不变）。
const navigateMock = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof RouterDom>('react-router-dom')
  return { ...actual, useNavigate: () => navigateMock }
})

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
  navigateMock.mockClear()
})

describe('Topology', () => {
  // M1：标题体系统一——Topology 原无可见标题（仅 useDocumentTitle 改浏览器标题），
  // 用户进来不知道这是网络拓扑页。补 PageHeader title（h4）。
  it('M1：页面标题统一到 PageHeader（h4「网络拓扑」）', () => {
    renderPage()
    expect(screen.getByRole('heading', { level: 4, name: '网络拓扑' })).toBeInTheDocument()
  })

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
    // M48：<title> tooltip 里也有节点名，忽略 title 子树，否则同一个名字命中两次
    expect(
      within(svg).getAllByText(/^(sw-core|app-01|app-02|rtr-wan)$/, { ignore: 'title' }),
    ).toHaveLength(4)
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

  // ---- M48：节点是真按钮（此前只有 cursor:pointer，点了毫无反应）----
  const nodeButton = (name: string) => screen.getByRole('button', { name })

  it('M48：点击普通节点 navigate 到 /assets/<id>/diagnostics', () => {
    renderPage()
    fireEvent.click(nodeButton('app-01'))
    expect(navigateMock).toHaveBeenCalledWith('/assets/n2/diagnostics')
  })

  it('M48：点击虚拟节点不 navigate，弹出 meta popover', () => {
    renderPage()
    fireEvent.click(nodeButton('rtr-wan'))
    expect(navigateMock).not.toHaveBeenCalled()
    // 元数据：name / asset_type / open_alerts
    expect(screen.getByText('资产名：rtr-wan')).toBeInTheDocument()
    expect(screen.getByText('类型：router')).toBeInTheDocument()
    expect(screen.getByText('当前告警：0')).toBeInTheDocument()
  })

  it('M48：键盘 Enter 触发 navigate（键盘可达）', () => {
    renderPage()
    fireEvent.keyDown(nodeButton('sw-core'), { key: 'Enter' })
    expect(navigateMock).toHaveBeenCalledWith('/assets/n1/diagnostics')
  })

  it('M48：键盘 Space 触发虚拟节点 popover（不 navigate）', () => {
    renderPage()
    fireEvent.keyDown(nodeButton('rtr-wan'), { key: ' ' })
    expect(navigateMock).not.toHaveBeenCalled()
    expect(screen.getByText('资产名：rtr-wan')).toBeInTheDocument()
  })

  it('M48：节点是 role=button + aria-label，且无 inner cursor 双重化', () => {
    const { container } = renderPage()
    expect(nodeButton('sw-core')).toHaveAttribute('tabindex', '0')
    // 节点名进原生 tooltip
    expect(container.querySelector('g[aria-label="sw-core"] > title')?.textContent).toBe('sw-core')
    // cursor 只在 group 上，inner circle 不再单独写
    expect(container.querySelector('g[aria-label="sw-core"] circle')).not.toHaveAttribute('style')
  })
})
