// M50 G-UI-Tickets：工单列表的行内操作与更新时间列。
//
// TicketTable 本身是纯展示（取数、分页、错误态都在 Tickets.tsx 那层），所以这里
// 只钉两件会真的坏掉的事：更新时间列到底在不在、相对时间有没有真的被格式化；
// 以及 [更多操作] 四项各自把**整行记录**交给了哪个回调（交错了行 = 改错工单）。
import '@testing-library/jest-dom'
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { TicketTable, type Ticket } from './TicketTable'

// 相对时间用「此刻」倒推，避免依赖系统时区/固定时钟；3 天前的档位在 zh-cn 下是 "3 天前"。
const ROW: Ticket = {
  id: '1',
  title: '服务器磁盘空间不足',
  priority: 'high',
  status: 'open',
  requester: '张三',
  assignee: '李四',
  created_at: '2026-02-14T10:00:00',
  updated_at: new Date(Date.now() - 3 * 86_400_000).toISOString(),
}

describe('TicketTable', () => {
  it('更新时间列在表头，且渲染成相对时间（这一列回答的是「多久没动了」）', () => {
    render(<TicketTable data={[ROW]} loading={false} onView={vi.fn()} />)

    expect(screen.getByText('更新时间')).toBeInTheDocument()
    expect(screen.getByText('3 天前')).toBeInTheDocument()
  })

  it('[更多操作] 下拉含四项，点击任一项把整行记录交给对应回调', async () => {
    const onView = vi.fn()
    const onAssign = vi.fn()
    const onChangePriority = vi.fn()
    const onClose = vi.fn()

    render(
      <TicketTable
        data={[ROW]}
        loading={false}
        onView={onView}
        onAssign={onAssign}
        onChangePriority={onChangePriority}
        onClose={onClose}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '更多操作' }))

    for (const label of ['查看详情', '改派', '改优先级', '关单']) {
      expect(await screen.findByText(label)).toBeInTheDocument()
    }

    // 每一项都带整行记录 —— 传错行就是改错工单
    fireEvent.click(screen.getByText('改派'))
    expect(onAssign).toHaveBeenCalledWith(ROW)

    fireEvent.click(screen.getByRole('button', { name: '更多操作' }))
    fireEvent.click(await screen.findByText('关单'))
    await waitFor(() => expect(onClose).toHaveBeenCalledWith(ROW))

    // 左边原有的 [详情] 按钮保持不变（既存用例与本轮都不该动它）
    fireEvent.click(screen.getByRole('button', { name: '详情' }))
    expect(onView).toHaveBeenCalledWith(ROW)
  })
})
