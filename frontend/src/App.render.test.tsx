// M61 — App 顶层可渲染性（真实浏览器首屏为空白页那条缺陷的回归钉子）。
//
// 缺陷：`<CommandPalette />` 挂在 `<BrowserRouter>` **外面**，而它内部调 `useNavigate()`
// → react-router 的 invariant 抛错 → React 卸载整棵树 → **整个应用白屏**
// （真浏览器实测：root 空、只有 `useNavigate() may be used only in the context of a
// <Router> component` 一条未捕获异常）。自 f7e98eb 引入起一直如此。
//
// 为什么此前所有用例都是绿的：`App.theme.test.tsx` 只渲染 `buildTheme` 的产物，
// `App.menu.test.tsx` / 各页用例都渲染 `AppLayout` 或页面本身并自己包了 Router ——
// **没有任何用例渲染过 `<App />` 整体**。于是「应用起不来」这件事全仓无守卫。
//
// 这条用例的价值在于它的失败模式：它不测任何业务，只测「App 挂载不抛异常且渲染出内容」——
// 恰恰是白屏这类缺陷唯一会被抓到的粒度。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import App from './App'
import api from './services/api'

beforeEach(() => {
  vi.clearAllMocks()
  // 除 /auth/me 外一律拒绝：本用例只关心挂载，任何别的请求都说明渲染路径比预期深
  vi.spyOn(api, 'get').mockImplementation(((url: string) => {
    if (url === '/auth/me') {
      return Promise.resolve({ data: { code: 0, data: { role: 'admin', capabilities: ['read', 'identity'] } } })
    }
    return Promise.reject(new Error('unexpected request: ' + url))
  }) as never)
})

/**
 * 渲染 `<App />`。QueryClientProvider 由 `main.tsx` 提供（生产入口就是这两层），
 * 这里照抄同一层数 —— 本用例要证的是「真实入口形状下 App 能挂载」，
 * 多加/少加一层就不再是那个形状了。
 */
function renderApp() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 0 } } })
  return render(
    <QueryClientProvider client={qc}>
      <App />
    </QueryClientProvider>,
  )
}

describe('M61 App 顶层可渲染（白屏回归）', () => {
  it('未登录：渲染登录页，不抛异常', async () => {
    localStorage.removeItem('user')
    renderApp()
    // Login 页的标题（App 内部自带 BrowserRouter，故这里不能再包一层）
    expect(await screen.findByText('网络运维监控平台')).toBeInTheDocument()
  })

  it('已登录：渲染带侧边栏的布局（CommandPalette 不得因缺 Router 上下文炸掉整棵树）', async () => {
    localStorage.setItem('user', JSON.stringify({ id: 'u1', username: 'admin', nickname: '管理员', role: 'admin' }))
    renderApp()
    // 侧边栏第一项出现 = 整棵树挂载成功（白屏时这里永远找不到）
    expect(await screen.findByText('仪表盘')).toBeInTheDocument()
    expect(screen.getByText('资产管理')).toBeInTheDocument()
  })
})
