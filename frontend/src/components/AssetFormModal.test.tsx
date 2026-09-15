// M62：资产表单的 IP 字段从「形状检查」换成「地址检查」后的**界面级**证据。
//
// 这一层只 `render` 组件本身（不 mock 任何服务、不经过 Assets 页）—— 组件是纯的：
// 校验通过才调 `onSubmit`，于是「有没有走到提交」就是校验是否放行的观测点。
// 三个断言面缺一不可：坏值必须**不发提交**、好值必须**走到提交**（否则「报错」用例
// 可能只是因为提交路径根本没接上），必填与格式是两条不同文案。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach, type Mock } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { AssetFormModal } from './AssetFormModal'
import type { AssetFormValues } from './AssetFormModal'

const IP_ERROR = /IP 地址格式不正确/

async function fillForm(ip: string) {
  fireEvent.change(screen.getByLabelText('资产名称'), { target: { value: 'web-server-01' } })
  // antd Select：mouseDown 打开下拉，再点选项（选项带 title=label）
  fireEvent.mouseDown(screen.getByLabelText('资产类型'))
  fireEvent.click(await screen.findByTitle('服务器'))
  fireEvent.change(screen.getByLabelText('IP 地址'), { target: { value: ip } })
  // antd 对「两个汉字」的按钮会在字间插一个空格（`创 建`），故用正则匹配
  fireEvent.click(screen.getByRole('button', { name: /创\s*建/ }))
}

describe('M62 AssetFormModal IP 校验', () => {
  let onSubmit: Mock<[AssetFormValues], void>

  beforeEach(() => {
    onSubmit = vi.fn<[AssetFormValues], void>()
    render(<AssetFormModal open onCancel={() => {}} onSubmit={onSubmit} />)
  })

  it('IPv4 192.168.1.1 → 通过校验并带着该值提交', async () => {
    await fillForm('192.168.1.1')
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ ip_address: '192.168.1.1' })),
    )
    expect(screen.queryByText(IP_ERROR)).not.toBeInTheDocument()
  })

  it('IPv4 256.0.0.1 → 段越界，显示格式错误且不提交', async () => {
    await fillForm('256.0.0.1')
    expect(await screen.findByText(IP_ERROR)).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('IPv6 ::1 → 与 v4 同样放行', async () => {
    await fillForm('::1')
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ ip_address: '::1' })),
    )
    expect(screen.queryByText(IP_ERROR)).not.toBeInTheDocument()
  })

  it('留空 → 显示必填文案（不是格式文案）且不提交', async () => {
    await fillForm('')
    expect(await screen.findByText('请输入 IP 地址')).toBeInTheDocument()
    expect(screen.queryByText(IP_ERROR)).not.toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })
})
