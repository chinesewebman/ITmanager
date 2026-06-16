// AlertSuppressions.test.tsx — 抑制规则管理页（P0-2）
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const { mockRules } = vi.hoisted(() => ({
  mockRules: [
    { id: 'r1', name: '抑制 db-*', host_pattern: 'db-*', severity_max: 3, time_window_seconds: 300, ttl_seconds: 0, enabled: true, description: '5 分钟内同 host 仅 1 条 warning' },
    { id: 'r2', name: '抑制 web-*', host_pattern: 'web-*', severity_max: 2, time_window_seconds: 600, ttl_seconds: 3600, enabled: false, description: '10 分钟窗口' },
  ],
}))

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: () => ({ data: mockRules, isLoading: false, error: null, refetch: () => {} }),
  queryKeys: {},
}))

import { AlertSuppressions } from './AlertSuppressions'

beforeEach(() => { localStorage.clear() })

describe('AlertSuppressions', () => {
  it('渲染规则列表 + 标题', async () => {
    render(
      <MemoryRouter>
        <AlertSuppressions />
      </MemoryRouter>,
    )
    expect(await screen.findByText('告警抑制规则')).toBeTruthy()
    expect(await screen.findByText('抑制 db-*')).toBeTruthy()
    expect(await screen.findByText('抑制 web-*')).toBeTruthy()
    expect(await screen.findByText('db-*')).toBeTruthy()
    expect(await screen.findByText('web-*')).toBeTruthy()
  })

  it('显示时间窗口 + TTL + 启用状态', async () => {
    render(
      <MemoryRouter>
        <AlertSuppressions />
      </MemoryRouter>,
    )
    expect(await screen.findByText('300 秒')).toBeTruthy()
    expect(await screen.findByText('600 秒')).toBeTruthy()
    expect(await screen.findByText('3600 秒')).toBeTruthy()
    expect(await screen.findByText('不过期')).toBeTruthy()
    expect(await screen.findByText('ON')).toBeTruthy()
    expect(await screen.findByText('OFF')).toBeTruthy()
  })

  it('显示操作按钮（新建/编辑/删除/模拟评估）', async () => {
    render(
      <MemoryRouter>
        <AlertSuppressions />
      </MemoryRouter>,
    )
    expect(await screen.findByText(/新建抑制规则/)).toBeTruthy()
    expect(await screen.findByText(/模拟评估/)).toBeTruthy()
    // 用 role 找 button（编辑 + 删除 + 新建 + 模拟评估 + 2 rules × 2 = 6 个）
    const buttons = await screen.findAllByRole('button')
    expect(buttons.length).toBeGreaterThanOrEqual(4)
  })
})
