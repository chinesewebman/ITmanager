// Oncall.test.tsx — 值班 + 升级管理页（P1-2）
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const { mockCurrent, mockSchedules, mockPolicies } = vi.hoisted(() => ({
  mockCurrent: [
    { schedule_id: 's1', schedule_name: 'dev-team', user_name: 'alice', ends_at: new Date(Date.now() + 3 * 3600000).toISOString() },
  ],
  mockSchedules: [
    { id: 's1', name: 'dev-team', description: '研发组', enabled: true },
    { id: 's2', name: 'ops-team', description: '运维组', enabled: true },
  ],
  mockPolicies: [
    { id: 'p1', name: 'critical', enabled: true, levels: [
      { level: 1, target_type: 'user', target_id: 'alice', wait_minutes: 5, notify_methods: 'email' },
    ] },
  ],
}))

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: (_key: any) => ({
    data: (() => {
      const k = JSON.stringify(_key)
      if (k.includes('current')) return mockCurrent
      if (k.includes('schedules')) return mockSchedules
      if (k.includes('policies')) return mockPolicies
      return null
    })(),
    isLoading: false,
    error: null,
    refetch: vi.fn(),
  }),
  queryKeys: {},
}))

import { Oncall } from './Oncall'

describe('Oncall', () => {
  it('渲染 3 个 tab', () => {
    render(<MemoryRouter><Oncall /></MemoryRouter>)
    expect(screen.getByText('当前值班')).toBeTruthy()
    expect(screen.getByText('值班组')).toBeTruthy()
    expect(screen.getByText('升级策略')).toBeTruthy()
  })

  it('当前值班 tab 默认显示 mock 数据', async () => {
    render(<MemoryRouter><Oncall /></MemoryRouter>)
    // 标题在 card 标题
    expect(await screen.findByText('当前在班')).toBeTruthy()
  })
})
