// Assets page smoke test
import '@testing-library/jest-dom'
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import Assets from './Assets'

const mockAssets = [
  { id: '1', name: 'web-server-01', asset_type: 'server', ip_address: '192.168.1.10', status: 'active', site_name: '机房A', rack_name: 'Rack-01' },
  { id: '2', name: 'db-server-01', asset_type: 'server', ip_address: '192.168.1.11', status: 'active', site_name: '机房A', rack_name: 'Rack-02' },
  { id: '3', name: 'no-ip-asset', asset_type: 'server', ip_address: '', status: 'active', site_name: '机房A', rack_name: 'Rack-03' },
]

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: () => ({ data: mockAssets, isLoading: false, refetch: vi.fn() }),
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
})
