// M25 步骤 6：经手记录时间线组件（docs/FIX-PLAN-TICKET-HISTORY.md §2.8）。
//
// 分组、排序、取值渲染的正确性在 utils/ticketHistory.test.ts 里守（纯函数）。
// 这里只守组件独有的三件事：空态文案不能把「没记录」说成「没改过」、
// 历史加载失败不许连累票面信息、加载更多要真的接在后面。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

const h = vi.hoisted(() => ({
  query: { data: undefined, isLoading: false, isError: false, error: undefined } as Record<
    string,
    unknown
  >,
  pages: {} as Record<number, unknown[]>,
  historyCalls: [] as unknown[][],
}))

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: () => ({ ...h.query, refetch: vi.fn() }),
  queryKeys: { tickets: { history: (id: string) => ['tickets', 'history', id] } },
}))

vi.mock('../services/api', () => ({
  ticketApi: {
    // 按请求的 page 回不同的行 —— 只回一页的话，「追加」与「替换」写出同样的
    // 结果，那条断言就守不住任何东西（见下方两次点击的用例）。
    history: (...args: unknown[]) => {
      h.historyCalls.push(args)
      const page = (args[1] as { page?: number } | undefined)?.page ?? 1
      return Promise.resolve({ data: { data: { items: h.pages[page] ?? [], page, size: 50 } } })
    },
  },
}))

import { TicketHistoryTimeline } from './TicketHistoryTimeline'

const BIRTH = {
  id: 'h1',
  ticket_id: 't1',
  batch_id: 'b1',
  kind: 'created',
  field_name: null,
  old_value: null,
  new_value: null,
  actor_id: 'u1',
  actor_name: '燕如',
  source: '',
  request_id: '',
  created_at: '2026-09-11T10:00:00',
}

const UPDATE_ROW = {
  ...BIRTH,
  id: 'h2',
  batch_id: 'b2',
  kind: 'updated',
  field_name: 'status',
  old_value: 'open',
  new_value: 'resolved',
  actor_name: '张三',
  created_at: '2026-09-11T11:00:00',
}

function withRows(items: unknown[], total = items.length) {
  h.query = { data: { items, total }, isLoading: false, isError: false, error: undefined }
}

beforeEach(() => {
  h.historyCalls = []
  h.pages = {}
  h.query = { data: undefined, isLoading: false, isError: false, error: undefined }
})

describe('TicketHistoryTimeline', () => {
  it('出生批次说「创建了这张工单」，更新批次列出字段的新旧值', () => {
    withRows([UPDATE_ROW, BIRTH])

    render(<TicketHistoryTimeline ticketId="t1" />)

    expect(screen.getByText('创建了这张工单')).toBeInTheDocument()
    expect(screen.getByText('修改了 1 个字段')).toBeInTheDocument()
    // 字段名走字典、值走枚举字典 —— 读历史的人看的该是「状态：新建 → 已解决」
    expect(screen.getByText('状态：')).toBeInTheDocument()
    expect(screen.getByText('新建')).toBeInTheDocument()
    expect(screen.getByText('已解决')).toBeInTheDocument()
  })

  it('没有记录时如实说明是「本功能上线前的改动未记录」，不是「没被改过」', () => {
    withRows([])

    render(<TicketHistoryTimeline ticketId="t1" />)

    // 表是 M25 才建的，所有老票都没历史 —— 这是常态。文案若写成「无记录」，
    // 读的人会理解成「这张票从没被改过」，而那是错的。
    expect(screen.getByText(/本功能上线前的改动未记录/)).toBeInTheDocument()
  })

  it('加载中不渲染空态（空态在 loading 期间是假话）', () => {
    h.query = { data: undefined, isLoading: true, isError: false, error: undefined }

    render(<TicketHistoryTimeline ticketId="t1" />)

    expect(screen.getByText('加载中…')).toBeInTheDocument()
    expect(screen.queryByText(/本功能上线前的改动未记录/)).not.toBeInTheDocument()
  })

  it('历史加载失败只落在本区块，不抛出', () => {
    h.query = {
      data: undefined,
      isLoading: false,
      isError: true,
      error: { response: { status: 500 } },
    }

    render(<TicketHistoryTimeline ticketId="t1" />)

    expect(screen.getByText(/服务端暂时不可用/)).toBeInTheDocument()
  })

  it('还有更早的记录时给「加载更多」，点击后接在已显示的行后面而不是覆盖', async () => {
    withRows([UPDATE_ROW], 3)
    h.pages[2] = [{ ...UPDATE_ROW, id: 'h3', batch_id: 'b0', actor_name: '李四' }]
    h.pages[3] = [{ ...UPDATE_ROW, id: 'h4', batch_id: 'b-1', actor_name: '王五' }]

    render(<TicketHistoryTimeline ticketId="t1" />)

    const btn = screen.getByRole('button', { name: /加载更多/ })
    expect(btn).toHaveTextContent('还有 2 条')

    fireEvent.click(btn)
    await waitFor(() => expect(screen.getByText('李四')).toBeInTheDocument())

    // 点第二次才是「追加 vs 替换」的判据：此时 extra 里已经有第 2 页的行，
    // 写成 setExtra(next.items) 的话李四会消失。只点一次的话 prev 是空数组，
    // 两种写法结果一样 —— 那种断言守不住任何东西。
    fireEvent.click(screen.getByRole('button', { name: /加载更多/ }))
    await waitFor(() => expect(screen.getByText('王五')).toBeInTheDocument())

    expect(screen.getByText('李四')).toBeInTheDocument()
    expect(screen.getByText('张三')).toBeInTheDocument()
    // 每次只往后拉一页，不重复拉同一页
    expect(h.historyCalls).toEqual([
      ['t1', { page: 2, page_size: 50 }],
      ['t1', { page: 3, page_size: 50 }],
    ])
    // 已经拉完 → 按钮消失
    await waitFor(() => expect(screen.queryByRole('button', { name: /加载更多/ })).toBeNull())
  })
})
