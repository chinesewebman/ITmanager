// React Query 统一封装（C-P9）。
// 优势：
//   - 自动缓存 + 后台 refetch（用户切回 tab 数据自动刷新）
//   - 失败重试 + 统一 loading/error 状态
//   - 写操作 invalidate 关联查询，避免手动 refetch
//   - 5xx 自动重试（业务错 4xx 不重试）
import { useMutation, useQuery } from '@tanstack/react-query'

// 1. 业务 query key 工厂（统一前缀，便于 invalidate 整棵树）
export const queryKeys = {
  assets: {
    all: ['assets'] as const,
    list: (filters?: Record<string, unknown>) => ['assets', 'list', filters ?? {}] as const,
  },
  alerts: {
    all: ['alerts'] as const,
    list: (filters?: Record<string, unknown>) => ['alerts', 'list', filters ?? {}] as const,
    stats: () => ['alerts', 'stats'] as const,
  },
  racks: {
    all: ['racks'] as const,
    list: () => ['racks', 'list'] as const,
    devices: (id: string) => ['racks', 'devices', id] as const,
  },
  tickets: {
    all: ['tickets'] as const,
    list: (filters?: Record<string, unknown>) => ['tickets', 'list', filters ?? {}] as const,
    stats: () => ['tickets', 'stats'] as const,
  },
  dashboard: {
    stats: () => ['dashboard', 'stats'] as const,
    trends: () => ['dashboard', 'trends'] as const,
  },
}

// 2. useApiQuery — 列表/详情查询统一入口
// 典型用法：
//   const { data, isLoading } = useApiQuery(
//     queryKeys.assets.list(),
//     () => assetApi.list().then(r => r.data.data.items),
//   )
export function useApiQuery<T>(
  key: readonly unknown[],
  fetcher: () => Promise<T>,
  options?: { staleTime?: number; enabled?: boolean },
) {
  return useQuery<T>({
    queryKey: key,
    queryFn: fetcher,
    staleTime: options?.staleTime ?? 30_000, // 30s 内不重 fetch
    enabled: options?.enabled,
    retry: (failureCount, error: any) => {
      // 4xx 业务错不重试
      const status = error?.response?.status
      if (status && status >= 400 && status < 500) return false
      // 5xx/网络错重试 2 次
      return failureCount < 2
    },
  })
}

// 3. useApiMutation — 写操作统一入口
// 典型用法：
//   const create = useApiMutation(assetApi.create, {
//     onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.assets.all })
//   })
export function useApiMutation<TVars, TResult>(
  mutator: (vars: TVars) => Promise<TResult>,
  options?: {
    onSuccess?: (result: TResult, vars: TVars) => void
    onError?: (error: unknown, vars: TVars) => void
  },
) {
  return useMutation({
    mutationFn: mutator,
    onSuccess: (result, vars) => {
      options?.onSuccess?.(result, vars)
    },
    onError: (err, vars) => {
      options?.onError?.(err, vars)
    },
  })
}
