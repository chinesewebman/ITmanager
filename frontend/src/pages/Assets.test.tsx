// Assets page：W1 去假数据兜底 + M9 搜索占位符 + M10 副标题计数。
// 修前：filtered 用 `data ?? MOCK_DATA` 兜底，接口失败渲染 5 台假资产且 isError 永远看不到。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import Assets from './Assets'

// vi.hoisted：mock 工厂在 import 期就会被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  overrides: {} as Record<string, unknown>,
  refetch: vi.fn(),
}))

const mockAssets = [
  { id: '1', name: 'web-server-01', asset_type: 'server', ip_address: '192.168.1.10', status: 'active', site_name: '机房A', rack_name: 'Rack-01' },
  { id: '2', name: 'db-server-01', asset_type: 'server', ip_address: '192.168.1.11', status: 'active', site_name: '机房A', rack_name: 'Rack-02' },
  { id: '3', name: 'no-ip-asset', asset_type: 'server', ip_address: '', status: 'active', site_name: '机房A', rack_name: 'Rack-03' },
]

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: () => ({
    data: mockAssets,
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: h.refetch,
    ...h.overrides,
  }),
  useApiMutation: () => ({ mutate: vi.fn(), mutateAsync: vi.fn() }),
  queryKeys: { assets: { list: () => ['assets', 'list'] } },
}))

// mock diagnosticApi（ping/traceroute）
const mockPing = vi.fn().mockResolvedValue({
  data: {
    code: 0,
    data: {
      host: '192.168.1.10',
      count: 4,
      transmitted: 4,
      received: 4,
      loss_percent: 0,
      min_ms: 0.1,
      avg_ms: 0.2,
      max_ms: 0.3,
      stddev_ms: 0.05,
      duration_ms: 2100,
    },
  },
})
const mockTrace = vi.fn().mockResolvedValue({
  data: {
    code: 0,
    data: {
      host: '192.168.1.10',
      max_hops: 20,
      reached: true,
      duration_ms: 3000,
      hops: [
        { hop: 1, host: 'gateway', ip: '192.168.1.1', rtts: ['1ms', '2ms', '1ms'], lossed: false },
      ],
    },
  },
})

// A-2: mock postmortemApi
const mockPostmortem = vi.fn().mockResolvedValue(new Blob(['%PDF-1.4 mock'], { type: 'application/pdf' }))

vi.mock('../services/api', () => ({
  assetApi: {
    list: () => Promise.resolve({ data: { data: { items: [] } } }),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
  },
  diagnosticApi: {
    ping: (...args: any[]) => mockPing(...args),
    traceroute: (...args: any[]) => mockTrace(...args),
  },
  postmortemApi: {
    downloadReport: (...args: any[]) => mockPostmortem(...args),
  },
}))

beforeEach(() => {
  h.overrides = {}
  h.refetch.mockClear()
})

