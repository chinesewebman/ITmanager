// Runbook.test.tsx — 故障 Runbook 管理页（P2-1）
// W1：此前列表 queryFn 内 `.catch(() => ({ items: MOCK_RUNBOOKS..., total: 3 }))`、
// 推荐面板内 `.catch(() => MOCK_RECOMMEND)`，isError 恒 false —— 接口挂了照常列出
// 3 篇虚构 SOP，故障现场运维会照着不存在的手册操作。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

// vi.hoisted：mock 工厂在 import 期被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  overrides: {} as Record<string, Record<string, unknown>>,
  refetch: {} as Record<string, ReturnType<typeof vi.fn>>,
  apiSend: vi.fn(),
}))

// M4：mock apiSend 以断言提交按钮 loading（防连点）。apiGet 由 useApiQuery mock 短路，不触发。
vi.mock('../services/api', () => ({
  apiGet: vi.fn().mockResolvedValue({ items: [], total: 0 }),
  apiSend: (...args: unknown[]) => h.apiSend(...args),
}))

// 标题刻意与已删除的 MOCK_RUNBOOKS 不重名（mock 里是「MySQL 主从延迟告警处理」等），
// 这样「虚构 SOP 必须消失」的断言才有区分度。
const RUNBOOKS = [
  { id: 'r1', title: '主库复制延迟排查', asset_type: 'server', summary: '主从延迟 > 30s', severity: 4, enabled: true, tags: 'db,mysql' },
  { id: 'r2', title: '接入交换机端口 down', asset_type: 'switch', summary: '端口 down 紧急处理', severity: 5, enabled: true, tags: 'network' },
]
const RECOMMEND = [
  { id: 'r1', title: '主库复制延迟排查', asset_type: 'server', summary: '主从延迟 > 30s', severity: 4, enabled: true, tags: 'db,mysql' },
]

const DATA: Record<string, unknown> = { list: { items: RUNBOOKS, total: 2 }, recommend: RECOMMEND }

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: (key: unknown) => {
    const kind = JSON.stringify(key).includes('recommend') ? 'recommend' : 'list'
    if (!h.refetch[kind]) h.refetch[kind] = vi.fn()
    return {
      data: DATA[kind],
      isLoading: false,
      isError: false,
      error: undefined,
      refetch: h.refetch[kind],
      ...h.overrides[kind],
    }
  },
  queryKeys: {},
}))

import RunbookList, { RunbookRecommend } from './Runbook'

function renderList() {
  return render(
    <MemoryRouter>
      <RunbookList />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  h.overrides = {}
  for (const k of ['list', 'recommend']) h.refetch[k]?.mockClear()
  h.apiSend.mockReset()
})

