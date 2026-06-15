import { Modal, Form, Input, Select } from 'antd'
import { useEffect } from 'react'
import type { Asset } from './AssetTable'

const ASSET_TYPES = [
  { value: 'server', label: '服务器' },
  { value: 'switch', label: '交换机' },
  { value: 'router', label: '路由器' },
  { value: 'firewall', label: '防火墙' },
  { value: 'storage', label: '存储' },
]

const STATUS_OPTIONS = [
  { value: 'active', label: '在线' },
  { value: 'inactive', label: '离线' },
]

export interface AssetFormValues {
  name: string
  asset_type: string
  ip_address: string
  status: string
}

export interface AssetFormModalProps {
  open: boolean
  editing?: Asset | null
  submitting?: boolean
  onCancel: () => void
  onSubmit: (values: AssetFormValues) => Promise<void> | void
}

/**
 * AssetFormModal - 资产创建/编辑表单弹窗。
 * 父组件持有 submitting 状态以禁用提交按钮。
 */
export function AssetFormModal({ open, editing, submitting, onCancel, onSubmit }: AssetFormModalProps) {
  const [form] = Form.useForm<AssetFormValues>()
  const isEdit = !!editing

  useEffect(() => {
    if (open) {
      if (editing) {
        form.setFieldsValue({
          name: editing.name,
          asset_type: editing.asset_type,
          ip_address: editing.ip_address,
          status: editing.status,
        })
      } else {
        form.resetFields()
        form.setFieldsValue({ status: 'active' })
      }
    }
  }, [open, editing, form])

  return (
    <Modal
      title={isEdit ? '编辑资产' : '新增资产'}
      open={open}
      onCancel={onCancel}
      okText={isEdit ? '保存' : '创建'}
      cancelText="取消"
      confirmLoading={submitting}
      onOk={async () => {
        try {
          const values = await form.validateFields()
          await onSubmit(values)
        } catch {
          // antd 已显示字段错误
        }
      }}
      destroyOnClose
    >
      <Form form={form} layout="vertical" preserve={false}>
        <Form.Item
          name="name"
          label="资产名称"
          rules={[{ required: true, message: '请输入资产名称' }]}
        >
          <Input placeholder="例如：web-server-01" />
        </Form.Item>
        <Form.Item
          name="asset_type"
          label="资产类型"
          rules={[{ required: true, message: '请选择资产类型' }]}
        >
          <Select options={ASSET_TYPES} placeholder="选择类型" />
        </Form.Item>
        <Form.Item
          name="ip_address"
          label="IP 地址"
          rules={[
            { required: true, message: '请输入 IP 地址' },
            { pattern: /^(\d{1,3}\.){3}\d{1,3}$/, message: 'IP 格式不正确' },
          ]}
        >
          <Input placeholder="192.168.1.10" />
        </Form.Item>
        <Form.Item name="status" label="状态" rules={[{ required: true }]}>
          <Select options={STATUS_OPTIONS} />
        </Form.Item>
      </Form>
    </Modal>
  )
}

export default AssetFormModal
