// M49 G-UI-Audit — 审计页用例。
//
// 这一层刻意**不打桩组件依赖**（真 useApiQuery + 真 auditApi + 真 antd Table/Drawer），
// 只在最外层的 axios 实例上打桩 api.get —— 于是断言能同时钉住两件事：
//   ① 页面把哪些 filter 传下去；② auditApi.list 拼出的**真实路径与参数形状**
//      （路径写成 /audit/logs 这类漂移会立刻红，那正是 B1-1 /auth/api-keys 的教训）。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import type { MockInstance } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Audit, { PAGE_SIZE } from './Audit'
import api from '../services/api'
import type { AuditListParams } from '../types'

// 时间串不带时区偏移 → dayjs 按本地解析，断言与 CI 时区无关
const EVENTS = [
  {
    id: 'e1',
    user_id: '11111111-1111-1111-1111-111111111111',
    username: 'alice',
    action: 'update',
    resource: 'assets',
    resource_id: 'a1b2c3d4-0000-0000-0000-000000000000',
    method: 'PUT',
    path: '/api/assets/a1b2c3d4',
    ip: '10.0.0.1',
    user_agent: 'curl/8.4.0',
    status: 200,
    error_msg: '',
    request_id: 'req-1',
    created_at: '2026-09-14T10:00:00',
  },
  {
    id: 'e2',
    // 未认证请求（登录失败）—— user_id 是真 null，且 username 为空
    user_id: null,
    username: '',
    action: 'login',
    resource: 'auth',
    resource_id: null,
    method: 'POST',
    path: '/api/auth/login',
    ip: '10.0.0.9',
    user_agent: 'curl/8.4.0',
    status: 401,
    error_msg: '用户名或密码错误',
    request_id: 'req-2',
    created_at: '2026-09-14T09:00:00',
  },
]

/** 后端信封：cursor 分页，`next_cursor` 只在本页取满 limit 时出现。 */
function envelope(items: unknown[], nextCursor?: string) {
  return { data: { code: 0, data: nextCursor ? { items, next_cursor: nextCursor } : { items } } }
}

type GetArgs = [string, { params?: AuditListParams }]

/** 页面会打两次 GET（列表 + 词表采样），断言只关心列表那次（limit = PAGE_SIZE）。 */
function listCalls(spy: MockInstance) {
  return (spy.mock.calls as GetArgs[]).filter(([, cfg]) => cfg?.params?.limit === PAGE_SIZE)
}

/** 最近一次列表请求的 [url, config]。 */
function lastListCall(spy: MockInstance): GetArgs | undefined {
  const calls = listCalls(spy)
  return calls.length ? calls[calls.length - 1] : undefined
}

function renderAudit() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 0 } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <Audit />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  // 只清调用记录，**不能**用 vi.restoreAllMocks()：它会连同 setup.ts 里
  // window.matchMedia 的 vi.fn() 实现一起清掉，antd 的 responsiveObserver 随即在
  // `({ matches }) => …` 上解构 undefined（Table/Grid 一挂就炸）。
  vi.clearAllMocks()
})

