// AppBreadcrumb smoke test
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AppBreadcrumb } from './AppBreadcrumb'

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/assets/:id/*" element={<AppBreadcrumb />} />
        <Route path="/:top/:id/*" element={<AppBreadcrumb />} />
        <Route path="/*" element={<AppBreadcrumb />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('AppBreadcrumb', () => {
  it('首页不显示面包屑', () => {
    renderAt('/')
    expect(screen.queryByText('首页')).toBeNull()
  })

  it('二级页面显示 首页 / 资产管理', () => {
    renderAt('/assets')
    expect(screen.getByText('首页')).toBeInTheDocument()
    expect(screen.getByText('资产管理')).toBeInTheDocument()
  })

  it('三级页面显示 首页 / 资产管理 / ID: xxx', () => {
    renderAt('/assets/abc123def456')
    expect(screen.getByText('首页')).toBeInTheDocument()
    expect(screen.getByText('资产管理')).toBeInTheDocument()
    expect(screen.getByText(/ID: abc123de/)).toBeInTheDocument()
  })

  it('告警中心页面', () => {
    renderAt('/alerts')
    expect(screen.getByText('告警中心')).toBeInTheDocument()
  })

  it('未匹配路径不显示面包屑 (由 404 页承担)', () => {
    renderAt('/some-unknown-path')
    // 兜底不渲染
    expect(screen.queryByText('资产管理')).toBeNull()
  })
})