describe('Runbook', () => {
  it('渲染列表 + 标题 + 数据', () => {
    renderList()
    expect(screen.getByText('故障 Runbook')).toBeInTheDocument()
    expect(screen.getByText('主库复制延迟排查')).toBeInTheDocument()
    expect(screen.getByText('接入交换机端口 down')).toBeInTheDocument()
  })

  // M1：标题体系统一——原手写 Space 标题（Text strong，非 heading），改为 PageHeader h4。
  it('M1：页面标题统一到 PageHeader（h4「故障 Runbook」）', () => {
    renderList()
    expect(screen.getByRole('heading', { level: 4, name: '故障 Runbook' })).toBeInTheDocument()
  })

  // M2：表格排序——此前全站零 sorter，用户无法点击表头排序。
  // Runbook 给标题/类型/严重度/启用加前端本地排序（标签多值串不加）；这里验证核心的严重度数值排序。
  it('M2：严重度列可排序（点击表头后按 severity 数值升序重排）', async () => {
    h.overrides.list = {
      data: {
        items: [
          { id: 'r1', title: '接入交换机端口 down', asset_type: 'switch', severity: 5, enabled: true, tags: 'network' },
          { id: 'r2', title: '主库复制延迟排查', asset_type: 'server', severity: 4, enabled: true, tags: 'db,mysql' },
        ],
        total: 2,
      },
    }
    const { container } = renderList()
    const rowTexts = () =>
      Array.from(container.querySelectorAll('tbody tr[data-row-key]')).map(
        (r) => r.textContent ?? '',
      )

    // 初始顺序 = dataSource 顺序：接入交换机（severity 5）在前
    expect(rowTexts()[0]).toContain('接入交换机端口 down')

    // 点「严重度」表头升序 → severity 4 < 5，主库复制延迟排查排前
    fireEvent.click(screen.getAllByText('严重度')[0])
    await waitFor(() => {
      expect(rowTexts()[0]).toContain('主库复制延迟排查')
    })
  })

  it('显示资产类型 tag + 严重度 tag', () => {
    renderList()
    expect(screen.getAllByText('server').length).toBeGreaterThan(0)
    // SeverityTag 默认显示 'P4 严重' / 'P5 灾难', 用 regex 匹配
    expect(screen.getAllByText(/P4/).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/P5/).length).toBeGreaterThan(0)
  })

  it('W1：列表失败显示错误态 + 重试，不回落 MOCK_RUNBOOKS', () => {
    h.overrides.list = { data: undefined, isError: true, error: { response: { status: 500 } } }
    renderList()
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    // 虚构 SOP 与真实列表都必须消失
    expect(screen.queryByText('MySQL 主从延迟告警处理')).toBeNull()
    expect(screen.queryByText('磁盘空间不足')).toBeNull()
    expect(screen.queryByText('主库复制延迟排查')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch.list).toHaveBeenCalled()
  })

  it('W1：列表为空时显示空态', () => {
    h.overrides.list = { data: { items: [], total: 0 } }
    renderList()
    expect(screen.getByText('暂无 Runbook')).toBeInTheDocument()
    expect(screen.queryByText('主库复制延迟排查')).toBeNull()
  })

  it('W1：items 形状异常（非数组）按空处理，不崩也不回落', () => {
    h.overrides.list = { data: { items: 'oops', total: 1 } }
    expect(() => renderList()).not.toThrow()
    expect(screen.getByText('暂无 Runbook')).toBeInTheDocument()
  })

  it('推荐面板正常显示推荐项', () => {
    render(
      <MemoryRouter>
        <RunbookRecommend assetType="server" severity={4} />
      </MemoryRouter>,
    )
    expect(screen.getByText('主库复制延迟排查')).toBeInTheDocument()
  })

  it('推荐为空时提示无推荐', () => {
    h.overrides.recommend = { data: [] }
    render(
      <MemoryRouter>
        <RunbookRecommend assetType="server" severity={4} />
      </MemoryRouter>,
    )
    expect(screen.getByText('无推荐 Runbook')).toBeInTheDocument()
  })

  it('W1：推荐接口失败显示错误态 + 重试，不回落 MOCK_RECOMMEND', () => {
    h.overrides.recommend = { data: undefined, isError: true, error: { response: { status: 500 } } }
    render(
      <MemoryRouter>
        <RunbookRecommend assetType="server" severity={4} />
      </MemoryRouter>,
    )
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    expect(screen.queryByText('MySQL 主从延迟告警处理')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch.recommend).toHaveBeenCalled()
  })

  it('W1：推荐接口形状异常（非数组）不崩', () => {
    h.overrides.recommend = { data: { oops: true } }
    expect(() =>
      render(
        <MemoryRouter>
          <RunbookRecommend assetType="server" severity={4} />
        </MemoryRouter>,
      ),
    ).not.toThrow()
    expect(screen.getByText('无推荐 Runbook')).toBeInTheDocument()
  })

  // M4：提交按钮 loading（范本 AssetFormModal confirmLoading）。此前 Modal 无 okText 也无 loading，
  // 接口慢时连点「OK」会重复创建同一条 Runbook。
  it('M4：新建 Runbook 提交中 OK 按钮 loading 且防连点（apiSend 只调一次）', async () => {
    let resolveSend!: (v: unknown) => void
    h.apiSend.mockImplementationOnce(() => new Promise((resolve) => { resolveSend = resolve }))
    renderList()

    fireEvent.click(screen.getByRole('button', { name: /新\s*建/ }))
    fireEvent.change(screen.getByPlaceholderText('如: MySQL 主从延迟告警处理'), { target: { value: '测试手册' } })

    const okBtn = screen.getByRole('button', { name: /OK|确\s*定/ })
    fireEvent.click(okBtn)

    await waitFor(() => {
      expect(okBtn).toHaveClass('ant-btn-loading')
    })

    fireEvent.click(okBtn)
    expect(h.apiSend).toHaveBeenCalledTimes(1)

    resolveSend(undefined)
  })

  // M5：危险操作确认统一——title 带对象名 + okText="删除" + okButtonProps danger
  // （范本 AssetTable Popconfirm 四件套）。此前 title 只有「确定删除?」不带对象名，无 danger。
  it('M5：删除确认框带对象名 + danger 确认按钮', async () => {
    h.apiSend.mockResolvedValue(undefined)
    renderList()

    // 第一行（主库复制延迟排查）的删除按钮
    fireEvent.click(screen.getAllByRole('button', { name: /删\s*除/ })[0])

    // 确认框 title 带对象名
    const title = await screen.findByText(/确认删除 Runbook「主库复制延迟排查」/)
    expect(title).toBeInTheDocument()

    // 确认按钮 danger 类型（antd v5 用 ant-btn-dangerous class）
    const popover = title.closest('.ant-popover') as HTMLElement
    const okBtn = within(popover).getByRole('button', { name: /删\s*除/ })
    expect(okBtn).toHaveClass('ant-btn-dangerous')

    // 确认后调用 DELETE
    fireEvent.click(okBtn)
    await waitFor(() => {
      expect(h.apiSend).toHaveBeenCalledWith('DELETE', '/runbooks/r1')
    })
  })

  // M11：Drawer/推荐面板严重度此前用 Tag color={severity>=4?'red':'orange'} 两档，
  // 与列表列 SeverityTag 权威 6 档不一致。统一到 SeverityTag（P5→紫红「灾难」、P4→红「严重」）。
  it('M11：Drawer 严重度统一到 SeverityTag（P5 紫红「灾难」，非旧「P5」红）', () => {
    renderList()
    // 第二行（接入交换机端口 down，severity=5）的「查看」打开 Drawer
    fireEvent.click(screen.getAllByRole('button', { name: /查\s*看/ })[1])
    // 列表列 + Drawer 各一个「P5 灾难」（旧代码 Drawer 是纯「P5」，列表是「P5 灾难」→ 只有 1 个）
    const tags = screen.getAllByText('P5 灾难')
    expect(tags.length).toBe(2)
    tags.forEach((t) => expect(t.closest('.ant-tag')).toHaveClass('ant-tag-magenta'))
  })

  it('M11：推荐面板严重度统一到 SeverityTag（「P4 严重」，非旧「P4」）', () => {
    render(
      <MemoryRouter>
        <RunbookRecommend assetType="server" severity={4} />
      </MemoryRouter>,
    )
    const tag = screen.getByText('P4 严重')
    expect(tag.closest('.ant-tag')).toHaveClass('ant-tag-red')
  })
})
