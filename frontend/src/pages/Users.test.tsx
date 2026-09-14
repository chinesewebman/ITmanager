// M61 G-User-AdminManagement — 用户管理页用例。
//
// 这一层**不打桩组件依赖**（真 useApiQuery/useApiMutation + 真 userApi + 真 antd
// Table/Switch/Select/Popconfirm），只在最外层的 axios 实例上打桩 api.get/patch/put ——
// 于是断言能同时钉住两件事：
//   ① 页面把哪些请求发出去（**真实路径与请求体形状**：路径写成 /user/:id、
//      body 写成 {state} 这类漂移会立刻红，那正是 B1-1 /auth/api-keys 的教训）；
//   ② 乐观更新的窗口语义（先翻 → 失败回滚 → 成功以服务端回执为准）。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import type { MockInstance } from 'vitest'
import { message } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Users from './Users'
import api from '../services/api'

// 三行用户：admin（启用）/ 离职同事（启用）/ 已禁用账号。
// last_login 一个为 null（从未登录），一个带值（时间列走 utils/time 格式化）。
const USERS = [
  {
    id: 'u1',
    username: 'admin',
    nickname: '管理员',
    email: 'admin@example.com',
    role: 'admin',
    status: 'active',
    last_login: '2026-09-14T10:00:00',
  },
  {
    id: 'u2',
    username: 'zhangsan',
    nickname: '张三',
    email: 'zhangsan@example.com',
    role: 'ops_user',
    status: 'active',
    last_login: null,
  },
  {
    id: 'u3',
    username: 'lisi',
    nickname: '李四',
    email: 'lisi@example.com',
    role: 'readonly',
    status: 'inactive',
    last_login: '2026-09-01T08:30:00',
  },
]

/** 后端列表信封：`{code, data:{items, total, page, page_size}}`（openapi UserList）。 */
function envelope(items: unknown[], total = items.length) {
  return { data: { code: 0, data: { items, total, page: 1, page_size: 20 } } }
}

/** 单对象信封（PATCH/PUT 的响应）。 */
function one(user: Record<string, unknown>) {
  return { data: { code: 0, data: user } }
}

type GetArgs = [string, { params?: Record<string, unknown> } | undefined]

function renderUsers() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 0 } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <Users />
    </QueryClientProvider>,
  )
}

/**
 * 当前**可见**的 Popconfirm 的确认按钮；气泡不可见时返回 null。
 *
 * 「不可见」的判据必须比 `querySelector` 更细：antd 关闭气泡走的是 leave 过渡
 * （`ant-popover-hidden` 要等 transitionend，jsdom 不触发过渡事件，于是停在
 * `ant-zoom-big-leave*` 上）。只看节点是否存在会让「取消了」的断言永远红 ——
 * 而节点在不在正是用例要区分的事。
 *
 * **不能**拿 `style.left === '-1000vw'` 当隐藏判据：那是 rc-trigger 定位前的初始位置，
 * 在 jsdom 里定位（rc-align 读 getBoundingClientRect）常常不完成，可见的气泡也停在那。
 */
function visiblePopconfirmOK(): HTMLElement | null {
  const pc = document.querySelector('.ant-popconfirm') as HTMLElement | null
  if (!pc) return null
  if (pc.className.includes('ant-popover-hidden')) return null
  if (pc.className.includes('ant-zoom-big-leave')) return null
  return pc.querySelector('.ant-btn-primary') as HTMLElement | null
}

/** 点开 Popconfirm 的确认按钮（antd 把 OK 渲染成 .ant-popconfirm 里的 primary 按钮）。 */
async function clickPopconfirmOK() {
  const ok = await waitFor(() => {
    const btn = visiblePopconfirmOK()
    if (!btn) throw new Error('Popconfirm OK button not found')
    return btn
  })
  fireEvent.click(ok)
}

/** 展开展开某一行的角色下拉。antd 的 Select 把 onMouseDown 挂在 `.ant-select-selector`
 * 上（不是根节点）—— 在根节点 fireEvent.mouseDown 不会展开，选项就永远找不到。 */
function openRoleDropdown(rowTestId: string) {
  const root = screen.getByTestId(rowTestId)
  fireEvent.mouseDown(root.querySelector('.ant-select-selector') ?? root)
}

