// Oncall.test.tsx — 值班 + 升级管理页（P1-2）
// W1：三个 tab 此前都在 queryFn 内 `catch { return MOCK_* }` + `data ?? MOCK_*`
// 双重兜底 → isError 恒 false，接口挂了页面照常显示虚构的值班人/值班组/升级策略。
// W2：当前值班时间此前用无 locale 的 toLocaleString('zh-CN')。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

// vi.hoisted：mock 工厂在 import 期被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  overrides: {} as Record<string, Record<string, unknown>>,
  refetch: {} as Record<string, ReturnType<typeof vi.fn>>,
}))

// 时间串不带时区偏移 → dayjs 按本地解析，断言与 CI 时区无关
// schedule_name 用不与 SCHEDULES 重名的值：antd Tabs 切页后当前 tab 仍挂载，
// 重名会让「值班组」tab 的断言撞上「当前值班」卡片的标题
const CURRENT = [
  { schedule_id: 's1', schedule_name: '研发组值班', user_name: 'alice', ends_at: '2026-02-14T10:00:00' },
]
const SCHEDULES = [
  { id: 's1', name: 'dev-team', description: '研发组', enabled: true },
  { id: 's2', name: 'ops-team', description: '运维组', enabled: true },
]
const POLICIES = [
  { id: 'p1', name: 'critical', enabled: true, levels: [
    { level: 1, target_type: 'user', target_id: 'alice', wait_minutes: 5, notify_methods: 'email' },
  ] },
]

const DATA: Record<string, unknown> = { current: CURRENT, schedules: SCHEDULES, policies: POLICIES }

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: (key: unknown) => {
    const k = Array.isArray(key) ? String(key[1]) : ''
    if (!h.refetch[k]) h.refetch[k] = vi.fn()
    return {
      data: DATA[k],
      isLoading: false,
      isError: false,
      error: undefined,
      refetch: h.refetch[k],
      ...h.overrides[k],
    }
  },
  queryKeys: {},
}))

import { Oncall } from './Oncall'

function renderOncall() {
  return render(<MemoryRouter><Oncall /></MemoryRouter>)
}

/** 切到某个 tab（antd 惰性渲染，非激活 tab 不挂载）。 */
function openTab(label: string) {
  fireEvent.click(screen.getByRole('tab', { name: label }))
}

beforeEach(() => {
  h.overrides = {}
  for (const k of ['current', 'schedules', 'policies']) h.refetch[k]?.mockClear()
})

describe('Oncall', () => {
  it('渲染 3 个 tab', () => {
    renderOncall()
    expect(screen.getByText('当前值班')).toBeInTheDocument()
    expect(screen.getByText('值班组')).toBeInTheDocument()
    expect(screen.getByText('升级策略')).toBeInTheDocument()
  })

  it('当前值班 tab 显示值班人 + W2 时间格式化', async () => {
    renderOncall()
    expect(await screen.findByText('当前在班')).toBeInTheDocument()
    expect(screen.getByText('alice')).toBeInTheDocument()
    // W2：统一走 utils/time（原 toLocaleString('zh-CN') 无 locale 参数，格式随环境变）
    expect(screen.getByText('值班至 2026-02-14 10:00:00')).toBeInTheDocument()
  })

  it('W1：当前值班失败时显示错误态 + 重试', () => {
    h.overrides.current = { data: undefined, isError: true, error: { response: { status: 500 } } }
    renderOncall()
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    expect(screen.queryByText('alice')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch.current).toHaveBeenCalled()
  })

  it('W1：当前无人在班时显示空态，不显示虚构值班人', () => {
    h.overrides.current = { data: [] }
    renderOncall()
    expect(screen.getByText('当前无人在班')).toBeInTheDocument()
    expect(screen.queryByText('alice')).toBeNull()
  })

  it('W1：值班组失败时显示错误态 + 重试，不回落 MOCK_SCHEDULES', () => {
    h.overrides.schedules = { data: undefined, isError: true, error: { response: { status: 500 } } }
    renderOncall()
    openTab('值班组')
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    expect(screen.queryByText('dev-team')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch.schedules).toHaveBeenCalled()
  })

  it('W1：值班组为空时表格显示空态', () => {
    h.overrides.schedules = { data: [] }
    renderOncall()
    openTab('值班组')
    expect(screen.getByText('暂无值班组')).toBeInTheDocument()
    expect(screen.queryByText('dev-team')).toBeNull()
  })

  it('值班组正常渲染（表格行 + 启用标签）', () => {
    renderOncall()
    openTab('值班组')
    expect(screen.getByText('dev-team')).toBeInTheDocument()
    expect(screen.getByText('ops-team')).toBeInTheDocument()
    expect(screen.getAllByText('ON')).toHaveLength(2)
  })

  it('W1：升级策略失败时显示错误态，不回落 MOCK_POLICIES', () => {
    h.overrides.policies = { data: undefined, isError: true, error: { response: { status: 500 } } }
    renderOncall()
    openTab('升级策略')
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    expect(screen.queryByText('critical')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch.policies).toHaveBeenCalled()
  })

  it('W1：升级策略为空时表格显示空态', () => {
    h.overrides.policies = { data: [] }
    renderOncall()
    openTab('升级策略')
    expect(screen.getByText('暂无升级策略')).toBeInTheDocument()
  })

  it('升级策略正常渲染（层级数 + 层级详情）', () => {
    renderOncall()
    openTab('升级策略')
    expect(screen.getByText('critical')).toBeInTheDocument()
    expect(screen.getByText('1 级')).toBeInTheDocument()
    expect(screen.getByText('L1 user/alice 5m email')).toBeInTheDocument()
  })
})
