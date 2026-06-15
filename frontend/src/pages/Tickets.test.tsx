// Tickets page smoke test
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import Tickets from './Tickets'

const mockTickets = [
  { id: '1', title: '服务器磁盘空间不足', priority: 'high', status: 'open', requester: '张三', assignee: '李四', created_at: '2026-02-14 10:00:00', updated_at: '2026-02-14 11:00:00' },
  { id: '2', title: '网络延迟过高', priority: 'critical', status: 'in_progress', requester: '王五', assignee: '李四', created_at: '2026-02-13 15:00:00', updated_at: '2026-02-14 09:00:00' },
]

vi.mock('../hooks/useApiQuery', () => ({
  // Tickets 直接 useApiQuery<Ticket[]> — data 就是 array
  useApiQuery: () => ({ data: mockTickets, isLoading: false, refetch: vi.fn() }),
  useApiMutation: () => ({ mutate: vi.fn(), mutateAsync: vi.fn() }),
  queryKeys: { tickets: { list: () => ['tickets', 'list'] } },
}))

describe('Tickets page', () => {
  it('渲染工单页 + 表格（mock 数据）', () => {
    render(<Tickets />)
    expect(screen.getByText('工单管理')).toBeInTheDocument()
    // TicketTable 显示 mock 工单标题
    expect(screen.getByText('服务器磁盘空间不足')).toBeInTheDocument()
    expect(screen.getByText('网络延迟过高')).toBeInTheDocument()
  })

  it('不 crash 渲染', () => {
    expect(() => render(<Tickets />)).not.toThrow()
  })
})
