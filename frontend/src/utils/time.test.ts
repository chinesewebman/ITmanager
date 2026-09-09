// FIX-PLAN-UI-PERF §W2：时间格式化的唯一出口。
//
// 断言刻意避开固定时区：用本地时间构造 Date 再转 ISO，dayjs 解析回本地后
// 应还原同一串墙上时间——这样在任意 TZ 的 CI 上都稳定。
import { describe, it, expect, vi, afterEach } from 'vitest'
import { formatDateTime, formatRelativeTime } from './time'

describe('formatDateTime', () => {
  it('格式化为 YYYY-MM-DD HH:mm:ss（本地墙上时间）', () => {
    const d = new Date(2026, 8, 9, 12, 34, 56) // 本地时间 2026-09-09 12:34:56
    expect(formatDateTime(d.toISOString())).toBe('2026-09-09 12:34:56')
  })

  it('空值与非法输入返回占位符，不渲染 Invalid Date', () => {
    expect(formatDateTime(undefined)).toBe('—')
    expect(formatDateTime(null)).toBe('—')
    expect(formatDateTime('')).toBe('—')
    expect(formatDateTime('not-a-date')).toBe('—')
  })
})

describe('formatRelativeTime', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('按相对时间渲染中文', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-09T12:00:00Z'))
    const iso = new Date(Date.now() - 3 * 60 * 1000).toISOString()
    expect(formatRelativeTime(iso)).toBe('3 分钟前')
  })

  it('空值与非法输入返回占位符', () => {
    expect(formatRelativeTime(undefined)).toBe('—')
    expect(formatRelativeTime('')).toBe('—')
    expect(formatRelativeTime('N/A')).toBe('—')
    // 注：dayjs 对「组件越界但形状像日期」的串（如 '2026-13-99'）会像 JS Date 一样
    // 滚动进位而不是判非法（实测 lenient/strict 都 true）。真实后端不会发这种值，
    // 这里只保证「无法解析」与「空值」不渲染 Invalid Date。
  })
})
