// useDocumentTitle hook smoke test
import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useDocumentTitle } from './useDocumentTitle'

describe('useDocumentTitle', () => {
  const originalTitle = document.title

  beforeEach(() => {
    document.title = originalTitle
  })

  afterEach(() => {
    document.title = originalTitle
  })

  it('设置带后缀的标题 (默认行为)', () => {
    renderHook(() => useDocumentTitle('资产管理'))
    expect(document.title).toBe('资产管理 - 网络运维监控平台')
  })

  it('设置不带后缀的标题 (suffix: false)', () => {
    renderHook(() => useDocumentTitle('资产详情', { suffix: false }))
    expect(document.title).toBe('资产详情')
  })

  it('unmount 时恢复之前的 title', () => {
    document.title = '初始标题'
    const { unmount } = renderHook(() => useDocumentTitle('资产管理'))
    expect(document.title).toBe('资产管理 - 网络运维监控平台')
    unmount()
    expect(document.title).toBe('初始标题')
  })

  it('title 变化时更新 document.title', () => {
    const { rerender } = renderHook(({ title }) => useDocumentTitle(title), {
      initialProps: { title: '告警中心' },
    })
    expect(document.title).toBe('告警中心 - 网络运维监控平台')
    rerender({ title: '工单管理' })
    expect(document.title).toBe('工单管理 - 网络运维监控平台')
  })
})
