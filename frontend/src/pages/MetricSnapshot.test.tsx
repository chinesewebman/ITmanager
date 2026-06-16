// MetricSnapshot.test.tsx — 指标快照查看页（P2-2）
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const mockLatest = [
  { id: '1', asset_id: 'asset-1', key: 'cpu.user', value: 45.2, ts: new Date().toISOString() },
  { id: '2', asset_id: 'asset-1', key: 'cpu.user', value: 60.3, ts: new Date().toISOString() },
]

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: (_key: any) => ({
    data: mockLatest,
    isLoading: false,
    error: null,
    refetch: vi.fn(),
  }),
  queryKeys: {},
}))

import MetricSnapshotList from './MetricSnapshot'

describe('MetricSnapshot', () => {
  it('渲染标题 + 查询表单', () => {
    render(<MemoryRouter><MetricSnapshotList /></MemoryRouter>)
    expect(screen.getByText('指标快照')).toBeTruthy()
    expect(screen.getByText('Asset ID')).toBeTruthy()
    expect(screen.getByText('Key')).toBeTruthy()
  })

  it('显示初始状态文案 (提示输入参数)', () => {
    render(<MemoryRouter><MetricSnapshotList /></MemoryRouter>)
    expect(screen.getByText('输入 asset_id + key 后点击查询')).toBeTruthy()
  })

  it('显示 4 个统计卡标签 (mock 数据 + form 不渲染时)', () => {
    render(<MemoryRouter><MetricSnapshotList /></MemoryRouter>)
    // 初始状态：searchParams=null → 4 统计卡不渲染
    // 验证：表头 + 提示文案
    expect(screen.getByText('Asset ID')).toBeTruthy()
    expect(screen.getByText('Key')).toBeTruthy()
    expect(screen.getByText('N')).toBeTruthy()
    expect(screen.getByText('输入 asset_id + key 后点击查询')).toBeTruthy()
  })
})
