// vitest 测试全局 helper：mock useApiQuery 让组件跳过网络请求
import { vi } from 'vitest'

// 默认 mock：返回 isLoading=false, data=undefined
// 调用方可在测试里 vi.mocked(useApiQuery).mockReturnValueOnce({...}) 覆盖单测
export function mockUseApiQueryDefault() {
  return {
    data: undefined,
    isLoading: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }
}
