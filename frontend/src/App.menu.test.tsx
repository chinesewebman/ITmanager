// M61 — 侧边栏「用户管理」入口的能力门禁。
//
// 为什么单独一层测试：入口条件渲染写错有两种表现，人工都不容易发现 ——
//   ① 该有却没有：admin 登进来找不到用户管理（功能等于没交付）；
//   ② 不该有却出现：ops_admin / readonly 看到一个点进去必然 403 的链接。
// 前者靠「渲染后能看到入口」抓，后者靠「没有 identity 能力时整条不出现」抓。
//
// 判据是 /auth/me 下发的 `capabilities`（**不是** role 字面量）：roles.go 明确警告过
// 复制角色→能力矩阵会漂移，M49 的审计入口也是这么做的。故这里打桩的是 /auth/me 的响应，
// 而不是 localStorage 里的 role —— 后者会让用例绿着而实际不生效。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { MenuProps } from 'antd'
import { AppLayout, buildMenuItems } from './App'
import api from './services/api'

/** 菜单项的 key 序列（antd 的 items 元素带 type/key 联合，这里只看 key）。 */
function keysOf(items: MenuProps['items']): string[] {
  return (items ?? []).map((it) => String((it as { key?: unknown }).key))
}

function renderAppLayout(capabilities: unknown) {
  vi.spyOn(api, 'get').mockImplementation(((url: string) => {
    if (url === '/auth/me') {
      return Promise.resolve({ data: { code: 0, data: { role: 'x', capabilities } } })
    }
    // 其余请求一律拒绝：本用例只关心菜单，任何别的请求都说明组件依赖被意外引入
    return Promise.reject(new Error('unexpected request: ' + url))
  }) as never)

  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 0 } } })
  return render(
    <QueryClientProvider client={qc}>
      {/* /404 命中非懒加载的 NotFoundPage：本用例不碰任何页面级 fetch */}
      <MemoryRouter initialEntries={['/404']}>
        <AppLayout />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.setItem('user', JSON.stringify({ id: 'u1', username: 'admin', nickname: '管理员', role: 'admin' }))
})

describe('M61 侧边栏用户管理入口（能力门禁）', () => {
  it('buildMenuItems：无 identity 能力 → 没有 /users；有 → 有，且其余菜单项一个不少', () => {
    const without = keysOf(buildMenuItems(false, false))
    const withIdentity = keysOf(buildMenuItems(true, false))

    expect(without).not.toContain('/users')
    expect(withIdentity).toContain('/users')
    // 只多这一项（防「条件渲染」顺手把别的菜单项吞掉）
    expect(withIdentity.filter((k) => !without.includes(k))).toEqual(['/users'])
    // 顺序：用户管理在系统设置之前（与页面里 buildMenuItems 的字面顺序一致）
    expect(withIdentity.indexOf('/users')).toBeLessThan(withIdentity.indexOf('/settings'))
  })

  it('admin（capabilities 含 identity）→ 侧边栏出现「用户管理」', async () => {
    renderAppLayout(['read', 'write', 'manage', 'audit', 'identity'])
    expect(await screen.findByText('用户管理')).toBeInTheDocument()
  })

  it('ops_admin（无 identity 能力）→ 侧边栏不出现「用户管理」（点进去只会 403）', async () => {
    renderAppLayout(['read', 'write', 'manage'])
    // 先等菜单渲染出来（否则「找不到用户管理」可能只是因为整棵树还没渲染）
    expect(await screen.findByText('系统设置')).toBeInTheDocument()
    expect(screen.queryByText('用户管理')).toBeNull()
  })

  it('/auth/me 失败（网络错/未登录）→ fail-closed，入口不显示', async () => {
    vi.spyOn(api, 'get').mockRejectedValue(new Error('network down') as never)
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={['/404']}>
          <AppLayout />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('系统设置')).toBeInTheDocument()
    expect(screen.queryByText('用户管理')).toBeNull()
  })

  it('capabilities 形状异常（不是数组 / 缺字段）→ 入口不显示', async () => {
    renderAppLayout('read,identity')
    expect(await screen.findByText('系统设置')).toBeInTheDocument()
    expect(screen.queryByText('用户管理')).toBeNull()
  })
})

// M71 — 侧边栏「审计日志」入口（audit 能力门禁）。
// 与 M61 同模式，但 audit 能力是 admin/ops_admin/auditor 三个角色共享，
// 所以「应该出现」的判定比 M61 宽：除了「完全没有 audit 能力」之外都应该有。
describe('M71 侧边栏审计日志入口（audit 能力门禁）', () => {
  it('buildMenuItems：无 audit 能力 → 没有 /audit；有 → 有，且其余菜单项一个不少', () => {
    const without = keysOf(buildMenuItems(false, false))
    const withAudit = keysOf(buildMenuItems(false, true))

    expect(without).not.toContain('/audit')
    expect(withAudit).toContain('/audit')
    // 只多这一项（防「条件渲染」顺手把别的菜单项吞掉）
    expect(withAudit.filter((k) => !without.includes(k))).toEqual(['/audit'])
    // 顺序：审计日志在系统设置之前，与 buildMenuItems 字面顺序一致
    expect(withAudit.indexOf('/audit')).toBeLessThan(withAudit.indexOf('/settings'))
  })

  it('admin（capabilities 含 audit）→ 侧边栏出现「审计日志」', async () => {
    renderAppLayout(['read', 'write', 'manage', 'audit', 'identity'])
    expect(await screen.findByText('审计日志')).toBeInTheDocument()
  })

  it('ops_admin / readonly（无 audit 能力）→ 侧边栏不出现「审计日志」（点进去只会 403）', async () => {
    renderAppLayout(['read', 'write', 'manage']) // 没有 audit
    expect(await screen.findByText('系统设置')).toBeInTheDocument()
    expect(screen.queryByText('审计日志')).toBeNull()
  })

  it('/auth/me 失败 → fail-closed，审计日志入口不显示', async () => {
    vi.spyOn(api, 'get').mockRejectedValue(new Error('network down') as never)
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={['/404']}>
          <AppLayout />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(await screen.findByText('系统设置')).toBeInTheDocument()
    expect(screen.queryByText('审计日志')).toBeNull()
  })

  it('capabilities 形状异常 → fail-closed，审计日志入口不显示', async () => {
    // 与 M61 同口径：capabilities 字段是 string 而不是数组时，Array.isArray 兜底返空
    renderAppLayout('read,audit')
    expect(await screen.findByText('系统设置')).toBeInTheDocument()
    expect(screen.queryByText('审计日志')).toBeNull()
  })
})
