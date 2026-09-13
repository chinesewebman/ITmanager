// AppBreadcrumb 测试 (M47 G-UI-Breadcrumb: 加资产名/工单标题)
// 老测试全保留; 新加 fetch 命中 + fallback 两个场景.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppBreadcrumb } from './AppBreadcrumb'
import { assetApi, ticketApi } from '../services/api'

const testQueryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: 0, gcTime: 0 },
  },
})

function renderAt(path: string) {
  return render(
    <QueryClientProvider client={testQueryClient}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/assets/:id/*" element={<AppBreadcrumb />} />
          <Route path="/tickets/:id/*" element={<AppBreadcrumb />} />
          <Route path="/:top/:id/*" element={<AppBreadcrumb />} />
          <Route path="/*" element={<AppBreadcrumb />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AppBreadcrumb', () => {
  beforeEach(() => {
    testQueryClient.clear()
    vi.restoreAllMocks()
  })

  it('首页不显示面包屑', () => {
    renderAt('/')
    expect(screen.queryByText('首页')).toBeNull()
  })

  it('二级页面显示 首页 / 资产管理', () => {
    renderAt('/assets')
    expect(screen.getByText('首页')).toBeInTheDocument()
    expect(screen.getByText('资产管理')).toBeInTheDocument()
  })

  it('三级页面显示 首页 / 资产管理 / ID: xxx (fallback, fetch 失败)', async () => {
    // M47: fetch 失败时保持老行为 — ID 截 8 位 (api mock 抛错)
    vi.spyOn(assetApi, 'get').mockRejectedValueOnce(new Error('boom'))
    renderAt('/assets/abc123def456')
    // fetch 失败 → fallback ID 立即出现 (不需要 await)
    expect(screen.getByText('首页')).toBeInTheDocument()
    expect(screen.getByText('资产管理')).toBeInTheDocument()
    expect(screen.getByText(/ID: abc123de/)).toBeInTheDocument()
    // 让 waitFor 等错误处理完
    await waitFor(() => {
      expect(screen.getByText(/ID: abc123de/)).toBeInTheDocument()
    })
  })

  it('告警中心页面', () => {
    renderAt('/alerts')
    expect(screen.getByText('告警中心')).toBeInTheDocument()
  })

  it('未匹配路径不显示面包屑 (由 404 页承担)', () => {
    renderAt('/some-unknown-path')
    // 兜底不渲染
    expect(screen.queryByText('资产管理')).toBeNull()
  })
})

describe('AppBreadcrumb M47 detail-name', () => {
  beforeEach(() => {
    testQueryClient.clear()
    vi.restoreAllMocks()
  })

  it('资产详情页: fetch 命中时显示资产名, 不是 ID', async () => {
    vi.spyOn(assetApi, 'get').mockResolvedValueOnce({
      data: { data: { id: 'a1', name: 'switch-core-01' } },
    } as any)
    renderAt('/assets/a1b2c3d4-e5f6-7890-abcd-ef1234567890')
    // 第一帧还是 fallback (loading) — waitFor 异步等取数显示名称
    await waitFor(() => {
      expect(screen.getByText('switch-core-01')).toBeInTheDocument()
    })
    // 老 "ID: a1b2c3d4..." 已不在, 因为 detailLabel 拿到名字
    expect(screen.queryByText(/ID: a1b2c3d4/)).toBeNull()
  })

  it('工单详情页: fetch 命中时显示工单标题', async () => {
    vi.spyOn(ticketApi, 'get').mockResolvedValueOnce({
      data: { data: { id: 't1', title: '交换机端口告警' } },
    } as any)
    renderAt('/tickets/t1-id')
    await waitFor(() => {
      expect(screen.getByText('交换机端口告警')).toBeInTheDocument()
    })
    expect(screen.queryByText(/ID: t1-id/)).toBeNull()
  })

  it('工单详情页: fetch 失败保持 ID fallback (不报错上抛)', () => {
    vi.spyOn(ticketApi, 'get').mockRejectedValueOnce(new Error('timeout'))
    renderAt('/tickets/abcd1234')
    expect(screen.getByText(/ID: abcd1234/)).toBeInTheDocument()
  })

  it('非详情页 (例如 alert-suppressions) 仍走 fallback ID (范围不扩散)', async () => {
    // M47 scope 限定 assets/tickets, 其他详情不动.
    renderAt('/alert-suppressions/some-id')
    expect(screen.getByText('首页')).toBeInTheDocument()
    expect(screen.getByText('告警抑制')).toBeInTheDocument()
    expect(screen.getByText(/ID: some-id/)).toBeInTheDocument()
  })
})
