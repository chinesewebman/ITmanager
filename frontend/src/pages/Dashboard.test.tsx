// FIX-PLAN-UI-PERF §W1/§W2/§W3：Dashboard 去假数据兜底 + 最近告警接 GET /alerts + 区块级错误态。
// 修前：stats/trends 空值回落 MOCK_*，最近告警无条件渲染 4 条假告警，失败永远看不到。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import Dashboard from './Dashboard'

// vi.hoisted：mock 工厂在 import 期就会被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  overrides: {} as Record<string, unknown>,
  refetch: vi.fn(),
}))

const MOCK_STATS = {
  assets: 156,
  alerts: 8,
  tickets: 23,
  sites: 3,
  machines: 45,
  networks: 12,
}
// 7 个点 → 走「后 3 天 vs 前 3 天」分支
const MOCK_TRENDS = [
  { date: '2026-09-01', count: 1 },
  { date: '2026-09-02', count: 2 },
  { date: '2026-09-03', count: 3 },
  { date: '2026-09-04', count: 4 },
  { date: '2026-09-05', count: 5 },
  { date: '2026-09-06', count: 6 },
  { date: '2026-09-07', count: 7 },
]
const MOCK_KPI = {
  mttr_seconds: 3600,
  mttd_seconds: 120,
  alert_density: 1.2,
  sla_closed_rate: 0.9,
  window_days: 7,
  resolved_alerts: 10,
  acked_alerts: 8,
  closed_tickets: 4,
  on_time_tickets: 3,
}
const MOCK_RECENT = [
  {
    id: 'a1',
    host: 'web-01',
    message: 'CPU 使用率过高',
    severity: 4,
    severity_name: 'P4 严重',
    status: 'problem',
    created_at: new Date(Date.now() - 3 * 60 * 1000).toISOString(),
  },
]

const DATA: Record<string, unknown> = {
  'dashboard,stats': MOCK_STATS,
  'dashboard,trends': MOCK_TRENDS,
  'dashboard,kpis': MOCK_KPI,
  'dashboard,recent-alerts': MOCK_RECENT,
}

vi.mock('../hooks/useApiQuery', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../hooks/useApiQuery')>()
  return {
    ...actual,
    useApiQuery: (key: unknown) => {
      const k = Array.isArray(key) ? key.join(',') : String(key)
      return {
        data: DATA[k],
        isLoading: false,
        isError: false,
        error: undefined,
        refetch: h.refetch,
        ...((h.overrides[k] as object) ?? {}),
      }
    },
  }
})

beforeEach(() => {
  h.overrides = {}
  h.refetch.mockClear()
})

describe('Dashboard page', () => {
  it('渲染标题 + 统计卡片（真实接口数据）', () => {
    render(<Dashboard />)
    expect(screen.getByText('仪表盘')).toBeInTheDocument()
    expect(screen.getByText('156')).toBeInTheDocument() // assets
    expect(screen.getByText('8')).toBeInTheDocument() // alerts
    expect(screen.getByText('23')).toBeInTheDocument() // tickets
  })

  it('统计接口失败时显示错误态，不再回落假数字', () => {
    h.overrides['dashboard,stats'] = {
      data: undefined,
      isError: true,
      error: { response: { status: 500 } },
    }
    render(<Dashboard />)
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    // 关键回归断言：假兜底数字必须消失
    expect(screen.queryByText('156')).toBeNull()
    expect(screen.queryByText('23')).toBeNull()
  })

  it('最近告警接口失败时区块内显示错误态 + 重试可触发', () => {
    h.overrides['dashboard,recent-alerts'] = {
      data: undefined,
      isError: true,
      error: { response: { status: 500 } },
    }
    render(<Dashboard />)
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    expect(screen.queryByText('web-01')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch).toHaveBeenCalled()
  })

  it('最近告警渲染真实条目（主机 + 相对时间）', () => {
    render(<Dashboard />)
    expect(screen.getByText('web-01')).toBeInTheDocument()
    expect(screen.getByText('CPU 使用率过高')).toBeInTheDocument()
    expect(screen.getByText('3 分钟前')).toBeInTheDocument()
  })

  it('最近告警为空时显示空态', () => {
    h.overrides['dashboard,recent-alerts'] = { data: [] }
    render(<Dashboard />)
    expect(screen.getByText('暂无告警')).toBeInTheDocument()
    expect(screen.queryByText('web-01')).toBeNull()
  })

  it('趋势角标按后 3 天 - 前 3 天计算（7 点数据 → 18-9=9）', () => {
    render(<Dashboard />)
    // 后 3 天 (5+6+7=18) - 前 3 天 (2+3+4=9) = 9
    expect(screen.getByLabelText('arrow-up').parentElement?.textContent).toContain('9')
  })
})
