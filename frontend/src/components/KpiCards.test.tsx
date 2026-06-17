// KpiCards 组件测试
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { KpiCards, KPI_THRESHOLDS, type KPI } from './KpiCards'

// 抑制 chart 等内部可能的副作用
vi.mock('@ant-design/icons', () => ({
  ClockCircleOutlined: () => <span data-testid="icon-clock" />,
  EyeOutlined: () => <span data-testid="icon-eye" />,
  AlertOutlined: () => <span data-testid="icon-alert" />,
  CheckCircleOutlined: () => <span data-testid="icon-check" />,
  InfoCircleOutlined: () => <span data-testid="icon-info" />,
}))

describe('KpiCards', () => {
  it('渲染完整 KPI 数据', () => {
    const kpi: KPI = {
      mttr_seconds: 3600, // 1h
      mttd_seconds: 300, // 5m
      alert_density: 2.5,
      sla_closed_rate: 0.95,
      window_days: 7,
      resolved_alerts: 5,
      acked_alerts: 8,
      closed_tickets: 20,
      on_time_tickets: 19,
    }
    render(<KpiCards kpi={kpi} />)
    // 4 个指标标题
    expect(screen.getByText('MTTR (平均恢复)')).toBeTruthy()
    expect(screen.getByText('MTTD (平均检测)')).toBeTruthy()
    expect(screen.getByText('告警密度')).toBeTruthy()
    expect(screen.getByText('SLA 达成率')).toBeTruthy()
    // SLA 显示 95%
    expect(screen.getByText('95.0%')).toBeTruthy()
    // 窗口标识
    expect(screen.getByText(/最近 7 天/)).toBeTruthy()
  })

  it('无数据时显示 n/a', () => {
    const kpi: KPI = {
      mttr_seconds: null,
      mttd_seconds: null,
      alert_density: 0,
      sla_closed_rate: null,
      window_days: 7,
    }
    render(<KpiCards kpi={kpi} />)
    // 3 个 n/a + 1 个 0 alerts/day
    const nA = screen.getAllByText('n/a')
    expect(nA.length).toBe(3) // MTTR + MTTD + SLA
  })

  it('kpi=null 时显示无数据提示', () => {
    render(<KpiCards kpi={null} />)
    expect(screen.getByText('无 KPI 数据')).toBeTruthy()
  })

  it('formatDuration: 秒/分/时/天 正确', () => {
    // 通过 props 间接验证：MTTR=45 → "45s"，=120 → "2m"，=3700 → "1h1m"，=90000 → "1d1h"
    const cases: Array<[number, string]> = [
      [45, '45s'],
      [120, '2m'],
      [3700, '1h1m'],
      [90000, '1d1h'],
    ]
    for (const [secs, want] of cases) {
      const { unmount } = render(<KpiCards kpi={{ mttr_seconds: secs, alert_density: 0, window_days: 1 }} />)
      expect(screen.getByText(want)).toBeTruthy()
      unmount()
    }
  })

  it('KPI_THRESHOLDS 常量值正确（防止 v1.1 改动时回归）', () => {
    expect(KPI_THRESHOLDS.MTTR_RED_SEC).toBe(3600) // 1h
    expect(KPI_THRESHOLDS.MTTD_RED_SEC).toBe(600) // 10min
    expect(KPI_THRESHOLDS.ALERT_DENSITY_RED).toBe(5)
    expect(KPI_THRESHOLDS.SLA_TARGET).toBe(0.9)
  })

  it('MTTR 越阈值时显示未达标视觉（SLA 字段触发红色）', () => {
    // SLA 是唯一会加 Tag 标签的字段，用它来验证 threshold 触发
    const kpi: KPI = {
      mttr_seconds: 7200, // 2h，超过 1h 阈值
      mttd_seconds: 1200, // 20min，超过 10min 阈值
      alert_density: 8, // 超过 5/day
      sla_closed_rate: 0.5, // 低于 90% 阈值
      window_days: 7,
    }
    render(<KpiCards kpi={kpi} />)
    // 验证 4 个 Statistic 组件都渲染了
    expect(screen.getByText('MTTR (平均恢复)')).toBeTruthy()
    expect(screen.getByText('MTTD (平均检测)')).toBeTruthy()
    expect(screen.getByText('告警密度')).toBeTruthy()
    expect(screen.getByText('SLA 达成率')).toBeTruthy()
    // SLA < 90% 时显示 "未达标" tag
    expect(screen.getByText('未达标')).toBeTruthy()
  })
})
