// H9：资产状态中文标签与维护态颜色。后端 asset.go:35 值域
// active/offline/maintenance/retired；此前移动端把 maintenance 误标红「离线」。
import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import { StatusTag, statusLabel } from './StatusTag'

describe('statusLabel 资产状态中文标签（H9 单一出口）', () => {
  it('映射 active/offline/maintenance/retired', () => {
    expect(statusLabel('active')).toBe('在线')
    expect(statusLabel('offline')).toBe('离线')
    expect(statusLabel('maintenance')).toBe('维护')
    expect(statusLabel('retired')).toBe('已退役')
  })

  it('未知值原样返回（不吞掉未知状态）', () => {
    expect(statusLabel('bogus')).toBe('bogus')
  })
})

describe('StatusTag 维护态颜色（H9）', () => {
  it('maintenance 渲染为 orange tag（区别于离线红）', () => {
    const { container } = render(<StatusTag value="maintenance" />)
    expect(container.querySelector('.ant-tag-orange')).not.toBeNull()
  })
})
