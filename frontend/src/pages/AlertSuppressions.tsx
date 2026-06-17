// 告警抑制规则管理页（P0-2）
// 提供 CRUD + Preview 模拟评估
import { useState } from 'react'
import {
  Button, Card, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Switch, Table, Tag, Typography, message,
} from 'antd'
import { PlusOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { useApiQuery } from '../hooks/useApiQuery'
import { EmptyState } from '../components/EmptyState'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

const { Text } = Typography

interface AlertSuppression {
  id?: string
  name: string
  host_pattern: string
  severity_max: number
  time_window_seconds: number
  ttl_seconds: number
  enabled: boolean
  description?: string
}

interface SuppressionMatchResult {
  suppressed: boolean
  matched_rule?: string
  reason?: string
  last_fired_at?: string
  window_expires_at?: string
}

const MOCK_RULES: AlertSuppression[] = [
  { id: '1', name: '抑制 db-* 警告', host_pattern: 'db-*', severity_max: 3, time_window_seconds: 300, ttl_seconds: 0, enabled: true, description: '5 分钟内同 host 仅保留 1 条 warning' },
  { id: '2', name: '抑制 web-* 信息', host_pattern: 'web-*', severity_max: 2, time_window_seconds: 600, ttl_seconds: 3600, enabled: true, description: '10 分钟窗口，1 小时后自动失效' },
]

async function apiGet<T>(path: string): Promise<T> {
  const token = localStorage.getItem('token') ?? ''
  const res = await fetch(`/api${path}`, { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) throw new Error('fetch failed')
  const json: any = await res.json()
  return json?.data
}

async function apiSend<T>(method: string, path: string, body?: any): Promise<T> {
  const token = localStorage.getItem('token') ?? ''
  const res = await fetch(`/api${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const err: any = await res.json().catch(() => ({}))
    throw new Error(err?.message ?? `HTTP ${res.status}`)
  }
  if (res.status === 204) return undefined as T
  const json: any = await res.json()
  return json?.data
}

export function AlertSuppressions() {
  const [form] = Form.useForm<AlertSuppression>()
  const [editing, setEditing] = useState<AlertSuppression | null>(null)
  const [modalOpen, setModalOpen] = useState(false)
  const [previewOpen, setPreviewOpen] = useState(false)
  const [previewResult, setPreviewResult] = useState<SuppressionMatchResult | null>(null)
  const [previewHost, setPreviewHost] = useState({ severity: 3, host_id: '', host_name: '' })

  useDocumentTitle('告警抑制')
  // mock 优先：网络断也展示
  const { data: rules = MOCK_RULES, refetch } = useApiQuery<AlertSuppression[]>(
    ['alert-suppressions'] as const,
    async () => {
      try { return await apiGet<AlertSuppression[]>('/alert-suppressions') } catch { return MOCK_RULES }
    },
  )

  function openCreate() {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ severity_max: 3, time_window_seconds: 300, ttl_seconds: 0, enabled: true })
    setModalOpen(true)
  }

  function openEdit(rule: AlertSuppression) {
    setEditing(rule)
    form.setFieldsValue(rule)
    setModalOpen(true)
  }

  async function onSubmit() {
    try {
      const values = await form.validateFields()
      if (editing?.id) {
        await apiSend('PUT', `/alert-suppressions/${editing.id}`, values)
        message.success('已更新')
      } else {
        await apiSend('POST', '/alert-suppressions', values)
        message.success('已创建')
      }
      setModalOpen(false)
      refetch()
    } catch (e: any) {
      if (e?.errorFields) return // form 校验失败
      message.error(e?.message ?? '操作失败')
    }
  }

  async function onDelete(id: string) {
    try {
      await apiSend('DELETE', `/alert-suppressions/${id}`)
      message.success('已删除')
      refetch()
    } catch (e: any) {
      message.error(e?.message ?? '删除失败')
    }
  }

  async function onPreview() {
    try {
      const res = await apiSend<SuppressionMatchResult>('POST', '/alert-suppressions/preview', previewHost)
      setPreviewResult(res)
    } catch (e: any) {
      message.error(e?.message ?? '评估失败')
    }
  }

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
          新建抑制规则
        </Button>
        <Button icon={<ThunderboltOutlined />} onClick={() => setPreviewOpen(true)}>
          模拟评估
        </Button>
      </Space>

      <Card title="告警抑制规则" size="small">
        <Table
          dataSource={rules}
          rowKey={(r) => r.id ?? r.name}
          pagination={false}
          locale={{
            emptyText: (
              <EmptyState
                title="暂无抑制规则"
                description="添加规则可在告警触发时自动抑制匹配的告警"
                compact
              />
            ),
          }}
          columns={[
            { title: '名称', dataIndex: 'name', key: 'name' },
            { title: '主机模式', dataIndex: 'host_pattern', key: 'host_pattern', render: (v: string) => <Tag color="purple">{v}</Tag> },
            { title: '严重级别 ≤', dataIndex: 'severity_max', key: 'severity_max', render: (v: number) => <Tag color={v >= 4 ? 'red' : v >= 3 ? 'orange' : 'blue'}>{v}</Tag> },
            { title: '时间窗口', dataIndex: 'time_window_seconds', key: 'time_window_seconds', render: (v: number) => `${v} 秒` },
            { title: 'TTL', dataIndex: 'ttl_seconds', key: 'ttl_seconds', render: (v: number) => v > 0 ? `${v} 秒` : '不过期' },
            { title: '启用', dataIndex: 'enabled', key: 'enabled', render: (v: boolean) => v ? <Tag color="green">ON</Tag> : <Tag>OFF</Tag> },
            { title: '说明', dataIndex: 'description', key: 'description' },
            {
              title: '操作', key: 'actions',
              render: (_: any, record: AlertSuppression) => (
                <Space>
                  <Button size="small" onClick={() => openEdit(record)}>编辑</Button>
                  <Popconfirm title="确定删除？" onConfirm={() => record.id && onDelete(record.id)}>
                    <Button size="small" danger>删除</Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>

      {/* 新建/编辑 Modal */}
      <Modal
        title={editing ? '编辑抑制规则' : '新建抑制规则'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={onSubmit}
        okText="保存"
        cancelText="取消"
      >
        <Form form={form} layout="vertical">
          <Form.Item label="名称" name="name" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：抑制 db-* 警告" />
          </Form.Item>
          <Form.Item label="主机模式 (glob)" name="host_pattern" rules={[{ required: true, message: '请输入主机模式' }]}>
            <Input placeholder="如：db-*、web-*-prod、switch-core-01" />
          </Form.Item>
          <Form.Item label="严重级别 ≤ (0-5)" name="severity_max" rules={[{ required: true, message: '请输入级别' }]}>
            <InputNumber min={0} max={5} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item label="时间窗口 (秒)" name="time_window_seconds" rules={[{ required: true }]}>
            <InputNumber min={1} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item label="TTL (秒, 0=不过期)" name="ttl_seconds">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item label="启用" name="enabled" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item label="说明" name="description">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>

      {/* Preview Modal */}
      <Modal
        title="模拟评估"
        open={previewOpen}
        onCancel={() => setPreviewOpen(false)}
        onOk={onPreview}
        okText="评估"
        cancelText="关闭"
      >
        <Form layout="vertical">
          <Form.Item label="严重级别 (1-5)">
            <Select
              value={previewHost.severity}
              onChange={(v) => setPreviewHost({ ...previewHost, severity: v })}
              options={[1, 2, 3, 4, 5].map((n) => ({ label: String(n), value: n }))}
            />
          </Form.Item>
          <Form.Item label="Host UUID">
            <Input
              value={previewHost.host_id}
              onChange={(e) => setPreviewHost({ ...previewHost, host_id: e.target.value })}
              placeholder="00000000-0000-0000-0000-000000000000"
            />
          </Form.Item>
          <Form.Item label="Host 名称">
            <Input
              value={previewHost.host_name}
              onChange={(e) => setPreviewHost({ ...previewHost, host_name: e.target.value })}
              placeholder="db-01"
            />
          </Form.Item>
        </Form>
        {previewResult && (
          <Card size="small" style={{ marginTop: 12 }}>
            {previewResult.suppressed ? (
              <Text type="danger">🚫 被抑制：{previewResult.reason}</Text>
            ) : (
              <Text type="success">✅ 放行：{previewResult.reason}</Text>
            )}
          </Card>
        )}
      </Modal>
    </div>
  )
}

export default AlertSuppressions
