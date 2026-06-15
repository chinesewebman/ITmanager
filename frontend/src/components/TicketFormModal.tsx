import { Form, Input, Modal, Select } from 'antd'
import { useEffect } from 'react'

export interface TicketFormValues {
  title: string
  priority: 'critical' | 'high' | 'normal' | 'low'
  description?: string
}

const PRIORITY_OPTIONS = [
  { value: 'critical', label: '紧急' },
  { value: 'high', label: '高' },
  { value: 'normal', label: '普通' },
  { value: 'low', label: '低' },
]

export interface TicketFormModalProps {
  open: boolean
  submitting?: boolean
  onCancel: () => void
  onSubmit: (values: TicketFormValues) => Promise<void> | void
}

export function TicketFormModal({ open, submitting, onCancel, onSubmit }: TicketFormModalProps) {
  const [form] = Form.useForm<TicketFormValues>()

  useEffect(() => {
    if (open) {
      form.resetFields()
      form.setFieldsValue({ priority: 'normal' })
    }
  }, [open, form])

  return (
    <Modal
      title="创建工单"
      open={open}
      onCancel={onCancel}
      okText="创建"
      cancelText="取消"
      confirmLoading={submitting}
      destroyOnClose
      onOk={async () => {
        try {
          const values = await form.validateFields()
          await onSubmit(values)
        } catch {
          /* validation failed, antd shows errors */
        }
      }}
    >
      <Form form={form} layout="vertical" preserve={false}>
        <Form.Item name="title" label="工单标题" rules={[{ required: true, message: '请输入标题' }]}>
          <Input placeholder="例如：服务器磁盘空间不足" />
        </Form.Item>
        <Form.Item name="priority" label="优先级" rules={[{ required: true }]}>
          <Select options={PRIORITY_OPTIONS} />
        </Form.Item>
        <Form.Item name="description" label="描述">
          <Input.TextArea rows={4} placeholder="详细描述问题..." />
        </Form.Item>
      </Form>
    </Modal>
  )
}

export default TicketFormModal
