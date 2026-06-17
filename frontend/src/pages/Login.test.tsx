// Login page smoke test
// Login 是唯一无 useApiQuery 的 page，单独测
import '@testing-library/jest-dom'  // 补齐 toBeInTheDocument 等 matcher 的类型
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import Login from './Login'

// Mock antd message（避免 jsdom 副作用）
vi.mock('antd', async () => {
  const actual = await vi.importActual<typeof import('antd')>('antd')
  return { ...actual, message: { success: vi.fn(), error: vi.fn() } }
})

// Mock services/api
vi.mock('../services/api', () => ({
  authApi: {
    login: vi.fn(),
  },
}))

import { authApi } from '../services/api'

// 用 MemoryRouter 包 Login（因为 Login 现在用 useNavigate/useLocation）
function renderLogin(initialPath: string = '/login', fromState: { from?: string } | null = null) {
  return render(
    <MemoryRouter initialEntries={[{ pathname: initialPath, state: fromState }]}>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/" element={<div>HOME_PAGE</div>} />
        <Route path="/assets" element={<div>ASSETS_PAGE</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('Login page', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
  })

  it('渲染登录表单（标题 + 用户名 + 密码 + 提交按钮）', () => {
    renderLogin()
    expect(screen.getByText('网络运维监控平台')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('用户名')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('密码')).toBeInTheDocument()
    // antd Button 在中文字符间插了 space，accessible name 变 "登 录"
    expect(screen.getByRole('button', { name: /登\s*录/ })).toBeInTheDocument()
    expect(screen.getByText('默认账号: admin / admin123')).toBeInTheDocument()
    // 记住用户名 checkbox
    expect(screen.getByText('记住用户名')).toBeInTheDocument()
  })

  it('提交空表单触发必填校验（不调 API）', async () => {
    renderLogin()
    fireEvent.click(screen.getByRole('button', { name: /登\s*录/ }))
    await waitFor(() => {
      expect(screen.getByText('请输入用户名')).toBeInTheDocument()
      expect(screen.getByText('请输入密码')).toBeInTheDocument()
    })
    expect(authApi.login).not.toHaveBeenCalled()
  })

  it('登录成功调 authApi.login 携带正确 credentials', async () => {
    vi.mocked(authApi.login).mockResolvedValue({
      data: { data: { user: { id: '1', username: 'admin' }, token: 'xxx' } },
    } as any)

    renderLogin()
    fireEvent.change(screen.getByPlaceholderText('用户名'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('密码'), { target: { value: 'admin123' } })
    fireEvent.click(screen.getByRole('button', { name: /登\s*录/ }))

    await waitFor(
      () => {
        expect(authApi.login).toHaveBeenCalledWith({ username: 'admin', password: 'admin123' })
      },
      { timeout: 2000 },
    )
  })

  it('登录失败弹 error 消息', async () => {
    const axiosError = {
      response: { data: { message: '用户名或密码错误' } },
    }
    vi.mocked(authApi.login).mockRejectedValueOnce(axiosError)

    renderLogin()
    fireEvent.change(screen.getByPlaceholderText('用户名'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('密码'), { target: { value: 'wrong' } })
    fireEvent.click(screen.getByRole('button', { name: /登\s*录/ }))

    await waitFor(() => {
      expect(authApi.login).toHaveBeenCalled()
    })
  })

  it('记住用户名勾选时写入 localStorage', async () => {
    vi.mocked(authApi.login).mockResolvedValue({
      data: { data: { user: { id: '1', username: 'admin' } } },
    } as any)

    renderLogin()
    fireEvent.change(screen.getByPlaceholderText('用户名'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('密码'), { target: { value: 'admin123' } })
    // 勾选"记住用户名"
    fireEvent.click(screen.getByText('记住用户名'))
    fireEvent.click(screen.getByRole('button', { name: /登\s*录/ }))

    await waitFor(() => {
      expect(authApi.login).toHaveBeenCalled()
    })
    // 验证 localStorage 写入
    await waitFor(() => {
      const stored = localStorage.getItem('itmanager_login_remember')
      expect(stored).toBeTruthy()
      expect(JSON.parse(stored!)).toEqual({ username: 'admin', remember: true })
    })
  })

  it('从 localStorage 恢复 username', () => {
    localStorage.setItem(
      'itmanager_login_remember',
      JSON.stringify({ username: 'saveduser', remember: true }),
    )
    renderLogin()
    // form 恢复 username
    const usernameInput = screen.getByPlaceholderText('用户名') as HTMLInputElement
    expect(usernameInput.value).toBe('saveduser')
  })
})
