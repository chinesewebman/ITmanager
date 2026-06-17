// Dashboard page smoke test
import '@testing-library/jest-dom'
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import Dashboard from './Dashboard'

// Mock useApiQuery — Dashboard 没用 mutation，但需要 stats + trends
const mockStats = {
  assets: 156,
  alerts: 8,
  tickets: 23,
  sites: 3,
  machines: 45,
  networks: 12,
}
const mockTrends = [
  { date: '2026-02-08', count: 10 },
  { date: '2026-02-09', count: 15 },
]

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: (key: unknown) => ({
    data: Array.isArray(key) && key.join(',').includes('trends') ? mockTrends : mockStats,
    isLoading: false,
    refetch: vi.fn(),
  }),
  useApiMutation: () => ({ mutate: vi.fn(), mutateAsync: vi.fn() }),
  queryKeys: { dashboard: { stats: () => ['dashboard', 'stats'], trends: () => ['dashboard', 'trends'] } },
}))

describe('Dashboard page', () => {
  it('渲染页面标题 + 统计卡片（mock 数据）', () => {
    render(<Dashboard />)
    // PageHeader 标题
    expect(screen.getByText('仪表盘')).toBeInTheDocument()
    // 统计卡片显示 mockStats 数字
    expect(screen.getByText('156')).toBeInTheDocument() // assets
    expect(screen.getByText('8')).toBeInTheDocument()   // alerts
    expect(screen.getByText('23')).toBeInTheDocument()  // tickets
  })

  it('不 crash 渲染', () => {
    expect(() => render(<Dashboard />)).not.toThrow()
  })
})
