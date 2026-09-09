// AssetTimeline.test.tsx — 资产诊断时间线页（P0-1）
// W1：此前 queryFn 内 `?? MOCK_TIMELINE` + `catch { return MOCK_TIMELINE }` 与渲染层
// `data ?? MOCK_TIMELINE` / `tl.summary ?? MOCK_SUMMARY` 三重兜底，isError 恒 false ——
// 接口挂了照常画出 4 条虚构事件，运维会当真实故障历史排查。
// W2：时间列此前用 toLocaleString('zh-CN', {hour12:false})。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'

// vi.hoisted：mock 工厂在 import 期被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  override: {} as Record<string, unknown>,
  refetch: vi.fn(),
}))

// 事件标题/资产名刻意与已删除的 MOCK_* 不重名（mock 里有 CPU 使用率超阈值 / mock-asset），
// 这样「虚构事件必须消失」的断言才有区分度。
// 时间串不带时区偏移 → dayjs 按本地解析，断言与 CI 时区无关
const TL = {
  asset: { id: 'asset-1', name: 'web-server-01', asset_type: 'server', status: 'active' },
  events: [
    {
      ts: '2026-02-14T10:00:00',
      kind: 'alert',
      sub_kind: 'triggered',
      severity: 4,
      title: '内存使用率超阈值',
      description: 'Warning · 内存持续 5 分钟 > 90%',
      ref_id: 'a1',
      ref_table: 'alerts',
    },
    {
      ts: '2026-02-14T10:05:00',
      kind: 'ticket',
      sub_kind: 'created',
      severity: 0,
      title: '服务响应慢',
      ref_id: 't1',
      ref_table: 'tickets',
    },
    {
      ts: '2026-02-14T11:00:00',
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
}

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: () => ({
    data: TL,
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: h.refetch,
    ...h.override,
  }),
  queryKeys: { diagnostics: { timeline: (id: string, days: number) => ['diagnostics', 'timeline', id, days] } },
}))

import { AssetTimeline } from './AssetTimeline'

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/assets/asset-1/diagnostics']}>
      <Routes>
        <Route path="/assets/:id/diagnostics" element={<AssetTimeline />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  h.override = {}
  h.refetch.mockClear()
})

describe('AssetTimeline', () => {
  it('渲染资产名称 + 摘要 + 时间线', async () => {
    renderPage()
    // 摘要卡片标题
    expect(await screen.findByText('告警总数')).toBeInTheDocument()
    expect(screen.getByText('未处理告警')).toBeInTheDocument()
    expect(screen.getByText('工单总数')).toBeInTheDocument()
    expect(screen.getByText(/MTTR/)).toBeInTheDocument()
    // 事件都渲染（真实数据，非 MOCK_*）
    expect(screen.getByText('内存使用率超阈值')).toBeInTheDocument()
    expect(screen.getByText('服务响应慢')).toBeInTheDocument()
    expect(screen.getByText('资产上线')).toBeInTheDocument()
  })

  it('显示 sub_kind 中文标签', async () => {
    renderPage()
    await screen.findByText('内存使用率超阈值')
    expect(screen.getByText('触发')).toBeInTheDocument()
    expect(screen.getByText('创建')).toBeInTheDocument()
    expect(screen.getByText('上线')).toBeInTheDocument()
  })

  it('资产 type + status 显示 Tag', async () => {
    renderPage()
    await screen.findByText(/资产诊断/)
    expect(screen.getByText('server')).toBeInTheDocument()
    expect(screen.getByText('active')).toBeInTheDocument()
    expect(screen.getByText(/30 天/)).toBeInTheDocument()
  })

  it('W2：时间线 label 走 utils/time 统一格式（T 分隔 → 空格）', async () => {
    renderPage()
    expect(await screen.findByText('2026-02-14 10:00:00')).toBeInTheDocument()
    expect(screen.getByText('2026-02-14 10:05:00')).toBeInTheDocument()
  })

  it('W1：接口失败显示错误态 + 重试，不回落 MOCK_TIMELINE', () => {
    h.override = { data: undefined, isError: true, error: { response: { status: 500 } } }
    renderPage()
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    // 虚构事件/资产/摘要都必须消失（否则运维会当真实故障历史排查）
    expect(screen.queryByText('CPU 使用率超阈值')).toBeNull()
    expect(screen.queryByText('mock-asset')).toBeNull()
    expect(screen.queryByText('告警总数')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch).toHaveBeenCalled()
  })

  it('W1：200 + 空 data 不白屏（undefined 守卫），走「资产不存在」空态', () => {
    h.override = { data: undefined }
    expect(() => renderPage()).not.toThrow()
    expect(screen.getByText('资产不存在')).toBeInTheDocument()
  })

  it('W1：asset 缺失时走空态，不读 asset.name 崩', () => {
    h.override = { data: { events: [], summary: TL.summary } }
    expect(() => renderPage()).not.toThrow()
    expect(screen.getByText('资产不存在')).toBeInTheDocument()
  })

  it('W1：events 非数组不崩，按空处理显示无事件', () => {
    h.override = { data: { asset: TL.asset, events: 'oops', summary: TL.summary } }
    expect(() => renderPage()).not.toThrow()
    expect(screen.getByText('该资产在 30 天窗口内无事件')).toBeInTheDocument()
  })

  it('W1：summary 缺失不崩（回落 0 值兜底 + MTTR 显示 —）', () => {
    h.override = { data: { asset: TL.asset, events: TL.events } }
    expect(() => renderPage()).not.toThrow()
    // 资产卡仍渲染，事件照常
    expect(screen.getByText('内存使用率超阈值')).toBeInTheDocument()
    // 摘要数字兜底为 0
    expect(screen.getByText('事件总数').nextElementSibling?.textContent).toBe('3')
  })
})
