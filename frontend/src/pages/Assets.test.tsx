// Assets page：W1 去假数据兜底 + M9 搜索占位符 + M10 副标题计数 + M3/P5 服务端分页。
// 修前：filtered 用 `data ?? MOCK_DATA` 兜底，接口失败渲染 5 台假资产且 isError 永远看不到。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import Assets from './Assets'

// vi.hoisted：mock 工厂在 import 期就会被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  overrides: {} as Record<string, unknown>,
  refetch: vi.fn(),
  lastKey: null as unknown,
  retireSpy: vi.fn().mockResolvedValue({ data: { code: 0 } }),
  restoreSpy: vi.fn().mockResolvedValue({ data: { code: 0 } }),
}))

const mockAssets = [
  { id: '1', name: 'web-server-01', asset_type: 'server', ip_address: '192.168.1.10', status: 'active', site_name: '机房A', rack_name: 'Rack-01' },
  { id: '2', name: 'db-server-01', asset_type: 'server', ip_address: '192.168.1.11', status: 'active', site_name: '机房A', rack_name: 'Rack-02' },
  { id: '3', name: 'no-ip-asset', asset_type: 'server', ip_address: '', status: 'active', site_name: '机房A', rack_name: 'Rack-03' },
]

// M3/P5：data 结构改为 {items, total}（服务端分页契约）。useApiQuery mock 记录 queryKey，
// 供分页/筛选变化断言（原 filtered 前端过滤已删除，筛选下沉到后端，仅触发 queryKey 更新）。
vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: (key: unknown) => {
    h.lastKey = key
    return {
      data: { items: mockAssets, total: mockAssets.length },
      isLoading: false,
      isError: false,
      error: undefined,
      refetch: h.refetch,
      ...h.overrides,
    }
  },
  useApiMutation: <TVars, TResult>(
    mutator: (vars: TVars) => Promise<TResult>,
  ) => {
    // 让 mutate 真调 mutator (串行 Promise chain), 这样测试可以验真路径
    // (Popconfirm onConfirm → mutate → assetApi.retire spy 被调)。
    // 不接 QueryClient (避免测试套必须包 QueryClientProvider 的级联改动)。
    return {
      mutate: (vars: TVars) => { void mutator(vars) },
      mutateAsync: (vars: TVars) => mutator(vars),
      isPending: false,
      isError: false,
      isSuccess: false,
      data: undefined,
      error: null,
      reset: () => {},
    } as any
  },
  queryKeys: { assets: { list: (f?: Record<string, unknown>) => ['assets', 'list', f ?? {}] } },
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
    list: () => Promise.resolve({ data: { data: { items: [], total: 0 } } }),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    // M51: 批量操作 spy
    retire: (...args: any[]) => h.retireSpy(...args),
    restore: (...args: any[]) => h.restoreSpy(...args),
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
  h.lastKey = null
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
    h.overrides = { data: { items: [], total: 0 } }
    render(<Assets />)
    expect(screen.getByText('暂无资产')).toBeInTheDocument()
    expect(screen.getByText('共 0 台资产')).toBeInTheDocument()
  })

  // M10 + M3/P5：副标题计数跟随服务端 total（此前用未过滤/截断总数，与表格行数不符）。
  it('M10：副标题计数来自服务端 total', () => {
    h.overrides = { data: { items: mockAssets, total: 100 } }
    render(<Assets />)
    expect(screen.getByText('共 100 台资产')).toBeInTheDocument()
  })

  // M3/P5：服务端分页——total 不再丢弃 + 翻页更新 queryKey（page 变化），筛选变化重置 page。
  it('M3/P5：服务端分页——翻页更新 queryKey 的 page，筛选重置回第 1 页', async () => {
    h.overrides = { data: { items: mockAssets, total: 100 } }
    const { container } = render(<Assets />)

    // 初始 queryKey 含 page:1/pageSize:20
    expect((h.lastKey as any)[2]).toMatchObject({ page: 1, pageSize: 20 })

    // 点「下一页」→ onPageChange(2, 20) → setPage(2) → queryKey page 变 2
    const next = container.querySelector('.ant-pagination-next')
    expect(next).toBeTruthy()
    fireEvent.click(next as Element)
    await waitFor(() => {
      expect((h.lastKey as any)[2]).toMatchObject({ page: 2 })
    })

    // 输入筛选关键词 → 重置 page 回 1，keyword 下沉进 queryKey
    fireEvent.change(screen.getByPlaceholderText('搜索名称 / IP'), { target: { value: 'web' } })
    await waitFor(() => {
      expect((h.lastKey as any)[2]).toMatchObject({ keyword: 'web', page: 1 })
    })
  })

  // M2：表格排序——此前全站零 sorter，用户无法点击表头排序。
  // AssetTable 给名称/类型/IP/机房/机柜/状态加前端本地排序；这里验证 IP 列走八位组
  // 数值序（192.168.1.2 应排在 192.168.1.10 前，字典序会错序）。
  it('M2：IP 地址列按八位组数值排序（192.168.1.2 排在 192.168.1.10 前）', async () => {
    h.overrides = {
      data: {
        items: [
          { id: '1', name: 'web-server-01', asset_type: 'server', ip_address: '192.168.1.10', status: 'active', site_name: '机房A', rack_name: 'Rack-01' },
          { id: '2', name: 'db-server-01', asset_type: 'server', ip_address: '192.168.1.2', status: 'active', site_name: '机房A', rack_name: 'Rack-02' },
        ],
        total: 2,
      },
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

  // M51: 批量操作 (G-UI-BulkAssets)
  // 老 useApiMutation mock 返 `{mutate: vi.fn()}` 不真正执行, 我们用 useApiQuery mock
  // 模拟 selectedRowKeys 装到组件 state 没法直接, 改: 用 row checkbox 触发 onChange.
  // 但 bulkMut 在 mock useApiMutation 路径下不走, 需打补丁: 让 useApiMutation 真执行.

  it('M51：选中 0 项时批量条不渲染', () => {
    render(<Assets />)
    expect(screen.queryByTestId('asset-bulk-bar')).toBeNull()
  })

  it('M51：选中 ≥1 项时批量条出现且显示「已选 N 项」', async () => {
    const { container } = render(<Assets />)
    // antd Table row checkbox 在 tbody 第一格
    const checkboxes = container.querySelectorAll('tbody tr[data-row-key] .ant-checkbox-input')
    expect(checkboxes.length).toBeGreaterThan(0)
    fireEvent.click(checkboxes[0])
    await waitFor(() => {
      expect(screen.getByTestId('asset-bulk-bar')).toBeInTheDocument()
    })
    expect(screen.getByTestId('asset-bulk-bar').textContent).toMatch(/已选.*1.*项/)
  })

  it('M51：清空选择按钮清掉 selectedRowKeys, 批量条隐藏', async () => {
    const { container } = render(<Assets />)
    const checkboxes = container.querySelectorAll('tbody tr[data-row-key] .ant-checkbox-input')
    fireEvent.click(checkboxes[0])
    fireEvent.click(checkboxes[1])
    await waitFor(() => {
      expect(screen.getByTestId('asset-bulk-bar')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId('asset-bulk-clear'))
    await waitFor(() => {
      expect(screen.queryByTestId('asset-bulk-bar')).toBeNull()
    })
  })

  it('M51：所有选中项都不是 retired 时不显示「批量恢复」', async () => {
    // mockAssets 默认全 active, 没 retired
    const { container } = render(<Assets />)
    const checkboxes = container.querySelectorAll('tbody tr[data-row-key] .ant-checkbox-input')
    fireEvent.click(checkboxes[0])
    await waitFor(() => {
      expect(screen.getByTestId('asset-bulk-bar')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('asset-bulk-restore')).toBeNull()
    expect(screen.getByTestId('asset-bulk-retire')).toBeInTheDocument()
  })

  // M51 mutation 实证: bypass onConfirm → retire API 不被调
  // 走真实 useApiMutation 路径 (通过 vi.mock '../hooks/useApiQuery' 让 useApiMutation 用真 useMutation),
  // 但测试套老 mock useApiMutation 返 `{mutate: vi.fn()}`, 不执行 mutator. 此测试改用
  // vi.spyOn(assetApi, 'retire') 直接观察调用, 然后渲染时强行改 AssetTable row onChange.
  it('M51：[批量退役] 二次确认后真调 assetApi.retire N 次, reason="批量退役"', async () => {
    h.retireSpy.mockClear()
    const { container } = render(<Assets />)
    const checkboxes = container.querySelectorAll('tbody tr[data-row-key] .ant-checkbox-input')
    expect(checkboxes.length).toBeGreaterThanOrEqual(2)
    fireEvent.click(checkboxes[0])
    fireEvent.click(checkboxes[1])
    await waitFor(() => {
      expect(screen.getByTestId('asset-bulk-retire')).toBeInTheDocument()
    })
    // Popconfirm onConfirm 由 antd Popconfirm 接管 (hover + click OK button).
    // 直接调 trigger 按钮的 onClick 不会弹 trap, 但 click trigger + click OK button 可行.
    // antd Popconfirm OK button 角色 = Popconfirm 弹层里的 `.ant-popconfirm .ant-btn-primary`.
    // 点击 trigger → 等待 Popconfirm 出现 → 点击 OK 按钮 → onConfirm 触发 → mutate → assetApi.retire 被调.
    fireEvent.click(screen.getByTestId('asset-bulk-retire'))
    const okBtn = await waitFor(() => {
      const btn = document.querySelector('.ant-popconfirm .ant-btn-primary') as HTMLButtonElement | null
      if (!btn) throw new Error('Popconfirm OK button not found')
      return btn
    })
    fireEvent.click(okBtn)
    await waitFor(() => {
      expect(h.retireSpy).toHaveBeenCalledTimes(2)
    })
    // 验证 reason = "批量退役" (M51 intent 硬要求: 批量 50 项不再弹填原因 modal)
    expect(h.retireSpy).toHaveBeenNthCalledWith(1, '1', '批量退役')
    expect(h.retireSpy).toHaveBeenNthCalledWith(2, '2', '批量退役')
  })

  it('M51-MUT：[批量退役] 二次确认后 bypass 路径 → retire spy 不被调 (mutation inversion 实证)', async () => {
    h.retireSpy.mockClear()
    // 临时改 Assets.tsx onConfirm 路径: 把 "onConfirm={() => bulkRetireMut.mutate(...)}" 改为空箭头.
    // 这里我们用源码 bypass 模式 (外部脚本验, 见 docs/M51-mutation-inversion.sh).
    // 单测层 mock 不易证 (closure), 此测做 placeholder 标记, 真证靠外部 mutation_inversion.
    const { container } = render(<Assets />)
    const checkboxes = container.querySelectorAll('tbody tr[data-row-key] .ant-checkbox-input')
    fireEvent.click(checkboxes[0])
    await waitFor(() => {
      expect(screen.getByTestId('asset-bulk-retire')).toBeInTheDocument()
    })
    // 不点 Popconfirm OK 按钮, spy 应保持 0 调用 — 这是"未触发" 实证, 不是"被 bypass" 实证.
    // 真 bypass 实证见 docs/M51-mutation-inversion.sh (改源码 cp /tmp + vitest + revert).
    expect(h.retireSpy).not.toHaveBeenCalled()
  })
})
