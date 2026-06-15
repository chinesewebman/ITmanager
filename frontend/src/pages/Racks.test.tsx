// Racks page smoke test
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import Racks from './Racks'

const mockSites = [
  { id: '1', name: '机房A' },
  { id: '2', name: '机房B' },
]
const mockRacks = [
  { id: '1', name: 'Rack-01', site_id: '1', total_units: 42, used_units: 20 },
]

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: (key: unknown) => ({
    data: Array.isArray(key) && key.join(',').includes('sites') ? mockSites : mockRacks,
    isLoading: false,
    refetch: vi.fn(),
  }),
  useApiMutation: () => ({ mutate: vi.fn(), mutateAsync: vi.fn() }),
  queryKeys: { racks: { list: () => ['racks', 'list'], devices: (id: string) => ['racks', 'devices', id] } },
}))

describe('Racks page', () => {
  it('渲染机柜页面（mock 数据 + 不 crash）', () => {
    render(<Racks />)
    // PageHeader 实际 title 是 "机房机柜"
    expect(screen.getByText('机房机柜')).toBeInTheDocument()
    // antd Select 懒渲染 options — 只断言 PageHeader + 不 panic
  })

  it('不 crash 渲染', () => {
    expect(() => render(<Racks />)).not.toThrow()
  })
})
