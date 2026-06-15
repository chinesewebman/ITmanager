// Assets page smoke test
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import Assets from './Assets'

const mockAssets = [
  { id: '1', name: 'web-server-01', asset_type: 'server', ip_address: '192.168.1.10', status: 'active', site_name: '机房A', rack_name: 'Rack-01' },
  { id: '2', name: 'db-server-01', asset_type: 'server', ip_address: '192.168.1.11', status: 'active', site_name: '机房A', rack_name: 'Rack-02' },
]

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: () => ({ data: mockAssets, isLoading: false, refetch: vi.fn() }),
  useApiMutation: () => ({ mutate: vi.fn(), mutateAsync: vi.fn() }),
  queryKeys: { assets: { list: () => ['assets', 'list'] } },
}))

describe('Assets page', () => {
  it('渲染资产表格 + 关键列（mock 数据）', () => {
    render(<Assets />)
    expect(screen.getByText('资产管理')).toBeInTheDocument()
    // AssetTable 渲染 mock 资产名
    expect(screen.getByText('web-server-01')).toBeInTheDocument()
    expect(screen.getByText('db-server-01')).toBeInTheDocument()
    // IP 列
    expect(screen.getByText('192.168.1.10')).toBeInTheDocument()
  })

  it('不 crash 渲染', () => {
    expect(() => render(<Assets />)).not.toThrow()
  })
})
