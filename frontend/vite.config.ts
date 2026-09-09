import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  // vitest 集成：jsdom 环境跑 React 组件测试
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    // antd 页面级用例在 jsdom 下挂载 Tabs + Table + Modal 要 ~7-10s（实测 Settings.tsx
    // 的密钥 tab：仅切 tab 就 5s，本机 2 核与 CI 同档），默认 5000ms 必超时。
    // 只抬天花板，不掩盖死锁：真挂起仍会在 20s 失败。
    testTimeout: 20000,
  },
})