/**
 * 在已展开的角色下拉里选一项。
 *
 * 按 `.ant-select-item-option-content` 的文本找，而不是 `getByTitle` 或 `getByText`：
 *   - antd v5 的选项节点不带 title 属性（getByTitle 恒空）；
 *   - 关着的 Select 已把当前值渲染成同样的文案，getByText 会撞上表格里那一行（只读用户
 *     (readonly) 同时是 u3 的当前值和候选值）→ 必须限定在**下拉面板**的选项节点里。
 */
async function pickRoleOption(label: string) {
  const opt = await waitFor(() => {
    const el = Array.from(document.querySelectorAll('.ant-select-item-option-content')).find(
      (n) => n.textContent === label,
    ) as HTMLElement | undefined
    if (!el) throw new Error(`role option not found: ${label}`)
    return el
  })
  fireEvent.click(opt)
}

/** 一次列表请求的 [url, config]（列表请求带 page_size）。 */
function lastListCall(spy: MockInstance): GetArgs | undefined {
  const calls = (spy.mock.calls as GetArgs[]).filter(([, cfg]) => cfg?.params?.page_size !== undefined)
  return calls.length ? calls[calls.length - 1] : undefined
}

beforeEach(() => {
  // 只清调用记录，**不用** vi.restoreAllMocks()（会连 setup.ts 的 matchMedia 实现一起清掉，
  // antd 的 responsiveObserver 随即在解构 undefined 上炸 —— 同 Audit.test.tsx 的注释）。
  vi.clearAllMocks()
  vi.mocked(message.error).mockClear()
  vi.mocked(message.success).mockClear()
})

