// H9：移动端资产卡此前 `status === 'active' ? '在线' : '离线'` 把 maintenance
// 误标红「离线」。改走 StatusTag + statusLabel 后应显示「维护」橙色 tag。
// 独立文件：mock Grid.useBreakpoint 强制 isMobile=true，避免影响 Assets.test.tsx 桌面端用例。
import '@testing-library/jest-dom'
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import Assets from './Assets'

vi.mock('antd', async () => {
  const actual = await vi.importActual<typeof import('antd')>('antd')
  return {
    ...actual,
    Grid: { ...actual.Grid, useBreakpoint: () => ({ xs: true, sm: false }) },
  }
})

const h = vi.hoisted(() => ({
  assets: [
    {
      id: 'm1',
      name: '维护中服务器',
      asset_type: 'server',
      ip_address: '10.0.0.1',
      status: 'maintenance',
      site_name: '机房A',
      rack_name: 'Rack-01',
    },
  ],
  refetch: vi.fn(),
}))

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: () => ({
    // M3/P5（rev47）后 Assets 的 data 结构改为 {items,total}（服务端分页），mock 同步
    data: { items: h.assets, total: h.assets.length },
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: h.refetch,
  }),
  useApiMutation: () => ({ mutate: vi.fn(), mutateAsync: vi.fn() }),
  queryKeys: { assets: { list: () => ['assets', 'list'] } },
}))

describe('H9 移动端资产卡状态', () => {
  it('maintenance 资产显示「维护」而非「离线」', async () => {
    render(<Assets />)
    expect(await screen.findByText('维护')).toBeInTheDocument()
    expect(screen.queryByText('离线')).not.toBeInTheDocument()
  })
})
