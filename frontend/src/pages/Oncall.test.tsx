// Oncall.test.tsx — 值班 + 升级管理页（P1-2）
// W1：三个 tab 此前都在 queryFn 内 `catch { return MOCK_* }` + `data ?? MOCK_*`
// 双重兜底 → isError 恒 false，接口挂了页面照常显示虚构的值班人/值班组/升级策略。
// W2：当前值班时间此前用无 locale 的 toLocaleString('zh-CN')。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { message } from 'antd'

// vi.hoisted：mock 工厂在 import 期被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  overrides: {} as Record<string, Record<string, unknown>>,
  refetch: {} as Record<string, ReturnType<typeof vi.fn>>,
  apiSend: vi.fn(),
}))

// M4：mock apiSend 以断言提交按钮 loading（防连点）。apiGet 由 useApiQuery mock 短路，不触发。
vi.mock('../services/api', () => ({
  apiGet: vi.fn().mockResolvedValue([]),
  apiSend: (...args: unknown[]) => h.apiSend(...args),
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
  h.apiSend.mockReset()
})

describe('Oncall', () => {
  it('渲染 3 个 tab', () => {
    renderOncall()
    expect(screen.getByText('当前值班')).toBeInTheDocument()
    expect(screen.getByText('值班组')).toBeInTheDocument()
    expect(screen.getByText('升级策略')).toBeInTheDocument()
  })

  // M1：标题体系统一——Oncall 原 Tabs 页无可见标题（只 useDocumentTitle 改浏览器标题），
  // 用户进来不知道这是值班管理页。补 PageHeader title（h4）。
  it('M1：页面标题统一到 PageHeader（h4「值班管理」）', () => {
    renderOncall()
    expect(screen.getByRole('heading', { level: 4, name: '值班管理' })).toBeInTheDocument()
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

  // M2：表格排序——此前全站零 sorter，用户无法点击表头排序。
  // 值班组表给名称/时区/启用/说明加前端本地排序；这里验证核心的名称字符串排序。
  it('M2：值班组「名称」列可排序（点击表头后按字母升序重排）', async () => {
    h.overrides.schedules = {
      data: [
        { id: 's1', name: 'ops-team', description: '运维组', enabled: true },
        { id: 's2', name: 'dev-team', description: '研发组', enabled: true },
      ],
    }
    const { container } = renderOncall()
    openTab('值班组')
    await screen.findByText('ops-team')
    const rowTexts = () =>
      Array.from(container.querySelectorAll('tbody tr[data-row-key]')).map(
        (r) => r.textContent ?? '',
      )

    // 初始顺序 = dataSource 顺序：ops-team 在前
    expect(rowTexts()[0]).toContain('ops-team')

    // 点「名称」表头升序 → dev-team < ops-team，dev-team 排前
    fireEvent.click(screen.getAllByText('名称')[0])
    await waitFor(() => {
      expect(rowTexts()[0]).toContain('dev-team')
    })
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

  // M2：表格排序——升级策略表给名称/层级数/启用加前端本地排序；这里验证核心的层级数数值排序。
  it('M2：升级策略「层级数」列可排序（点击表头后按层级数升序重排）', async () => {
    h.overrides.policies = {
      data: [
        { id: 'p1', name: 'critical', enabled: true, levels: [
          { level: 1, target_type: 'user', target_id: 'alice', wait_minutes: 5, notify_methods: 'email' },
          { level: 2, target_type: 'user', target_id: 'bob', wait_minutes: 10, notify_methods: 'email' },
        ] },
        { id: 'p2', name: 'info', enabled: true, levels: [
          { level: 1, target_type: 'user', target_id: 'carol', wait_minutes: 5, notify_methods: 'email' },
        ] },
      ],
    }
    const { container } = renderOncall()
    openTab('升级策略')
    await screen.findByText('critical')
    const rowTexts = () =>
      Array.from(container.querySelectorAll('tbody tr[data-row-key]')).map(
        (r) => r.textContent ?? '',
      )

    // 初始顺序 = dataSource 顺序：critical（2 级）在前
    expect(rowTexts()[0]).toContain('critical')

    // 点「层级数」表头升序 → 1 级 < 2 级，info 排前
    fireEvent.click(screen.getAllByText('层级数')[0])
    await waitFor(() => {
      expect(rowTexts()[0]).toContain('info')
    })
  })

  // M4：提交按钮 loading（范本 AssetFormModal confirmLoading）。此前两个 tab 的 onOk={onSubmit}
  // 均无 loading，接口慢时连点「保存」会重复创建值班组/升级策略。
  it('M4：值班组提交中保存按钮 loading 且防连点（apiSend 只调一次）', async () => {
    let resolveSend!: (v: unknown) => void
    h.apiSend.mockImplementationOnce(() => new Promise((resolve) => { resolveSend = resolve }))
    renderOncall()
    openTab('值班组')

    fireEvent.click(screen.getByRole('button', { name: /新\s*建/ }))
    fireEvent.change(screen.getByRole('textbox', { name: '名称' }), { target: { value: 'team-a' } })

    const saveBtn = screen.getByRole('button', { name: /保\s*存/ })
    fireEvent.click(saveBtn)

    // 请求 pending → 按钮进入 loading
    await waitFor(() => {
      expect(saveBtn).toHaveClass('ant-btn-loading')
    })

    // loading 期间连点不应触发第二次提交
    fireEvent.click(saveBtn)
    fireEvent.click(saveBtn)
    expect(h.apiSend).toHaveBeenCalledTimes(1)

    resolveSend(undefined)
  })

  it('M4：升级策略提交中保存按钮 loading 且防连点', async () => {
    let resolveSend!: (v: unknown) => void
    h.apiSend.mockImplementationOnce(() => new Promise((resolve) => { resolveSend = resolve }))
    renderOncall()
    openTab('升级策略')

    fireEvent.click(screen.getByRole('button', { name: /新\s*建/ }))
    fireEvent.change(screen.getByRole('textbox', { name: '名称' }), { target: { value: 'policy-a' } })

    const saveBtn = screen.getByRole('button', { name: /保\s*存/ })
    fireEvent.click(saveBtn)

    await waitFor(() => {
      expect(saveBtn).toHaveClass('ant-btn-loading')
    })

    fireEvent.click(saveBtn)
    expect(h.apiSend).toHaveBeenCalledTimes(1)

    resolveSend(undefined)
  })

  // M5：危险操作确认统一——title 带对象名 + okText="删除" + okButtonProps danger
  // （范本 AssetTable Popconfirm 四件套）。此前两处 title 都只有「删除？」不带对象名，无 danger。
  it('M5：删除值班组确认框带对象名 + danger 确认按钮', async () => {
    h.apiSend.mockResolvedValue(undefined)
    renderOncall()
    openTab('值班组')

    // 第一行（dev-team）的删除按钮
    fireEvent.click(screen.getAllByRole('button', { name: /删\s*除/ })[0])

    const title = await screen.findByText(/确认删除值班组「dev-team」/)
    expect(title).toBeInTheDocument()

    const popover = title.closest('.ant-popover') as HTMLElement
    const okBtn = within(popover).getByRole('button', { name: /删\s*除/ })
    expect(okBtn).toHaveClass('ant-btn-dangerous')

    fireEvent.click(okBtn)
    await waitFor(() => {
      expect(h.apiSend).toHaveBeenCalledWith('DELETE', '/oncall/schedules/s1')
    })
  })

  it('M5：删除升级策略确认框带对象名 + danger 确认按钮', async () => {
    h.apiSend.mockResolvedValue(undefined)
    renderOncall()
    openTab('升级策略')

    // 唯一一行（critical）的删除按钮
    fireEvent.click(screen.getAllByRole('button', { name: /删\s*除/ })[0])

    const title = await screen.findByText(/确认删除升级策略「critical」/)
    expect(title).toBeInTheDocument()

    const popover = title.closest('.ant-popover') as HTMLElement
    const okBtn = within(popover).getByRole('button', { name: /删\s*除/ })
    expect(okBtn).toHaveClass('ant-btn-dangerous')

    fireEvent.click(okBtn)
    await waitFor(() => {
      expect(h.apiSend).toHaveBeenCalledWith('DELETE', '/oncall/policies/p1')
    })
  })

  // M6：required 无 message → 统一「请输入/请选择 XXX」。此前「名称」required 无 message，
  // antd 默认英文「${label} is required」，语气不一致。
  it('M6：值班组名称必填带中文提示', async () => {
    renderOncall()
    openTab('值班组')
    fireEvent.click(screen.getByRole('button', { name: /新\s*建/ }))
    // 不填名称直接保存 → 触发校验
    fireEvent.click(screen.getByRole('button', { name: /保\s*存/ }))
    expect(await screen.findByText('请输入名称')).toBeInTheDocument()
  })

  it('M6：升级策略名称必填带中文提示', async () => {
    renderOncall()
    openTab('升级策略')
    fireEvent.click(screen.getByRole('button', { name: /新\s*建/ }))
    fireEvent.click(screen.getByRole('button', { name: /保\s*存/ }))
    expect(await screen.findByText('请输入名称')).toBeInTheDocument()
  })

  // M7：升级策略 Levels 是 JSON textarea，非法 JSON 时 JSON.parse 抛 SyntaxError。
  // 此前 catch 直接 message.error(e.message) → 英文 "Unexpected token..." 技术报错。
  it('M7：升级策略 Levels 非法 JSON 提示友好中文（apiSend 不调用）', async () => {
    vi.mocked(message.error).mockClear()
    renderOncall()
    openTab('升级策略')
    fireEvent.click(screen.getByRole('button', { name: /新\s*建/ }))
    fireEvent.change(screen.getByRole('textbox', { name: '名称' }), { target: { value: 'policy-x' } })
    fireEvent.change(screen.getByLabelText('Levels (JSON 数组)'), { target: { value: '{bad json' } })
    fireEvent.click(screen.getByRole('button', { name: /保\s*存/ }))

    await waitFor(() => {
      expect(message.error).toHaveBeenCalledWith('Levels JSON 格式错误，请检查后重试')
    })
    expect(h.apiSend).not.toHaveBeenCalled()
  })
})