describe('Audit 审计日志页', () => {
  it('渲染列表：时间/操作人/动作/对象/摘要，未认证行显示匿名', async () => {
    const spy = vi.spyOn(api, 'get').mockResolvedValue(envelope(EVENTS) as never)
    renderAudit()

    expect(await screen.findByText('alice')).toBeInTheDocument()
    // 时间列走 utils/time 统一格式（T 分隔 → 空格）
    expect(screen.getByText('2026-09-14 10:00:00')).toBeInTheDocument()
    expect(screen.getByText('update')).toBeInTheDocument()
    expect(screen.getByText('login')).toBeInTheDocument()
    expect(screen.getByText('assets')).toBeInTheDocument()
    // user_id 为 null 的行不能渲染成空白或 "null"
    expect(screen.getByText('匿名')).toBeInTheDocument()
    // 摘要列无 payload，就是 method + path + status
    expect(screen.getByText('/api/auth/login')).toBeInTheDocument()
    expect(screen.getByText('401')).toBeInTheDocument()
    // 首屏列表请求：真实路径 + 30/页 + 无 cursor
    expect(listCalls(spy)[0]).toEqual([
      '/audit-logs',
      {
        params: {
          user_id: undefined,
          action: undefined,
          method: undefined,
          path: undefined,
          cursor: undefined,
          limit: PAGE_SIZE,
        },
      },
    ])
  })

  it('空态分两种：无筛选 → 暂无审计事件；有筛选 → 暂无匹配', async () => {
    // 列表为空、词表仍有一行：筛选下拉不能因为「本页 0 行」就变空（否则用户无从筛选）
    vi.spyOn(api, 'get').mockImplementation(((url: string, cfg?: { params?: AuditListParams }) => {
      if (url !== '/audit-logs') return Promise.reject(new Error(url))
      return Promise.resolve(cfg?.params?.limit === PAGE_SIZE ? envelope([]) : envelope(EVENTS))
    }) as never)
    renderAudit()
    expect(await screen.findByText('暂无审计事件')).toBeInTheDocument()

    // 选一个 action 后同一份空数据要换成「暂无匹配」——两条文案语义不同
    fireEvent.mouseDown(screen.getAllByRole('combobox')[1])
    fireEvent.click(await screen.findByTitle('update'))
    expect(await screen.findByText('暂无匹配')).toBeInTheDocument()
    expect(screen.queryByText('暂无审计事件')).toBeNull()
  })

  it('动作筛选变化：请求带上 action，并回到第一页（不带 cursor）', async () => {
    const spy = vi.spyOn(api, 'get').mockResolvedValue(envelope(EVENTS, 'CUR-2') as never)
    renderAudit()
    await screen.findByText('alice')

    // 先翻到第 2 页（让 cursor 进栈）
    fireEvent.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => {
      expect(lastListCall(spy)?.[1].params?.cursor).toBe('CUR-2')
    })

    // 再改筛选：必须丢掉 cursor（拿着上一页游标过滤会翻到半截）
    fireEvent.mouseDown(screen.getAllByRole('combobox')[1])
    fireEvent.click(await screen.findByTitle('login'))
    await waitFor(() => {
      const call = lastListCall(spy)
      expect(call?.[0]).toBe('/audit-logs')
      expect(call?.[1].params?.action).toBe('login')
      expect(call?.[1].params?.cursor).toBeUndefined()
    })
  })

  it('cursor 分页：无 next_cursor 时「下一页」禁用（不是按本页条数推断）', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(envelope(EVENTS) as never)
    renderAudit()
    await screen.findByText('alice')
    expect(screen.getByRole('button', { name: '下一页' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '上一页' })).toBeDisabled()
  })

  it('详情抽屉：点「详情」展示该行完整 JSON（审计行不含请求体，故无 payload 字段）', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(envelope(EVENTS) as never)
    renderAudit()
    await screen.findByText('alice')

    // 第一行的详情按钮
    fireEvent.click(screen.getAllByRole('button', { name: '详情' })[0])

    const pre = await screen.findByTestId('audit-event-json')
    const parsed = JSON.parse(pre.textContent ?? '{}')
    expect(parsed).toMatchObject({ id: 'e1', path: '/api/assets/a1b2c3d4', method: 'PUT', status: 200 })
    // 契约要点：抽屉里**没有** payload/请求体字段
    expect(pre.textContent).not.toContain('payload')
  })

  it('列表失败 → 显式错误态（不静默回落空列表），重试重新请求', async () => {
    const spy = vi
      .spyOn(api, 'get')
      .mockRejectedValue(Object.assign(new Error('boom'), { response: { status: 500 } }))
    renderAudit()

    // useApiQuery 对 5xx 退避重试 2 次（~3s）才落到 isError，故这里放宽等待窗口
    expect(await screen.findByText('数据加载失败', {}, { timeout: 15000 })).toBeInTheDocument()
    // 关键：失败时不能显示「暂无审计事件」——那会把故障伪装成「没有留痕」
    expect(screen.queryByText('暂无审计事件')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    await waitFor(
      () => {
        expect(listCalls(spy).length).toBeGreaterThan(1)
      },
      { timeout: 5000 },
    )
  })
})
