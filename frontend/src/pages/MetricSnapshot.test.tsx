// MetricSnapshot.test.tsx — 指标快照查看页（P2-2）
// W1：此前 queryFn 内 `catch { return MOCK_LATEST }`，isError 恒 false ——
// 接口挂了页面照常画出 5 个虚构的 cpu.user 采样点，运维会据此判断「CPU 正常」。
// W2：时间列此前用无 locale 的 new Date(v).toLocaleString()。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { message } from 'antd'

// vi.hoisted：mock 工厂在 import 期被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  override: {} as Record<string, unknown>,
  refetch: vi.fn(),
}))

// 时间串不带时区偏移 → dayjs 按本地解析，断言与 CI 时区无关
const ITEMS = [
  { id: '1', asset_id: 'asset-1', key: 'cpu.user', value: 45.2, ts: '2026-02-14T10:00:00' },
  { id: '2', asset_id: 'asset-1', key: 'cpu.user', value: 60.3, ts: '2026-02-14T10:01:00' },
]

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: () => ({
    data: ITEMS,
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: h.refetch,
    ...h.override,
  }),
  queryKeys: {},
}))

import MetricSnapshotList from './MetricSnapshot'

function renderPage() {
  return render(
    <MemoryRouter>
      <MetricSnapshotList />
    </MemoryRouter>,
  )
}

/** 触发一次查询（填入 asset_id 后点「查询」）。 */
function runQuery(assetId = 'asset-1') {
  fireEvent.change(screen.getByPlaceholderText('uuid'), { target: { value: assetId } })
  fireEvent.click(screen.getByRole('button', { name: /查\s*询/ }))
}

/** 统计卡值：取标签的后一个兄弟节点（数值 div 在标签之后）。 */
function statValue(label: string): string | undefined {
  return screen.getByText(label).nextElementSibling?.textContent ?? undefined
}

beforeEach(() => {
  h.override = {}
  h.refetch.mockClear()
})

describe('MetricSnapshot', () => {
  it('渲染标题 + 查询表单', () => {
    renderPage()
    expect(screen.getByText('指标快照')).toBeInTheDocument()
    expect(screen.getByText('Asset ID')).toBeInTheDocument()
    expect(screen.getByText('Key')).toBeInTheDocument()
  })

  it('初始状态提示输入参数，不渲染统计卡', () => {
    renderPage()
    expect(screen.getByText('输入 asset_id + key 后点击查询')).toBeInTheDocument()
    expect(screen.queryByText('最新值')).toBeNull()
  })

  it('未填 asset_id 直接查询 → 提示且不发请求（仍停在初始态）', () => {
    renderPage()
    fireEvent.click(screen.getByRole('button', { name: /查\s*询/ }))
    // setup.ts 全局 mock 了 antd message → 断言调用而不是 DOM 文案
    expect(message.warning).toHaveBeenCalledWith('请输入 asset_id')
    expect(screen.getByText('输入 asset_id + key 后点击查询')).toBeInTheDocument()
    expect(screen.queryByText('最新值')).toBeNull()
  })

  it('查询后渲染统计卡与表格（真实推导，不回落 mock）', () => {
    renderPage()
    runQuery()
    // 45.2 / 60.3 → 最新 45.20、平均 52.75、最大 60.30、样本 2
    expect(statValue('最新值')).toBe('45.20')
    expect(statValue('平均值')).toBe('52.75')
    expect(statValue('最大值')).toBe('60.30')
    expect(statValue('样本数')).toBe('2')
    // 表格行里的值 Tag 同样存在
    expect(screen.getAllByText('45.20')).toHaveLength(2)
    // 已删除的 MOCK_LATEST 里的 5 个采样点不得出现
    expect(screen.queryByText('48.10')).toBeNull()
    expect(screen.queryByText('55.00')).toBeNull()
  })

  it('W2：时间列走 utils/time 统一格式（T 分隔 → 空格）', () => {
    renderPage()
    runQuery()
    expect(screen.getByText('2026-02-14 10:00:00')).toBeInTheDocument()
    expect(screen.getByText('2026-02-14 10:01:00')).toBeInTheDocument()
  })

  // M2：表格排序——此前全站零 sorter，用户无法点击表头排序。
  // MetricSnapshot 给时间/Asset ID/Key/Value 加前端本地排序；这里验证核心的 Value 数值排序。
  it('M2：Value 列可排序（点击表头后按数值升序重排）', async () => {
    h.override = {
      data: [
        { id: '1', asset_id: 'asset-1', key: 'cpu.user', value: 60.3, ts: '2026-02-14T10:00:00' },
        { id: '2', asset_id: 'asset-1', key: 'cpu.user', value: 45.2, ts: '2026-02-14T10:01:00' },
      ],
    }
    const { container } = renderPage()
    runQuery()
    const rowTexts = () =>
      Array.from(container.querySelectorAll('tbody tr[data-row-key]')).map(
        (r) => r.textContent ?? '',
      )

    // 初始顺序 = dataSource 顺序：value 60.30 在前
    expect(rowTexts()[0]).toContain('60.30')

    // 点「Value」表头升序 → 45.20 < 60.30，45.20 排前
    fireEvent.click(screen.getAllByText('Value')[0])
    await waitFor(() => {
      expect(rowTexts()[0]).toContain('45.20')
    })
  })

  it('W1：查询失败时显示错误态 + 重试，不回落 MOCK_LATEST', () => {
    h.override = { data: undefined, isError: true, error: { response: { status: 500 } } }
    renderPage()
    runQuery()
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    // 关键回归断言：虚构采样点与由它推导的统计卡都必须消失
    expect(screen.queryByText('60.30')).toBeNull()
    expect(screen.queryByText('最新值')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch).toHaveBeenCalled()
  })

  it('W1：查询结果为空时显示空态', () => {
    h.override = { data: [] }
    renderPage()
    runQuery()
    expect(screen.getByText('暂无指标数据')).toBeInTheDocument()
    expect(statValue('样本数')).toBe('0')
  })
})
