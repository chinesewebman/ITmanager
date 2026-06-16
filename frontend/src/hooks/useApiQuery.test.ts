import { describe, it, expect } from 'vitest'
import { queryKeys } from './useApiQuery'

describe('queryKeys 工厂', () => {
  it('assets.all 始终为 ["assets"]', () => {
    expect(queryKeys.assets.all).toEqual(['assets'])
  })

  it('assets.list 无 filters 时第三个元素是空对象', () => {
    expect(queryKeys.assets.list()).toEqual(['assets', 'list', {}])
  })

  it('assets.list 传 filters 时第三个元素是 filters 对象', () => {
    const filters = { status: 'active', type: 'server' }
    expect(queryKeys.assets.list(filters)).toEqual(['assets', 'list', filters])
  })

  it('alerts.stats 不需要参数', () => {
    expect(queryKeys.alerts.stats()).toEqual(['alerts', 'stats'])
  })

  it('racks.devices 接收 id 参数', () => {
    expect(queryKeys.racks.devices('rack-123')).toEqual(['racks', 'devices', 'rack-123'])
  })

  it('不同 store 的 key 不会互相污染', () => {
    expect(queryKeys.assets.all[0]).toBe('assets')
    expect(queryKeys.alerts.all[0]).toBe('alerts')
    expect(queryKeys.racks.all[0]).toBe('racks')
    expect(queryKeys.tickets.all[0]).toBe('tickets')
    expect(queryKeys.dashboard.stats()[0]).toBe('dashboard')
  })

  it('dashboard.trends / dashboard.stats 区分', () => {
    expect(queryKeys.dashboard.trends()).toEqual(['dashboard', 'trends'])
    expect(queryKeys.dashboard.stats()).toEqual(['dashboard', 'stats'])
  })

  it('filter 引用不同则 key 不同（cache miss 行为）', () => {
    // 验证 key 包含 filters 引用 —— 即使对象内容相同引用不同也会 cache miss
    // (这在 invalidate 操作里要小心)
    const k1 = queryKeys.assets.list({ a: 1 })
    const k2 = queryKeys.assets.list({ a: 1 })
    // 不同调用是不同对象引用
    expect(k1).not.toBe(k2)
    expect(k1[2]).not.toBe(k2[2])
  })
})
