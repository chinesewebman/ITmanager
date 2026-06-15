// vitest 全局 setup：注册 jest-dom matchers + 抑制 antd message.error 等副作用
console.log('[setup] loading...')
import '@testing-library/jest-dom/vitest'
import { afterEach, vi } from 'vitest'
import { cleanup } from '@testing-library/react'
console.log('[setup] jest-dom matchers extended')

// 每次测试后 unmount 组件（避免 React 状态泄漏）
afterEach(() => {
  cleanup()
})

// antd v5 + React 18 在 jsdom 下需要 matchMedia polyfill
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })),
})

// antd ResponsiveObserve / getComputedStyle 在 jsdom 下需要
window.getComputedStyle = window.getComputedStyle || (() => ({ getPropertyValue: () => '' })) as any

// Mock antd message 避免 jsdom 报 ReactDOM.unstable_batchedUpdates 警告
vi.mock('antd', async () => {
  const actual = await vi.importActual<typeof import('antd')>('antd')
  return {
    ...actual,
    message: {
      success: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
      info: vi.fn(),
    },
  }
})
