import { Button, Result } from 'antd'
import type { ReactNode } from 'react'

/**
 * ErrorState - 数据加载失败时的显式错误态（FIX-PLAN-UI-PERF §W1）。
 *
 * 为什么需要它：此前 11 个页面在接口失败时静默回落到 mock 数据（`catch { return MOCK_* }`
 * 或 `?? MOCK_*`），React Query 的 `isError` 恒为 false —— 错误提示不出现、重试形同虚设、
 * 假数据进缓存。运维看板显示从未发生的告警/资产数，比空白更危险。
 *
 * 用法：
 *   const { data, isLoading, isError, error, refetch } = useApiQuery(...)
 *   if (isError) return <ErrorState error={error} onRetry={refetch} />
 *   // 或局部：<ErrorState status={500} onRetry={refetch} compact />
 *
 * 401/403 不提供「重试」（重试不会改变权限），文案也不诱导用户重试。
 */
export interface ErrorStateProps {
  title?: string
  /** HTTP 状态码；不传时从 error 推导 */
  status?: number
  /** 原始错误（axios error）；用于推导状态码 */
  error?: unknown
  onRetry?: () => void
  extra?: ReactNode
  /** 嵌在 Card/Modal 内时收紧留白 */
  compact?: boolean
}

function statusOf(status?: number, error?: unknown): number | undefined {
  if (typeof status === 'number') return status
  const s = (error as { response?: { status?: unknown } } | undefined)?.response?.status
  return typeof s === 'number' ? s : undefined
}

function describe(status?: number): string {
  if (status === 401) return '登录已过期，请重新登录。'
  if (status === 403) return '当前账号没有查看该数据的权限。'
  if (status === 404) return '请求的数据不存在或已被删除。'
  if (typeof status === 'number' && status >= 500) return '服务端暂时不可用，请稍后重试。'
  if (status === undefined) return '无法连接服务，请检查网络或稍后重试。'
  return '请求失败，请稍后重试。'
}

/** 401/403 重试无意义，不渲染重试按钮。 */
function retryable(status?: number): boolean {
  return status !== 401 && status !== 403
}

function resultStatus(status?: number): '403' | '404' | '500' | 'warning' {
  if (status === 403) return '403'
  if (status === 404) return '404'
  if (typeof status === 'number' && status >= 500) return '500'
  return 'warning'
}

export function ErrorState({
  title = '数据加载失败',
  status,
  error,
  onRetry,
  extra,
  compact = false,
}: ErrorStateProps) {
  const code = statusOf(status, error)
  return (
    <Result
      status={resultStatus(code)}
      title={title}
      subTitle={describe(code)}
      style={compact ? { padding: '24px 0' } : undefined}
      extra={
        extra ??
        (onRetry && retryable(code) ? (
          <Button type="primary" onClick={onRetry}>
            重试
          </Button>
        ) : null)
      }
    />
  )
}

export default ErrorState
