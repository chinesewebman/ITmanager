// FIX-PLAN-UI-PERF §W3：活跃告警趋势角标。
// 修前写死 ArrowDownOutlined + 绿色 + Math.abs()，告警上升也显示「绿色 ↓ 5」。
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { DashboardCards, type DashboardCardsStats } from './DashboardCards'

const STATS: DashboardCardsStats = {
  assets: 10,
  alerts: 8,
  tickets: 2,
  sites: 1,
  machines: 4,
  networks: 3,
}

describe('DashboardCards 趋势角标', () => {
  it('上升：向上箭头 + 非绿色（告警变多不是好事）', () => {
    render(<DashboardCards stats={STATS} alertTrendDelta={5} />)
    const icon = screen.getByLabelText('arrow-up')
    expect(icon).toBeInTheDocument()
    expect(icon.parentElement?.style.color).not.toBe('rgb(82, 196, 26)')
  })

  it('下降：向下箭头 + 绿色', () => {
    render(<DashboardCards stats={STATS} alertTrendDelta={-3} />)
    const icon = screen.getByLabelText('arrow-down')
    expect(icon).toBeInTheDocument()
    expect(icon.parentElement?.style.color).toBe('rgb(82, 196, 26)')
  })

  it('零与 undefined 都不渲染角标', () => {
    const { unmount } = render(<DashboardCards stats={STATS} alertTrendDelta={0} />)
    expect(screen.queryByLabelText('arrow-up')).toBeNull()
    expect(screen.queryByLabelText('arrow-down')).toBeNull()
    unmount()

    render(<DashboardCards stats={STATS} />)
    expect(screen.queryByLabelText('arrow-up')).toBeNull()
    expect(screen.queryByLabelText('arrow-down')).toBeNull()
  })
})
