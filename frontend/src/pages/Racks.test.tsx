// Racks.test.tsx — 机房机柜页
// W1：此前三处 `res?.data?.data ?? MOCK_*` 兜底（站点 / 机柜 / 设备），
// 接口失败或形状变化时静默显示虚构的机房、机柜、设备 —— 运维会在真实机房里
// 对着假机柜排查。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'

// vi.hoisted：mock 工厂在 import 期被调用，共享状态必须在提升块里创建
const h = vi.hoisted(() => ({
  overrides: {} as Record<string, Record<string, unknown>>,
  refetch: {} as Record<string, ReturnType<typeof vi.fn>>,
}))

const SITES = [
  { id: '1', name: '机房A' },
  { id: '2', name: '机房B' },
]
const RACKS = [
  { id: 'r1', name: 'Rack-01', site_id: '1', total_units: 42, used_units: 20 },
]
const DEVICES = [
  { id: 'd1', name: 'server-01', asset_type: 'server', rack_position: 42, health_status: 'green', alert_count: 0 },
]

const DATA: Record<string, unknown> = { sites: SITES, racks: RACKS, devices: DEVICES }

vi.mock('../hooks/useApiQuery', () => ({
  useApiQuery: (key: unknown) => {
    const k = Array.isArray(key) ? key : []
    // ['racks'] = 站点列表；['racks','list',siteId] = 机柜；['racks','devices',id] = 设备
    const kind = k.length === 1 ? 'sites' : k[1] === 'list' ? 'racks' : 'devices'
    if (!h.refetch[kind]) h.refetch[kind] = vi.fn()
    return {
      data: DATA[kind],
      isLoading: false,
      isError: false,
      error: undefined,
      refetch: h.refetch[kind],
      ...h.overrides[kind],
    }
  },
  queryKeys: { racks: { all: ['racks'], devices: (id: string) => ['racks', 'devices', id] } },
}))

import Racks from './Racks'

/** 选中机房（触发机柜查询）。 */
async function selectSite(name = '机房A') {
  fireEvent.mouseDown(screen.getByRole('combobox'))
  fireEvent.click(await screen.findByTitle(name))
}

beforeEach(() => {
  h.overrides = {}
  for (const k of ['sites', 'racks', 'devices']) h.refetch[k]?.mockClear()
})

describe('Racks page', () => {
  it('渲染机柜页面，未选机房时给出引导空态', () => {
    render(<Racks />)
    expect(screen.getByText('机房机柜')).toBeInTheDocument()
    expect(screen.getByText('请先选择机房')).toBeInTheDocument()
  })

  it('选择机房后渲染机柜网格', async () => {
    render(<Racks />)
    await selectSite()
    expect(screen.getByText('Rack-01')).toBeInTheDocument()
    expect(screen.queryByText('请先选择机房')).toBeNull()
  })

  it('W1：站点接口失败显示错误态 + 重试，不回落 MOCK_SITES', () => {
    h.overrides.sites = { data: undefined, isError: true, error: { response: { status: 500 } } }
    render(<Racks />)
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    // 虚构机房名必须消失（否则用户会去选一个不存在的机房）
    expect(screen.queryByText('机房C')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch.sites).toHaveBeenCalled()
  })

  it('W1：机柜接口失败显示错误态 + 重试，不回落 mockRacks', async () => {
    h.overrides.racks = { data: undefined, isError: true, error: { response: { status: 500 } } }
    render(<Racks />)
    await selectSite()
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    expect(screen.queryByText('Rack-02')).toBeNull()
    expect(screen.queryByText('Rack-03')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(h.refetch.racks).toHaveBeenCalled()
  })

  it('W1：机柜列表为空时显示空态，不回落 mockRacks', async () => {
    h.overrides.racks = { data: [] }
    render(<Racks />)
    await selectSite()
    expect(screen.getByText('暂无机柜')).toBeInTheDocument()
    expect(screen.queryByText('Rack-01')).toBeNull()
  })

  it('W1：设备接口失败显示错误态，不回落 mockDevices', async () => {
    h.overrides.devices = { data: undefined, isError: true, error: { response: { status: 500 } } }
    render(<Racks />)
    await selectSite()
    fireEvent.click(screen.getByText('Rack-01'))
    expect(await screen.findByText('Rack-01 设备列表')).toBeInTheDocument()
    expect(screen.getByText('数据加载失败')).toBeInTheDocument()
    // mockDevices 里的虚构设备必须消失
    expect(screen.queryByText('switch-01')).toBeNull()
    expect(screen.queryByText('server-02')).toBeNull()
  })

  it('打开机柜弹窗显示设备列表', async () => {
    render(<Racks />)
    await selectSite()
    fireEvent.click(screen.getByText('Rack-01'))
    expect(await screen.findByText('Rack-01 设备列表')).toBeInTheDocument()
    expect(screen.getByText('server-01')).toBeInTheDocument()
  })

  it('设备列表为空时弹窗内显示空态', async () => {
    h.overrides.devices = { data: [] }
    render(<Racks />)
    await selectSite()
    fireEvent.click(screen.getByText('Rack-01'))
    expect(await screen.findByText('该机柜暂无设备')).toBeInTheDocument()
  })
})
