// AlertSuppressions.test.tsx — 抑制规则管理页（P0-2）
// W1：此前 `catch { return MOCK_RULES }` + `data: rules = MOCK_RULES` 双重兜底，
// isError 恒 false —— 接口挂了页面照常列出虚构的抑制规则，运维会误以为
// 「db-* 的告警已经被抑制了」，而实际一条规则都不存在。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

// vi.hoisted：mock 工厂在 import 期被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  override: {} as Record<string, unknown>,
  refetch: vi.fn(),
}))

const RULES = [
  { id: 'r1', name: '抑制 db-*', host_pattern: 'db-*', severity_max: 3, time_window_seconds: 300, ttl_seconds: 0, enabled: true, description: '5 分钟内同 host 仅 1 条 warning' },
  { id: 'r2', name: '抑制 web-*', host_pattern: 'web-*', severity_max: 2, time_window_seconds: 600, ttl_seconds: 3600, enabled: false, description: '10 分钟窗口' },
]

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: () => ({
    data: RULES,
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: h.refetch,
    ...h.override,
  }),
  queryKeys: {},
}))

import { AlertSuppressions } from './AlertSuppressions'

function renderPage() {
  return render(
    <MemoryRouter>
      <AlertSuppressions />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  h.override = {}
  h.refetch.mockClear()
})

describe('AlertSuppressions', () => {
  it('渲染规则列表 + 标题', async () => {
    renderPage()
    expect(await screen.findByText('告警抑制规则')).toBeInTheDocument()
    expect(await screen.findByText('抑制 db-*')).toBeInTheDocument()
    expect(await screen.findByText('抑制 web-*')).toBeInTheDocument()
    expect(await screen.findByText('db-*')).toBeInTheDocument()
    expect(await screen.findByText('web-*')).toBeInTheDocument()
  })

  it('显示时间窗口 + TTL + 启用状态', async () => {
    renderPage()
    expect(await screen.findByText('300 秒')).toBeInTheDocument()
    expect(await screen.findByText('600 秒')).toBeInTheDocument()
    expect(await screen.findByText('3600 秒')).toBeInTheDocument()
    expect(await screen.findByText('不过期')).toBeInTheDocument()
    expect(await screen.findByText('ON')).toBeInTheDocument()
    expect(await screen.findByText('OFF')).toBeInTheDocument()
  })

  it('显示操作按钮（新建/编辑/删除/模拟评估）', async () => {
    renderPage()
    expect(await screen.findByText(/新建抑制规则/)).toBeInTheDocument()
    expect(await screen.findByText(/模拟评估/)).toBeInTheDocument()
    // 用 role 找 button（编辑 + 删除 + 新建 + 模拟评估 + 2 rules × 2 = 6 个）
    const buttons = await screen.findAllByRole('button')
    expect(buttons.length).toBeGreaterThanOrEqual(4)
  })

  it('W1：接口失败时显示错误态 + 重试，不回落 MOCK_RULES', () => {
    h.override = { data: undefined, isError: true, error: { response: { status: 500 } } }
    renderPage()
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    // 关键回归断言：虚构的规则名必须消失（否则运维会以为抑制在生效）
    expect(screen.queryByText('抑制 db-* 警告')).toBeNull()
    expect(screen.queryByText('抑制 web-* 信息')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch).toHaveBeenCalled()
  })

  it('W1：规则为空时显示空态', () => {
    h.override = { data: [] }
    renderPage()
    expect(screen.getByText('暂无抑制规则')).toBeInTheDocument()
    expect(screen.queryByText('抑制 db-*')).toBeNull()
    // 空列表也不得回落任何虚构规则（含已删除的 MOCK_RULES 内容）
    expect(screen.queryByText('抑制 db-* 警告')).toBeNull()
    expect(screen.queryByText('抑制 web-* 信息')).toBeNull()
  })

  it('点击「编辑」打开弹窗并预填规则', () => {
    renderPage()
    // antd Button 会在两个汉字间插空格 → 用正则匹配
    fireEvent.click(screen.getAllByText(/编\s*辑/)[0])
    expect(screen.getByText('编辑抑制规则')).toBeInTheDocument()
    expect(screen.getByDisplayValue('抑制 db-*')).toBeInTheDocument()
    expect(screen.getByDisplayValue('db-*')).toBeInTheDocument()
  })
})
