// AssetTimeline.test.tsx — 资产诊断时间线页（P0-1）
//
// 跟其他 page test 风格一致：mock useApiQuery 直接给 mock 数据，
// 断言标题 / 摘要 / 时间线渲染

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'

// mock useApiQuery 让 fetch 不被实际调用，直接返回 mock 数据
// 用 vi.hoisted 让 mock data 提到模块顶层
const { mockTimelineData } = vi.hoisted(() => ({
  mockTimelineData: {
    asset: { id: 'asset-1', name: 'web-server-01', asset_type: 'server', status: 'active' },
    events: [
      {
        ts: new Date(Date.now() - 30 * 60_000).toISOString(),
        kind: 'alert',
        sub_kind: 'triggered',
        severity: 4,
        title: 'CPU 使用率超阈值',
        description: 'Warning · CPU 持续 5 分钟 > 90%',
        ref_id: 'a1',
        ref_table: 'alerts',
      },
      {
        ts: new Date(Date.now() - 25 * 60_000).toISOString(),
        kind: 'alert',
        sub_kind: 'acknowledged',
        severity: 0,
        title: '已确认告警',
        ref_id: 'a1',
        ref_table: 'alerts',
      },
      {
        ts: new Date(Date.now() - 2 * 3600_000).toISOString(),
        kind: 'ticket',
        sub_kind: 'created',
        severity: 0,
        title: '服务响应慢',
        ref_id: 't1',
        ref_table: 'tickets',
      },
      {
        ts: new Date(Date.now() - 24 * 3600_000).toISOString(),
        kind: 'status_change',
        sub_kind: 'online',
        severity: 0,
        title: '资产上线',
      },
    ],
    summary: {
      alert_count: 5,
      ticket_count: 1,
      open_alerts: 1,
      open_tickets: 0,
      mttr_seconds: 1800,
      link_down_count: 0,
      window_days: 30,
    },
  },
}))

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: () => ({
    data: mockTimelineData,
    isLoading: false,
    error: null,
  }),
  queryKeys: { diagnostics: { timeline: (id: string, days: number) => ['diagnostics', 'timeline', id, days] } },
}))

import { AssetTimeline } from './AssetTimeline'

beforeEach(() => {
  localStorage.clear()
})

describe('AssetTimeline', () => {
  it('渲染资产名称 + 摘要 + 时间线', async () => {
    render(
      <MemoryRouter initialEntries={['/assets/asset-1/diagnostics']}>
        <Routes>
          <Route path="/assets/:id/diagnostics" element={<AssetTimeline />} />
        </Routes>
      </MemoryRouter>,
    )
    // 摘要卡片标题
    expect(await screen.findByText('告警总数')).toBeTruthy()
    expect(await screen.findByText('未处理告警')).toBeTruthy()
    expect(await screen.findByText('工单总数')).toBeTruthy()
    expect(await screen.findByText(/MTTR/)).toBeTruthy()
    // 4 个 mock 事件都渲染
    expect(await screen.findByText('CPU 使用率超阈值')).toBeTruthy()
    expect(await screen.findByText('已确认告警')).toBeTruthy()
    expect(await screen.findByText('服务响应慢')).toBeTruthy()
    expect(await screen.findByText('资产上线')).toBeTruthy()
  })

  it('显示 sub_kind 中文标签', async () => {
    render(
      <MemoryRouter initialEntries={['/assets/asset-1/diagnostics']}>
        <Routes>
          <Route path="/assets/:id/diagnostics" element={<AssetTimeline />} />
        </Routes>
      </MemoryRouter>,
    )
    // 触发 / 已确认 / 创建 / 上线
    expect(await screen.findByText('触发')).toBeTruthy()
    expect(await screen.findByText('已确认')).toBeTruthy()
    expect(await screen.findByText('创建')).toBeTruthy()
    expect(await screen.findByText('上线')).toBeTruthy()
  })

  it('资产 type + status 显示 Tag', async () => {
    render(
      <MemoryRouter initialEntries={['/assets/asset-1/diagnostics']}>
        <Routes>
          <Route path="/assets/:id/diagnostics" element={<AssetTimeline />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(await screen.findByText('server')).toBeTruthy()
    expect(await screen.findByText('active')).toBeTruthy()
    expect(await screen.findByText(/查询窗口/)).toBeTruthy()
    expect(await screen.findByText(/30 天/)).toBeTruthy()
  })
})