describe('M61 用户管理页', () => {
  it('渲染列表：用户名/邮箱/角色/状态/最后登录；请求走 GET /users 且带 page_size', async () => {
    const spy = vi.spyOn(api, 'get').mockResolvedValue(envelope(USERS) as never)
    renderUsers()

    expect(await screen.findByText('zhangsan')).toBeInTheDocument()
    expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    // 角色显示中文名 + 词表值（下拉选项与展示同源）
    expect(screen.getByText('运维人员 (ops_user)')).toBeInTheDocument()
    // 已禁用行显示「禁用」，未登录显示「从未登录」
    expect(screen.getByText('禁用')).toBeInTheDocument()
    expect(screen.getByText('从未登录')).toBeInTheDocument()
    // 时间列走 utils/time 统一格式（T 分隔 → 空格）
    expect(screen.getByText('2026-09-14 10:00:00')).toBeInTheDocument()

    expect(lastListCall(spy)?.[0]).toBe('/users')
    expect(lastListCall(spy)?.[1]?.params).toMatchObject({ page: 1, page_size: 20 })
  })

  it('列表为空：显示空态（不是空白表格），且不发任何写请求', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(envelope([]) as never)
    const patchSpy = vi.spyOn(api, 'patch')
    const putSpy = vi.spyOn(api, 'put')
    renderUsers()

    expect(await screen.findByText('没有用户')).toBeInTheDocument()
    expect(patchSpy).not.toHaveBeenCalled()
    expect(putSpy).not.toHaveBeenCalled()
  })

  it('禁用账号：点 Switch → 弹确认 → 确认后 PATCH /users/:id/status {status:"inactive"} → 列表重取', async () => {
    const getSpy = vi.spyOn(api, 'get').mockResolvedValue(envelope(USERS) as never)
    const patchSpy = vi
      .spyOn(api, 'patch')
      .mockResolvedValue(one({ ...USERS[1], status: 'inactive' }) as never)

    renderUsers()
    await screen.findByText('zhangsan')

    // 张三当前是启用态 → Switch 应为 checked
    const sw = screen.getByTestId('user-status-u2')
    expect(sw).toHaveAttribute('aria-checked', 'true')

    fireEvent.click(sw)
    // 确认文案必须点名是要禁用谁（「禁用账号」+ 用户名）
    expect(await screen.findByText('禁用账号')).toBeInTheDocument()
    expect(document.querySelector('.ant-popconfirm')?.textContent).toContain('zhangsan')

    await clickPopconfirmOK()

    await waitFor(() => expect(patchSpy).toHaveBeenCalledTimes(1))
    // 真实路径与**请求体字段名**（写成 {state} / /user/:id 会在这里红）
    expect(patchSpy.mock.calls[0]).toEqual(['/users/u2/status', { status: 'inactive' }])

    // 成功后 invalidate → 列表重取（第二次 GET）
    await waitFor(() => {
      const listCalls = (getSpy.mock.calls as GetArgs[]).filter(([, c]) => c?.params?.page_size !== undefined)
      expect(listCalls.length).toBeGreaterThanOrEqual(2)
    })
    expect(vi.mocked(message.success)).toHaveBeenCalledWith('状态已更新')
  })

  it('M61-MUT：[状态切换] 不点确认 → 不发请求、状态不变（mutation inversion 对照）', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(envelope(USERS) as never)
    const patchSpy = vi.spyOn(api, 'patch')

    renderUsers()
    await screen.findByText('zhangsan')

    fireEvent.click(screen.getByTestId('user-status-u2'))
    // 只打开确认框，不点确认
    expect(await screen.findByText('禁用账号')).toBeInTheDocument()
    expect(patchSpy).not.toHaveBeenCalled()
    expect(screen.getByTestId('user-status-u2')).toHaveAttribute('aria-checked', 'true')
  })

  it('启用已禁用账号：PATCH body 是 {status:"active"}（方向不能反）', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(envelope(USERS) as never)
    const patchSpy = vi
      .spyOn(api, 'patch')
      .mockResolvedValue(one({ ...USERS[2], status: 'active' }) as never)

    renderUsers()
    await screen.findByText('lisi')

    fireEvent.click(screen.getByTestId('user-status-u3'))
    expect(await screen.findByText('启用账号')).toBeInTheDocument()
    await clickPopconfirmOK()

    await waitFor(() => expect(patchSpy).toHaveBeenCalledTimes(1))
    expect(patchSpy.mock.calls[0]).toEqual(['/users/u3/status', { status: 'active' }])
  })

  it('改角色：选新角色 → 弹确认 → 确认后 PATCH /users/:id/role {role:"ops_admin"}', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(envelope(USERS) as never)
    const patchSpy = vi
      .spyOn(api, 'patch')
      .mockResolvedValue(one({ ...USERS[1], role: 'ops_admin' }) as never)

    renderUsers()
    await screen.findByText('zhangsan')

    // 打开角色下拉并选「运维管理员」(ops_admin)
    openRoleDropdown('user-role-u2')
    await pickRoleOption('运维管理员 (ops_admin)')

    // 选中即弹确认（受控 Popconfirm），未确认前不发请求
    await waitFor(() => expect(document.querySelector('.ant-popconfirm')).toBeTruthy())
    expect(patchSpy).not.toHaveBeenCalled()
    expect(document.querySelector('.ant-popconfirm')?.textContent).toContain('zhangsan')

    await clickPopconfirmOK()
    await waitFor(() => expect(patchSpy).toHaveBeenCalledTimes(1))
    expect(patchSpy.mock.calls[0]).toEqual(['/users/u2/role', { role: 'ops_admin' }])
  })

  it('改角色：取消确认 → 不发请求，下拉回到原值', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(envelope(USERS) as never)
    const patchSpy = vi.spyOn(api, 'patch')

    renderUsers()
    await screen.findByText('zhangsan')

    openRoleDropdown('user-role-u2')
    await pickRoleOption('审计员 (auditor)')
    await waitFor(() => expect(document.querySelector('.ant-popconfirm')).toBeTruthy())

    const cancelBtn = document.querySelector('.ant-popconfirm .ant-btn-default') as HTMLButtonElement
    fireEvent.click(cancelBtn)

    // antd 关掉气泡是加 hidden class（节点留在 DOM），故判据是「可见的确认按钮消失」
    await waitFor(() => {
      expect(visiblePopconfirmOK()).toBeNull()
    })
    expect(patchSpy).not.toHaveBeenCalled()
    // 下拉值 = 行数据的原值（没被候选值污染）
    expect(screen.getByTestId('user-role-u2').textContent).toContain('运维人员')
  })

  it('admin 自我禁用（后端 403）→ 显示服务端原因 + Switch 回滚到原值', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(envelope(USERS) as never)
    // 403 是策略拒绝：改参数重试无用，前端必须回滚而不是留着一个假装成功的开关
    const patchSpy = vi.spyOn(api, 'patch').mockRejectedValue({
      response: { status: 403, data: { code: 'forbidden', message: '不能禁用自己的账号' } },
    } as never)

    renderUsers()
    await screen.findByText('admin')

    fireEvent.click(screen.getByTestId('user-status-u1'))
    await clickPopconfirmOK()

    await waitFor(() => expect(patchSpy).toHaveBeenCalledTimes(1))
    // 乐观翻动发生在确认时 → 失败后必须回到 true
    await waitFor(() => {
      expect(screen.getByTestId('user-status-u1')).toHaveAttribute('aria-checked', 'true')
    })
    // 显示的是**服务端原因**（比拦截器的通用「没有权限访问」更能说清为什么）
    expect(vi.mocked(message.error)).toHaveBeenCalledWith('不能禁用自己的账号')
  })

  it('降级最后一名管理员（后端 403）→ 角色回滚到原值', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(envelope(USERS) as never)
    vi.spyOn(api, 'patch').mockRejectedValue({
      response: {
        status: 403,
        data: { code: 'forbidden', message: '这是最后一名可登录的管理员，禁用/降级后系统将无人能管理用户' },
      },
    } as never)

    renderUsers()
    await screen.findByText('admin')

    openRoleDropdown('user-role-u1')
    await pickRoleOption('运维管理员 (ops_admin)')
    await clickPopconfirmOK()

    await waitFor(() =>
      expect(vi.mocked(message.error)).toHaveBeenCalledWith(
        '这是最后一名可登录的管理员，禁用/降级后系统将无人能管理用户',
      ),
    )
    expect(screen.getByTestId('user-role-u1').textContent).toContain('超级管理员')
  })

  it('角色词表外（后端 400）→ 显示原因 + 角色回滚', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(envelope(USERS) as never)
    vi.spyOn(api, 'patch').mockRejectedValue({
      response: {
        status: 400,
        data: { code: 'bad_request', message: 'invalid input: 未知角色 "superuser"' },
      },
    } as never)

    renderUsers()
    await screen.findByText('zhangsan')

    openRoleDropdown('user-role-u2')
    await pickRoleOption('只读用户 (readonly)')
    await clickPopconfirmOK()

    await waitFor(() =>
      expect(vi.mocked(message.error)).toHaveBeenCalledWith('invalid input: 未知角色 "superuser"'),
    )
    expect(screen.getByTestId('user-role-u2').textContent).toContain('运维人员')
  })

  it('强制改密：确认后 PUT /users/:id {must_change_password:true}（不是 admin 设新密码）', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(envelope(USERS) as never)
    const putSpy = vi.spyOn(api, 'put').mockResolvedValue(one(USERS[1]) as never)
    const patchSpy = vi.spyOn(api, 'patch')

    renderUsers()
    await screen.findByText('zhangsan')

    fireEvent.click(screen.getByTestId('user-force-change-u2'))
    expect(await screen.findByText('强制下次登录改密')).toBeInTheDocument()
    await clickPopconfirmOK()

    await waitFor(() => expect(putSpy).toHaveBeenCalledTimes(1))
    expect(putSpy.mock.calls[0]).toEqual(['/users/u2', { must_change_password: true }])
    // 走 PUT 的第三个字段，不是 PATCH status/role（两个窄端点都不收这个键 → 后端 400）
    expect(patchSpy).not.toHaveBeenCalled()
    expect(vi.mocked(message.success)).toHaveBeenCalledWith('已置为下次登录必须改密')
  })

  it('列表加载失败：显示错误态（不回落假数据）', async () => {
    vi.spyOn(api, 'get').mockRejectedValue({
      response: { status: 500, data: { message: 'boom' } },
    } as never)
    renderUsers()

    // useApiQuery 对 5xx 会重试 2 次（指数退避 1s+2s），故这里放宽等待窗口
    expect(await screen.findByText('用户列表加载失败', {}, { timeout: 6000 })).toBeInTheDocument()
    expect(screen.queryByText('zhangsan')).toBeNull()
  })

  it('分页：第 2 页带 page=2（服务端分页，不是本地假分页）', async () => {
    const getSpy = vi.spyOn(api, 'get').mockResolvedValue(envelope(USERS, 40) as never)
    renderUsers()
    await screen.findByText('zhangsan')

    // 关掉 popconfirm 干扰，点分页第 2 页
    fireEvent.click(screen.getByTitle('2'))
    await waitFor(() => {
      expect(lastListCall(getSpy)?.[1]?.params).toMatchObject({ page: 2, page_size: 20 })
    })
  })

  it('行级校验：缺 id/status 的行不渲染（外部数据边界），且不影响其余行', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(
      envelope([
        USERS[0],
        { username: 'half-row', role: 'ops_user' }, // 缺 id/status：不是用户行
      ]) as never,
    )
    const { container } = renderUsers()

    expect(await screen.findByText('admin')).toBeInTheDocument()
    expect(screen.queryByText('half-row')).toBeNull()
    expect(container.querySelectorAll('tbody tr[data-row-key]').length).toBe(1)
  })
})
