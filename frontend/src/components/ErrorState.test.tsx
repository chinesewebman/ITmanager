import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ErrorState } from './ErrorState'

describe('ErrorState', () => {
  it('渲染标题与重试按钮，点击触发 onRetry', () => {
    const onRetry = vi.fn()
    render(<ErrorState status={500} onRetry={onRetry} />)
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    expect(screen.getByText('服务端暂时不可用，请稍后重试。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('从 error 对象推导状态码（不传 status）', () => {
    render(<ErrorState error={{ response: { status: 404 } }} onRetry={vi.fn()} />)
    expect(screen.getByText('请求的数据不存在或已被删除。')).toBeInTheDocument()
  })

  it('403 不提供重试按钮（重试不会改变权限）', () => {
    render(<ErrorState status={403} onRetry={vi.fn()} />)
    expect(screen.getByText('当前账号没有查看该数据的权限。')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /重\s*试/ })).toBeNull()
  })

  it('401 不提供重试按钮', () => {
    render(<ErrorState status={401} onRetry={vi.fn()} />)
    expect(screen.queryByRole('button', { name: /重\s*试/ })).toBeNull()
  })

  it('无状态码时按「无法连接服务」渲染', () => {
    render(<ErrorState />)
    expect(screen.getByText('无法连接服务，请检查网络或稍后重试。')).toBeInTheDocument()
  })

  it('无 onRetry 时不渲染按钮', () => {
    render(<ErrorState status={500} />)
    expect(screen.queryByRole('button', { name: /重\s*试/ })).toBeNull()
  })
})
