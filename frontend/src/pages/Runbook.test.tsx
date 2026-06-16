// Runbook.test.tsx — 故障 Runbook 管理页（P2-1）
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const { mockRunbooks, mockRecommend } = vi.hoisted(() => ({
  mockRunbooks: [
    { id: 'r1', title: 'MySQL 主从延迟告警处理', asset_type: 'server', summary: '主从延迟 > 30s', severity: 4, enabled: true, tags: 'db,mysql' },
    { id: 'r2', title: '核心交换机端口 down', asset_type: 'switch', summary: '核心交换机端口 down', severity: 5, enabled: true, tags: 'network' },
  ],
  mockRecommend: [
    { id: 'r1', title: 'MySQL 主从延迟告警处理', asset_type: 'server', summary: '主从延迟 > 30s', severity: 4, enabled: true, tags: 'db,mysql' },
  ],
}))

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: (_key: any) => {
    const k = JSON.stringify(_key)
    if (k.includes('recommend')) {
      return { data: mockRecommend, isLoading: false, error: null, refetch: vi.fn() }
    }
    return { data: { items: mockRunbooks, total: 2 }, isLoading: false, error: null, refetch: vi.fn() }
  },
  queryKeys: {},
}))

import RunbookList, { RunbookRecommend } from './Runbook'

describe('Runbook', () => {
  it('渲染列表 + 标题 + mock 数据', () => {
    render(<MemoryRouter><RunbookList /></MemoryRouter>)
    expect(screen.getByText('故障 Runbook')).toBeTruthy()
    expect(screen.getByText('MySQL 主从延迟告警处理')).toBeTruthy()
    expect(screen.getByText('核心交换机端口 down')).toBeTruthy()
  })

  it('显示资产类型 tag + 严重度 tag', () => {
    render(<MemoryRouter><RunbookList /></MemoryRouter>)
    expect(screen.getAllByText('server').length).toBeGreaterThan(0)
    expect(screen.getAllByText('P4').length).toBeGreaterThan(0)
    expect(screen.getAllByText('P5').length).toBeGreaterThan(0)
  })

  it('推荐面板显示 mock 推荐项', () => {
    render(<MemoryRouter><RunbookRecommend assetType="server" severity={4} /></MemoryRouter>)
    expect(screen.getByText('MySQL 主从延迟告警处理')).toBeTruthy()
  })
})
