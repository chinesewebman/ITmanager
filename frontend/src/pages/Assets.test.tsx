// Assets page：W1 去假数据兜底 + M9 搜索占位符 + M10 副标题计数 + M3/P5 服务端分页。
// 修前：filtered 用 `data ?? MOCK_DATA` 兜底，接口失败渲染 5 台假资产且 isError 永远看不到。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import type * as Antd from 'antd'
import { message } from 'antd'
import Assets from './Assets'

// vi.hoisted：mock 工厂在 import 期就会被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  overrides: {} as Record<string, unknown>,
  refetch: vi.fn(),
  lastKey: null as unknown,
  retireSpy: vi.fn().mockResolvedValue({ data: { code: 0 } }),
  bulkRetireSpy: vi.fn(),
  restoreSpy: vi.fn().mockResolvedValue({ data: { code: 0 } }),
}))

const mockAssets = [
  { id: '1', name: 'web-server-01', asset_type: 'server', ip_address: '192.168.1.10', status: 'active', site_name: '机房A', rack_name: 'Rack-01' },
  { id: '2', name: 'db-server-01', asset_type: 'server', ip_address: '192.168.1.11', status: 'active', site_name: '机房A', rack_name: 'Rack-02' },
  { id: '3', name: 'no-ip-asset', asset_type: 'server', ip_address: '', status: 'active', site_name: '机房A', rack_name: 'Rack-03' },
]

// M58：批量操作 handler 依赖 useApiMutation 的回调（成功/失败分流、成功后清空选择），
// 探针只暴露被测组件真正消费的那几个成员，避免为测试引入 react-query 的 UseMutationResult 全量类型。
type MutationProbe<TVars> = {
  mutate: (vars: TVars) => void
  mutateAsync: (vars: TVars) => Promise<unknown>
  isPending: boolean
  isError: boolean
  isSuccess: boolean
  data: undefined
  error: null
  reset: () => void
}

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
    options?: {
      onSuccess?: (result: TResult, vars: TVars) => void
      onError?: (error: unknown, vars: TVars) => void
    },
  ): MutationProbe<TVars> => {
    // 让 mutate 真调 mutator (串行 Promise chain), 这样测试可以验真路径
    // (Popconfirm onConfirm → mutate → assetApi.bulkRetire spy 被调)。
    // M58: 同时按 useApiMutation 的真实语义回调 onSuccess / onError —— 否则
    // 「部分成功分流到哪个后缀的消息」「成功后清空选择」这些行为在测试里不可观测。
    // 不接 QueryClient (避免测试套必须包 QueryClientProvider 的级联改动)。
    return {
      mutate: (vars: TVars) => {
        void Promise.resolve()
          .then(() => mutator(vars))
          .then(
            (r) => options?.onSuccess?.(r, vars),
            (e: unknown) => options?.onError?.(e, vars),
          )
      },
      mutateAsync: (vars: TVars) => mutator(vars),
      isPending: false,
      isError: false,
      isSuccess: false,
      data: undefined,
      error: null,
      reset: () => {},
    }
  },
  queryKeys: { assets: { list: (f?: Record<string, unknown>) => ['assets', 'list', f ?? {}] } },
}))

