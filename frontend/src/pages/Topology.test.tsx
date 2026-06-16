// Topology.test.tsx — 网络拓扑图（P1-1）
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const { mockGraph } = vi.hoisted(() => ({
  mockGraph: {
    nodes: [
      { id: 'n1', name: 'switch-core', asset_type: 'switch', status: 'active', open_alerts: 0, is_virtual: false, position_x: 0, position_y: 0 },
      { id: 'n2', name: 'web-01', asset_type: 'server', status: 'active', open_alerts: 3, is_virtual: false, position_x: 300, position_y: 0 },
      { id: 'n3', name: 'web-02', asset_type: 'server', status: 'active', open_alerts: 0, is_virtual: false, position_x: -300, position_y: 0 },
      { id: 'n4', name: 'external', asset_type: 'router', status: 'external', open_alerts: 0, is_virtual: true, position_x: 0, position_y: -300 },
    ],
    edges: [
      { id: 'e1', source: 'n1', target: 'n2', interface_name: 'Gi0/1', status: 'up' },
      { id: 'e2', source: 'n1', target: 'n3', interface_name: 'Gi0/2', status: 'down' },
      { id: 'e3', source: 'n1', target: 'n4', interface_name: 'Gi0/3', status: 'up' },
    ],
    stats: { total_nodes: 4, total_edges: 3, nodes_with_alert: 1, down_edges: 1, virtual_nodes: 1, window_days: 30 },
  },
}))

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: () => ({ data: mockGraph, isLoading: false, error: null }),
  queryKeys: {},
}))

import { Topology } from './Topology'

beforeEach(() => { localStorage.clear() })

describe('Topology', () => {
  it('渲染统计卡片 + 标题', async () => {
    render(<MemoryRouter><Topology /></MemoryRouter>)
    expect(await screen.findByText('网络拓扑图')).toBeTruthy()
    expect(await screen.findByText('节点总数')).toBeTruthy()
    expect(await screen.findByText('边总数')).toBeTruthy()
    expect(await screen.findByText('告警节点')).toBeTruthy()
    expect(await screen.findByText('Down 边')).toBeTruthy()
    expect(await screen.findByText('虚拟节点')).toBeTruthy()
    expect(await screen.findByText('30 天')).toBeTruthy()
  })

  it('渲染所有节点名', async () => {
    const { container } = render(<MemoryRouter><Topology /></MemoryRouter>)
    await screen.findByText('网络拓扑图')
    const svg = container.querySelector('svg') as unknown as HTMLElement
    const nodes = within(svg).getAllByText(/switch-core|web-01|web-02|external/)
    expect(nodes.length).toBeGreaterThanOrEqual(4)
  })

  it('告警节点显示 badge 数字', async () => {
    const { container } = render(<MemoryRouter><Topology /></MemoryRouter>)
    await screen.findByText('网络拓扑图')
    const svg = container.querySelector('svg') as unknown as HTMLElement
    // 3 = web-01 的告警数
    expect(within(svg).getByText('3')).toBeTruthy()
  })

  it('显示边接口名', async () => {
    const { container } = render(<MemoryRouter><Topology /></MemoryRouter>)
    await screen.findByText('网络拓扑图')
    const svg = container.querySelector('svg') as unknown as HTMLElement
    expect(within(svg).getByText('Gi0/1')).toBeTruthy()
    expect(within(svg).getByText('Gi0/2')).toBeTruthy()
  })

  it('显示告警过滤 switch', async () => {
    render(<MemoryRouter><Topology /></MemoryRouter>)
    expect(await screen.findByText(/仅显示告警节点/)).toBeTruthy()
  })
})
