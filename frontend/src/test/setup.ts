// vitest 全局 setup：注册 jest-dom matchers + 抑制 antd message.error 等副作用
import { expect, afterEach, vi } from 'vitest'
import * as matchers from '@testing-library/jest-dom/matchers'
import { cleanup } from '@testing-library/react'

// 注册 @testing-library/jest-dom matchers（toBeInTheDocument 等）
expect.extend(matchers)

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

// jsdom 默认没 localStorage；zustand persist 需要
const localStorageMock = (() => {
  let store: Record<string, string> = {}
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => { store[key] = value },
    removeItem: (key: string) => { delete store[key] },
    clear: () => { store = {} },
    key: (i: number) => Object.keys(store)[i] ?? null,
    get length() { return Object.keys(store).length },
  }
})()
Object.defineProperty(window, 'localStorage', { value: localStorageMock, writable: true })
