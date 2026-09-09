// AlertSuppressions.test.tsx — 抑制规则管理页（P0-2）
// W1：此前 `catch { return MOCK_RULES }` + `data: rules = MOCK_RULES` 双重兜底，
// isError 恒 false —— 接口挂了页面照常列出虚构的抑制规则，运维会误以为
// 「db-* 的告警已经被抑制了」，而实际一条规则都不存在。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

// vi.hoisted：mock 工厂在 import 期被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  override: {} as Record<string, unknown>,
  refetch: vi.fn(),
  apiSend: vi.fn(),
}))

// M4：mock apiSend 以断言提交按钮 loading（防连点）。apiGet 由 useApiQuery mock 短路，不触发。
vi.mock('../services/api', () => ({
  apiGet: vi.fn().mockResolvedValue([]),
  apiSend: (...args: unknown[]) => h.apiSend(...args),
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
  h.apiSend.mockReset()
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

  // M4：提交按钮 loading（范本 AssetFormModal confirmLoading）。此前 onOk={onSubmit} 无 loading，
  // 接口慢时用户连点「保存」会重复创建同一条规则。
  it('M4：提交中保存按钮 loading 且防连点（apiSend 只调一次）', async () => {
    let resolveSend!: (v: unknown) => void
    h.apiSend.mockImplementationOnce(() => new Promise((resolve) => { resolveSend = resolve }))
    renderPage()

    // 打开新建弹窗，填必填项（severity_max / time_window_seconds / enabled 有默认值）
    fireEvent.click(screen.getByText(/新建抑制规则/))
    fireEvent.change(screen.getByPlaceholderText('如：抑制 db-* 警告'), { target: { value: '抑制 x-*' } })
    fireEvent.change(screen.getByPlaceholderText('如：db-*、web-*-prod、switch-core-01'), { target: { value: 'x-*' } })

    const saveBtn = screen.getByRole('button', { name: /保\s*存/ })
    fireEvent.click(saveBtn)

    // 请求 pending → 按钮进入 loading
    await waitFor(() => {
      expect(saveBtn).toHaveClass('ant-btn-loading')
    })

    // loading 期间连点不应触发第二次提交（antd Button loading 时 handleClick 短路）
    fireEvent.click(saveBtn)
    fireEvent.click(saveBtn)
    expect(h.apiSend).toHaveBeenCalledTimes(1)

    resolveSend(undefined)
  })

  // M5：危险操作确认统一——title 带对象名 + okText="删除" + okButtonProps danger
  // （范本 AssetTable Popconfirm 四件套）。此前 title 只有「确定删除？」不带对象名，无 danger。
  it('M5：删除确认框带对象名 + danger 确认按钮', async () => {
    h.apiSend.mockResolvedValue(undefined)
    renderPage()

    // 第一行（抑制 db-*）的删除按钮
    fireEvent.click(screen.getAllByRole('button', { name: /删\s*除/ })[0])

    // 确认框 title 带对象名
    const title = await screen.findByText(/确认删除规则「抑制 db-\*」/)
    expect(title).toBeInTheDocument()

    // 确认按钮 danger 类型（antd v5 用 ant-btn-dangerous class）
    const popover = title.closest('.ant-popover') as HTMLElement
    const okBtn = within(popover).getByRole('button', { name: /删\s*除/ })
    expect(okBtn).toHaveClass('ant-btn-dangerous')

    // 确认后调用 DELETE
    fireEvent.click(okBtn)
    await waitFor(() => {
      expect(h.apiSend).toHaveBeenCalledWith('DELETE', '/alert-suppressions/r1')
    })
  })

  // M11：severity_max 配色此前自造三档（v>=4红/v>=3橙/else蓝），与 SeverityTag 权威 6 档不一致
  // （P2 应为金，旧代码落 else 分支染蓝）。只统一颜色，label 保持纯数字（severity_max 是「≤N」阈值，非「P4 严重」标签）。
  it('M11：severity_max 配色统一到 SeverityTag 权威 6 档（P2→金，非旧蓝）', async () => {
    renderPage()
    await screen.findByText('抑制 db-*')
    // r2 severity_max=2 → SEVERITY_META[2] 金色；旧代码 v=2 落 'else' 分支染蓝
    const tag = screen.getByText('2').closest('.ant-tag') as HTMLElement
    expect(tag).toHaveClass('ant-tag-gold')
    expect(tag).not.toHaveClass('ant-tag-blue')
  })
})