// Mock antd message（避免 jsdom 副作用 + 让「部分成功走哪个后缀的消息」可断言）
vi.mock('antd', async () => {
  const actual = await vi.importActual<typeof Antd>('antd')
  return {
    ...actual,
    message: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() },
  }
})

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
    // M51: 批量操作 spy；M58: 批量退役改走 bulkRetire 单端点
    retire: (...args: any[]) => h.retireSpy(...args),
    bulkRetire: (ids: string[], reason: string) => h.bulkRetireSpy(ids, reason),
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

  // M58: 批量退役改走单端点 POST /assets/bulk-retire（原 N 次串行 /:id/retire）。
  // 断言落在**调用面**上：bulkRetire 被调一次且带全部 id（N 次单条调用 = 回归）。
  it('M58：[批量退役] 二次确认后调 assetApi.bulkRetire 1 次, 带全部 id + reason="批量退役"', async () => {
    h.bulkRetireSpy.mockResolvedValue({
      data: { code: 0, data: { succeeded: ['1', '2'], failed: {} } },
    })
    const { container } = render(<Assets />)
    const checkboxes = container.querySelectorAll('tbody tr[data-row-key] .ant-checkbox-input')
    expect(checkboxes.length).toBeGreaterThanOrEqual(2)
    fireEvent.click(checkboxes[0])
    fireEvent.click(checkboxes[1])
    await waitFor(() => {
      expect(screen.getByTestId('asset-bulk-retire')).toBeInTheDocument()
    })
    // Popconfirm onConfirm 由 antd Popconfirm 接管：click trigger → click OK 按钮 → onConfirm。
    fireEvent.click(screen.getByTestId('asset-bulk-retire'))
    const okBtn = await waitFor(() => {
      const btn = document.querySelector('.ant-popconfirm .ant-btn-primary') as HTMLButtonElement | null
      if (!btn) throw new Error('Popconfirm OK button not found')
      return btn
    })
    fireEvent.click(okBtn)
    await waitFor(() => {
      expect(h.bulkRetireSpy).toHaveBeenCalledTimes(1)
    })
    expect(h.bulkRetireSpy).toHaveBeenCalledWith(['1', '2'], '批量退役')
    // 单条端点不再被批量路径调用（回归护栏：留着串行循环的旧实现会在这里红）
    expect(h.retireSpy).not.toHaveBeenCalled()
  })

  it('M58：[批量退役] 全部成功 → success 消息含项数 + 清空选择 + refetch', async () => {
    vi.mocked(message.success).mockClear()
    h.bulkRetireSpy.mockResolvedValue({
      data: { code: 0, data: { succeeded: ['1', '2'], failed: {} } },
    })
    const { container } = render(<Assets />)
    const checkboxes = container.querySelectorAll('tbody tr[data-row-key] .ant-checkbox-input')
    fireEvent.click(checkboxes[0])
    fireEvent.click(checkboxes[1])
    await waitFor(() => {
      expect(screen.getByTestId('asset-bulk-retire')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId('asset-bulk-retire'))
    const okBtn = await waitFor(() => {
      const btn = document.querySelector('.ant-popconfirm .ant-btn-primary') as HTMLButtonElement | null
      if (!btn) throw new Error('Popconfirm OK button not found')
      return btn
    })
    fireEvent.click(okBtn)
    await waitFor(() => {
      expect(vi.mocked(message.success)).toHaveBeenCalledWith('批量退役成功 2 项')
    })
    // 成功即清空选择 → 批量条消失（不必手动点「清空选择」）
    await waitFor(() => {
      expect(screen.queryByTestId('asset-bulk-bar')).toBeNull()
    })
    expect(h.refetch).toHaveBeenCalled()
  })

  it('M58：[批量退役] 部分失败 → warning 报成功/失败两侧计数, 不谎报全失败', async () => {
    vi.mocked(message.warning).mockClear()
    vi.mocked(message.error).mockClear()
    // failed 是 map：一个 id 不存在（后端返 200 + failed）
    h.bulkRetireSpy.mockResolvedValue({
      data: { code: 0, data: { succeeded: ['1'], failed: { 2: '资产不存在' } } },
    })
    const { container } = render(<Assets />)
    const checkboxes = container.querySelectorAll('tbody tr[data-row-key] .ant-checkbox-input')
    fireEvent.click(checkboxes[0])
    fireEvent.click(checkboxes[1])
    await waitFor(() => {
      expect(screen.getByTestId('asset-bulk-retire')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId('asset-bulk-retire'))
    const okBtn = await waitFor(() => {
      const btn = document.querySelector('.ant-popconfirm .ant-btn-primary') as HTMLButtonElement | null
      if (!btn) throw new Error('Popconfirm OK button not found')
      return btn
    })
    fireEvent.click(okBtn)
    await waitFor(() => {
      expect(vi.mocked(message.warning)).toHaveBeenCalledWith('批量退役成功 1 项，失败 1 项')
    })
    // 部分成功不是失败：不得走 error 后缀（旧实现用 throw 表达部分失败，会落在这里）
    expect(vi.mocked(message.error)).not.toHaveBeenCalled()
    await waitFor(() => {
      expect(screen.queryByTestId('asset-bulk-bar')).toBeNull()
    })
  })

  it('M58：[批量退役] 全部失败 → error 报失败项数（不能报「成功 0 项」）', async () => {
    vi.mocked(message.error).mockClear()
    vi.mocked(message.success).mockClear()
    h.bulkRetireSpy.mockResolvedValue({
      data: { code: 0, data: { succeeded: [], failed: { 1: '无法退役（资产已退役或参数无效）', 2: '资产不存在' } } },
    })
    const { container } = render(<Assets />)
    const checkboxes = container.querySelectorAll('tbody tr[data-row-key] .ant-checkbox-input')
    fireEvent.click(checkboxes[0])
    fireEvent.click(checkboxes[1])
    await waitFor(() => {
      expect(screen.getByTestId('asset-bulk-retire')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId('asset-bulk-retire'))
    const okBtn = await waitFor(() => {
      const btn = document.querySelector('.ant-popconfirm .ant-btn-primary') as HTMLButtonElement | null
      if (!btn) throw new Error('Popconfirm OK button not found')
      return btn
    })
    fireEvent.click(okBtn)
    await waitFor(() => {
      expect(vi.mocked(message.error)).toHaveBeenCalledWith('批量退役失败 2 项')
    })
    expect(vi.mocked(message.success)).not.toHaveBeenCalled()
  })

  it('M58：[批量退役] 请求整体失败（网络/4xx）→ onError 报错，不退化成成功文案', async () => {
    vi.mocked(message.error).mockClear()
    h.bulkRetireSpy.mockRejectedValue(new Error('Request failed with status code 400'))
    const { container } = render(<Assets />)
    const checkboxes = container.querySelectorAll('tbody tr[data-row-key] .ant-checkbox-input')
    fireEvent.click(checkboxes[0])
    await waitFor(() => {
      expect(screen.getByTestId('asset-bulk-retire')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId('asset-bulk-retire'))
    const okBtn = await waitFor(() => {
      const btn = document.querySelector('.ant-popconfirm .ant-btn-primary') as HTMLButtonElement | null
      if (!btn) throw new Error('Popconfirm OK button not found')
      return btn
    })
    fireEvent.click(okBtn)
    await waitFor(() => {
      expect(vi.mocked(message.error)).toHaveBeenCalledWith(
        '批量退役失败：Request failed with status code 400',
      )
    })
  })

  it('M58-MUT：[批量退役] 不点二次确认 → bulkRetire 不被调（mutation inversion 对照）', async () => {
    h.bulkRetireSpy.mockClear()
    const { container } = render(<Assets />)
    const checkboxes = container.querySelectorAll('tbody tr[data-row-key] .ant-checkbox-input')
    fireEvent.click(checkboxes[0])
    await waitFor(() => {
      expect(screen.getByTestId('asset-bulk-retire')).toBeInTheDocument()
    })
    // 只点开 Popconfirm, 不点 OK → mutator 不得执行。
    fireEvent.click(screen.getByTestId('asset-bulk-retire'))
    expect(h.bulkRetireSpy).not.toHaveBeenCalled()
  })

  // M52: AssetFilterBar status 下拉 (G-UI-AssetFilter)
  // 注: antd 5 Select 不把 placeholder 暴露为 input 的 placeholder 属性,
  // 而是渲染为 .ant-select-selection-placeholder span, 所以用 querySelector 限定 scope 而非 getByText.
  // 同时 AssetTable 也含"状态"列标题, 不能用 getByText (会冲突).
  it('M52：AssetFilterBar 渲染含"状态" placeholder (status Select 出现)', () => {
    render(<Assets />)
    const placeholderSpans = document.querySelectorAll('.ant-select-selection-placeholder')
    const placeholders = Array.from(placeholderSpans).map((s) => (s.textContent ?? '').trim())
    expect(placeholders).toContain('状态')
  })

  it('M52：选 status=active → queryKey 含 status (assetApi.list 收到 status 参数)', async () => {
    h.lastKey = null
    render(<Assets />)
    // 找 status Select (其 placeholder 文本是 "状态")
    const placeholderSpans = document.querySelectorAll('.ant-select-selection-placeholder')
    const statusPlaceholder = Array.from(placeholderSpans).find(
      (s) => (s.textContent ?? '').trim() === '状态',
    )
    expect(statusPlaceholder).toBeTruthy()
    // antd Select 由 .ant-select-selector 接管 click, 直接点 placeholder 父级 .ant-select 也行
    // 但更稳是点 .ant-select-selector (Select 弹层入口).
    const statusTrigger = statusPlaceholder!.closest('.ant-select-selector') as HTMLElement
    expect(statusTrigger).toBeTruthy()
    fireEvent.mouseDown(statusTrigger)
    await waitFor(() => {
      const opts = document.querySelectorAll('.ant-select-item-option')
      expect(opts.length).toBeGreaterThan(0)
    })
    // 找"在线" option (active)
    const activeOpt = Array.from(document.querySelectorAll('.ant-select-item-option')).find(
      (el) => (el.textContent ?? '').includes('在线'),
    )
    expect(activeOpt).toBeDefined()
    fireEvent.click(activeOpt!)
    // 验证 queryKey 更新 (useApiQuery mock 记录 lastKey)
    await waitFor(() => {
      const k = h.lastKey as any
      const filters = k?.[2] ?? {}
      expect(filters.status).toBe('active')
      // status 变 → 翻页重置回 1
      expect(filters.page).toBe(1)
    })
  })

  it('M52：选 status=retired → queryKey 含 retired, 副标题 "已筛选" 出现 (hasFilter 判定含 status)', async () => {
    h.lastKey = null
    render(<Assets />)
    const placeholderSpans = document.querySelectorAll('.ant-select-selection-placeholder')
    const statusPlaceholder = Array.from(placeholderSpans).find(
      (s) => (s.textContent ?? '').trim() === '状态',
    )
    const statusTrigger = statusPlaceholder!.closest('.ant-select-selector') as HTMLElement
    fireEvent.mouseDown(statusTrigger)
    await waitFor(() => {
      expect(document.querySelectorAll('.ant-select-item-option').length).toBeGreaterThan(0)
    })
    const retiredOpt = Array.from(document.querySelectorAll('.ant-select-item-option')).find(
      (el) => (el.textContent ?? '').includes('已退役'),
    )
    expect(retiredOpt).toBeDefined()
    fireEvent.click(retiredOpt!)
    await waitFor(() => {
      const k = h.lastKey as any
      expect(k?.[2]?.status).toBe('retired')
      expect(k?.[2]?.page).toBe(1)
    })
    // 副标题 "（已筛选）" 出现 (hasFilter 判定含 status 字段)
    expect(screen.getByText(/已筛选/)).toBeInTheDocument()
  })
})
