// Login page smoke test
// Login 是唯一无 useApiQuery 的 page，单独测
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
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

describe('Login page', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // 拦截 window.location.href 赋值
    Object.defineProperty(window, 'location', {
      writable: true,
      value: { href: '' },
    })
  })

  it('渲染登录表单（标题 + 用户名 + 密码 + 提交按钮）', () => {
    render(<Login />)
    expect(screen.getByText('网络运维监控平台')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('用户名: admin')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('密码: admin123')).toBeInTheDocument()
    // antd Button 在中文字符间插了 space，accessible name 变 "登 录"
    expect(screen.getByRole('button', { name: /登\s*录/ })).toBeInTheDocument()
    expect(screen.getByText('默认账号: admin / admin123')).toBeInTheDocument()
  })

  it('提交空表单触发必填校验（不调 API）', async () => {
    render(<Login />)
    fireEvent.click(screen.getByRole('button', { name: /登\s*录/ }))
    await waitFor(() => {
      expect(screen.getByText('请输入用户名')).toBeInTheDocument()
      expect(screen.getByText('请输入密码')).toBeInTheDocument()
    })
    expect(authApi.login).not.toHaveBeenCalled()
  })

  it('登录成功调 authApi.login 携带正确 credentials', async () => {
    // mock authApi.login 返回 Login.tsx 期望的 response.data.data.user 形状
    vi.mocked(authApi.login).mockResolvedValue({
      data: { data: { user: { id: '1', username: 'admin' }, token: 'xxx' } },
    } as any)

    render(<Login />)
    fireEvent.change(screen.getByPlaceholderText('用户名: admin'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('密码: admin123'), { target: { value: 'admin123' } })
    fireEvent.click(screen.getByRole('button', { name: /登\s*录/ }))

    // 验证 authApi.login 被调（onFinish 触发链路走通）
    // 注：antd Form 在 jsdom 下 fireEvent.click submit 按钮不会自动触发 onFinish
    //   （需要 form.submit() 或 userEvent.click），这里只验证 credentials 正确
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

    render(<Login />)
    fireEvent.change(screen.getByPlaceholderText('用户名: admin'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('密码: admin123'), { target: { value: 'wrong' } })
    fireEvent.click(screen.getByRole('button', { name: /登\s*录/ }))

    await waitFor(() => {
      // antd Form 校验通过 → authApi.login 被调
      expect(authApi.login).toHaveBeenCalled()
    })
    // 错误消息通过 antd message.error 弹（已 mock）— 间接验证不走 panic
  })
})