describe('Assets page', () => {
  it('渲染资产表格 + 关键列（mock 数据）', () => {
    render(<Assets />)
    expect(screen.getByText('资产管理')).toBeInTheDocument()
    // AssetTable 渲染 mock 资产名
    expect(screen.getByText('web-server-01')).toBeInTheDocument()
    expect(screen.getByText('db-server-01')).toBeInTheDocument()
    // IP 列
    expect(screen.getByText('192.168.1.10')).toBeInTheDocument()
  })

  it('不 crash 渲染', () => {
    expect(() => render(<Assets />)).not.toThrow()
  })

  it('每行显示 Ping/Trace 按钮', () => {
    render(<Assets />)
    const pings = screen.getAllByText('Ping')
    const traces = screen.getAllByText('Trace')
    // 2 个有 IP 的资产都应该有按钮
    expect(pings.length).toBeGreaterThanOrEqual(2)
    expect(traces.length).toBeGreaterThanOrEqual(2)
  })

  it('点击 Ping 按钮触发诊断 modal 并调用 API', async () => {
    mockPing.mockClear()
    render(<Assets />)
    const pings = screen.getAllByText('Ping')
    fireEvent.click(pings[0])

    await waitFor(() => {
      expect(mockPing).toHaveBeenCalledWith('192.168.1.10', 4)
    })
    // modal 标题
    expect(screen.getByText(/Ping 探活/)).toBeTruthy()
    // 结果展示
    await waitFor(() => {
      expect(screen.getByText(/min 0.1 ms/)).toBeTruthy()
    })
  })

  // H10：诊断失败原 catch 空实现 → 弹窗全白。加 diagError + Alert + 重试。
  it('H10：Ping 失败时弹窗显示错误 Alert + 重试（而非全白）', async () => {
    mockPing.mockClear()
    mockPing.mockRejectedValue(new Error('网络错误'))
    render(<Assets />)
    fireEvent.click(screen.getAllByText('Ping')[0])

    // 失败后弹窗内渲染错误 Alert，不再全白
    await waitFor(() => {
      expect(screen.getByText('诊断失败')).toBeInTheDocument()
    })
    expect(screen.getByText('网络错误')).toBeInTheDocument()

    // 重试：mockPing 改成功，点重试后再次调用
    mockPing.mockResolvedValue({
      data: { code: 0, data: { host: '192.168.1.10', count: 4, transmitted: 4, received: 4, loss_percent: 0, min_ms: 0.1, avg_ms: 0.2, max_ms: 0.3, duration_ms: 2100 } },
    })
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    await waitFor(() => {
      expect(mockPing).toHaveBeenCalledTimes(2)
    })
  })

  it('每行显示复盘按钮', () => {
    render(<Assets />)
    // 3 个资产 → 3 个复盘按钮
    expect(screen.getAllByText('复盘').length).toBeGreaterThanOrEqual(3)
  })

  it('点击复盘按钮调用下载 API', async () => {
    mockPostmortem.mockClear()
    render(<Assets />)
    const btns = screen.getAllByText('复盘')
    fireEvent.click(btns[0])

    await waitFor(() => {
      expect(mockPostmortem).toHaveBeenCalledWith(expect.any(String), 30)
    })
  })

  it('W1：接口失败时显示错误态 + 重试，不回落假资产', () => {
    h.overrides = { data: undefined, isError: true, error: { response: { status: 500 } } }
    render(<Assets />)
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    // 关键回归断言：假兜底资产（MOCK_DATA 里的 web-server-01）必须消失
    expect(screen.queryByText('web-server-01')).toBeNull()
    expect(screen.queryByPlaceholderText('搜索名称 / IP')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch).toHaveBeenCalled()
  })

  it('W1：200 + 空 items 不白屏（走空态，不是假资产）', () => {
    h.overrides = { data: [] }
    render(<Assets />)
    expect(screen.getByText('暂无资产')).toBeInTheDocument()
    expect(screen.getByText('共 0 台资产')).toBeInTheDocument()
  })

  it('M10：副标题计数跟随筛选结果，筛选后带「（已筛选）」', () => {
    render(<Assets />)
    expect(screen.getByText('共 3 台资产')).toBeInTheDocument()
    fireEvent.change(screen.getByPlaceholderText('搜索名称 / IP'), { target: { value: 'web' } })
    expect(screen.getByText('共 1 台资产（已筛选）')).toBeInTheDocument()
    expect(screen.getByText('web-server-01')).toBeInTheDocument()
    expect(screen.queryByText('db-server-01')).toBeNull()
  })

  // M2：表格排序——此前全站零 sorter，用户无法点击表头排序。
  // AssetTable 给名称/类型/IP/机房/机柜/状态加前端本地排序；这里验证 IP 列走八位组
  // 数值序（192.168.1.2 应排在 192.168.1.10 前，字典序会错序）。
  it('M2：IP 地址列按八位组数值排序（192.168.1.2 排在 192.168.1.10 前）', async () => {
    h.overrides = {
      data: [
        { id: '1', name: 'web-server-01', asset_type: 'server', ip_address: '192.168.1.10', status: 'active', site_name: '机房A', rack_name: 'Rack-01' },
        { id: '2', name: 'db-server-01', asset_type: 'server', ip_address: '192.168.1.2', status: 'active', site_name: '机房A', rack_name: 'Rack-02' },
      ],
    }
    const { container } = render(<Assets />)
    // 整行 textContent（rowSelection 会加 checkbox 首列，不能取 td[0]）
    const rowTexts = () =>
      Array.from(container.querySelectorAll('tbody tr[data-row-key]')).map(
        (r) => r.textContent ?? '',
      )

    // 初始顺序 = dataSource 顺序：web-server-01（192.168.1.10）在前
    expect(rowTexts()[0]).toContain('web-server-01')

    // 点「IP 地址」表头升序 → 数值序 192.168.1.2 < 192.168.1.10，db-server-01 排前
    // （scroll+fixed 列导致 header 渲染两份 title span，取第一个）
    fireEvent.click(screen.getAllByText('IP 地址')[0])
    await waitFor(() => {
      expect(rowTexts()[0]).toContain('db-server-01')
    })
  })

  it('M9：搜索占位符只承诺「名称 / IP」（不再承诺资产标签/SN）', () => {
    render(<Assets />)
    expect(screen.getByPlaceholderText('搜索名称 / IP')).toBeInTheDocument()
  })

  it('W1：ip_address 为 undefined 时筛选不抛异常', () => {
    h.overrides = {
      data: [{ id: '9', name: 'ghost', asset_type: 'server', ip_address: undefined, status: 'active' }],
    }
    render(<Assets />)
    expect(() => {
      fireEvent.change(screen.getByPlaceholderText('搜索名称 / IP'), { target: { value: '192' } })
    }).not.toThrow()
    expect(screen.getByText('共 0 台资产（已筛选）')).toBeInTheDocument()
  })
})
