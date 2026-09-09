// Tickets page：W1 去假数据兜底 + 统计卡改真实推导 + M10 副标题计数 + W2 时间格式化。
// 修前：列表回落 MOCK_TICKETS；统计卡无条件渲染写死的 DEFAULT_STATS（与接口无关）。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import Tickets from './Tickets'

// vi.hoisted：mock 工厂在 import 期就会被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  listOverride: {} as Record<string, unknown>,
  statsOverride: {} as Record<string, unknown>,
  listRefetch: vi.fn(),
  statsRefetch: vi.fn(),
}))

// 时间串不带时区偏移 → dayjs 按本地解析，断言与 CI 时区无关
const LIST_TICKETS = [
  { id: '1', title: '服务器磁盘空间不足', priority: 'high', status: 'open', requester: '张三', assignee: '李四', created_at: '2026-02-14T10:00:00', updated_at: '2026-02-14T11:00:00' },
  { id: '2', title: '网络延迟过高', priority: 'critical', status: 'in_progress', requester: '王五', assignee: '李四', created_at: '2026-02-13T15:00:00', updated_at: '2026-02-14T09:00:00' },
]

// 统计卡数据源：未筛选全量列表 → 派生计数 7/4/2/9/3
function mk(status: string, i: number) {
  return { id: `s${i}`, title: `t${i}`, priority: 'normal', status, requester: 'x', assignee: '', created_at: '2026-02-01T00:00:00', updated_at: '2026-02-01T00:00:00' }
}
const STATS_TICKETS = [
  ...Array.from({ length: 7 }, (_, i) => mk('open', i)),
  ...Array.from({ length: 4 }, (_, i) => mk('in_progress', i + 10)),
  ...Array.from({ length: 2 }, (_, i) => mk('pending', i + 20)),
  ...Array.from({ length: 9 }, (_, i) => mk('resolved', i + 30)),
  ...Array.from({ length: 3 }, (_, i) => mk('closed', i + 40)),
]

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: (key: unknown) => {
    const k = Array.isArray(key) ? key : []
    if (k[1] === 'stats') {
      return {
        data: STATS_TICKETS,
        isLoading: false,
        isError: false,
        error: undefined,
        refetch: h.statsRefetch,
        ...h.statsOverride,
      }
    }
    return {
      data: LIST_TICKETS,
      isLoading: false,
      isError: false,
      error: undefined,
      refetch: h.listRefetch,
      ...h.listOverride,
    }
  },
  useApiMutation: () => ({ mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false }),
  queryKeys: {
    tickets: {
      list: (f?: unknown) => ['tickets', 'list', f ?? {}],
      stats: () => ['tickets', 'stats'],
    },
  },
}))

beforeEach(() => {
  h.listOverride = {}
  h.statsOverride = {}
  h.listRefetch.mockClear()
  h.statsRefetch.mockClear()
})

/** 统计卡值：取标签的前一个兄弟节点（数值 div）。 */
function statValue(label: string): string | undefined {
  return screen.getByText(label).previousElementSibling?.textContent ?? undefined
}

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

  it('W1：列表失败时显示错误态 + 重试，不回落 MOCK_TICKETS', () => {
    h.listOverride = { data: undefined, isError: true, error: { response: { status: 500 } } }
    render(<Tickets />)
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    // 关键回归断言：MOCK_TICKETS 里的标题必须消失
    expect(screen.queryByText('服务器磁盘空间不足')).toBeNull()
    expect(screen.queryByText('防火墙规则变更')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.listRefetch).toHaveBeenCalled()
  })

  it('W1：统计卡按未筛选列表真实推导（5 档，不再写死 3/5/2/15）', () => {
    render(<Tickets />)
    expect(statValue('待处理')).toBe('7')
    expect(statValue('处理中')).toBe('4')
    expect(statValue('待定')).toBe('2')
    expect(statValue('已解决')).toBe('9')
    expect(statValue('已关闭')).toBe('3')
    // 旧 DEFAULT_STATS 的虚构数字必须消失
    expect(screen.queryByText('15')).toBeNull()
  })

  it('W1：统计接口失败只在统计区块内报错，列表照常渲染', () => {
    h.statsOverride = { data: undefined, isError: true, error: { response: { status: 500 } } }
    render(<Tickets />)
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    // 区块隔离：列表数据不受牵连
    expect(screen.getByText('服务器磁盘空间不足')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.statsRefetch).toHaveBeenCalled()
  })

  it('M10：副标题计数用列表行数，筛选后补「（已筛选）」', async () => {
    render(<Tickets />)
    expect(screen.getByText('共 2 个工单')).toBeInTheDocument()
    // 打开状态下拉并选「新建」
    fireEvent.mouseDown(screen.getAllByRole('combobox')[0])
    fireEvent.click(await screen.findByTitle('新建'))
    expect(screen.getByText('共 2 个工单（已筛选）')).toBeInTheDocument()
  })

  it('W2：创建时间走 utils/time 统一格式（T 分隔 → 空格）', () => {
    render(<Tickets />)
    expect(screen.getByText('2026-02-14 10:00:00')).toBeInTheDocument()
    expect(screen.getByText('2026-02-13 15:00:00')).toBeInTheDocument()
  })

  it('W2：详情弹窗的创建/更新时间同样格式化', () => {
    render(<Tickets />)
    fireEvent.click(screen.getAllByText('详情')[0])
    expect(screen.getByText('工单详情')).toBeInTheDocument()
    expect(screen.getByText('2026-02-14 11:00:00')).toBeInTheDocument()
  })
})
