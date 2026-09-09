// 故障 Runbook 管理页（P2-1）
// 按 asset_type 分类的标准操作手册（SOP），可关联告警
//
// W1：`:52` `.catch(() => ({ items: MOCK_RUNBOOKS..., total: 3 }))` 与
// `:240` `.catch(() => MOCK_RECOMMEND)` 两处兜底已删除。原写法让 isError 恒 false ——
// 接口挂了照常列出 3 篇虚构的 SOP（含「MySQL 主从延迟告警处理」），
// 故障现场运维会照着不存在的手册操作。
import { useState } from 'react'
import {
  Button, Card, Drawer, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Switch, Table, Tag, Typography, message,
} from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useApiQuery } from '../hooks/useApiQuery'
import { apiGet, apiSend } from '../services/api'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { SeverityTag } from '../components/SeverityTag'
import { PageHeader } from '../components/PageHeader'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

const { Text, Paragraph } = Typography

interface Runbook {
  id?: string
  title: string
  asset_type: string
  summary?: string
  content_md?: string
  steps?: string
  tags?: string
  severity: number
  enabled: boolean
  updated_at?: string
}

function RunbookList() {
  const [filter, setFilter] = useState<{ asset_type?: string; severity?: number }>({})

  useDocumentTitle('故障 Runbook')
  const [editing, setEditing] = useState<Runbook | null>(null)
  const [creating, setCreating] = useState(false)
  const [viewing, setViewing] = useState<Runbook | null>(null)
  // M4：提交按钮 loading，防连点重复创建（范本 AssetFormModal confirmLoading）
  const [submitting, setSubmitting] = useState(false)
  const [form] = Form.useForm<Runbook>()

  const { data, isLoading, isError, error, refetch } = useApiQuery(
    ['runbooks', filter],
    () =>
      apiGet<{ items: Runbook[]; total: number }>(`/runbooks?${new URLSearchParams({
        ...(filter.asset_type ? { asset_type: filter.asset_type } : {}),
        ...(filter.severity ? { severity: String(filter.severity) } : {}),
      } as any).toString()}`),
  )

  function openCreate() {
    setCreating(true)
    form.resetFields()
    form.setFieldsValue({ asset_type: 'server', severity: 3, enabled: true })
  }

  function openEdit(rb: Runbook) {
    setEditing(rb)
    form.setFieldsValue(rb)
  }

  async function onSubmit() {
    setSubmitting(true)
    try {
      const values = await form.validateFields()
      if (editing) {
        await apiSend('PUT', `/runbooks/${editing.id}`, values)
        message.success('已更新')
      } else {
        await apiSend('POST', '/runbooks', values)
        message.success('已创建')
      }
      setEditing(null)
      setCreating(false)
      refetch()
    } catch (e: any) {
      if (e?.errorFields) return // 表单校验失败，antd 已在字段上提示
      if (e?.isAxiosError) return // 4xx/5xx/网络错误已由响应拦截器提示，不重复弹
      message.error(e?.message ?? '提交失败')
    } finally {
      setSubmitting(false)
    }
  }

  async function onDelete(id: string) {
    // 失败不谎报成功：403/500 已由响应拦截器提示，这里不再重复弹
    try {
      await apiSend('DELETE', `/runbooks/${id}`)
      message.success('已删除')
      refetch()
    } catch (e: any) {
      if (!e?.isAxiosError) message.error(e?.message ?? '删除失败')
    }
  }

  // 接口形状归一：items 非数组（形状变了 / 后端返回 null）一律当空，不回落 mock
  const items = Array.isArray(data?.items) ? data.items : []
  const total = data?.total ?? 0

  return (
    <div style={{ padding: 24 }}>
      {/* M1：标题体系统一——原手写 Space 标题（Text strong），改为 PageHeader h4；
          丢弃装饰性 BookOutlined 图标，计数 Tag 与新建按钮移入 extra */}
      <PageHeader
        title="故障 Runbook"
        extra={
          <Space>
            <Tag>{total} 条</Tag>
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新建</Button>
          </Space>
        }
      />

      <Card size="small" style={{ marginBottom: 16 }}>
        <Space>
          <span>资产类型:</span>
          <Select
            allowClear
            placeholder="全部"
            style={{ width: 140 }}
            value={filter.asset_type}
            onChange={v => setFilter(f => ({ ...f, asset_type: v }))}
            options={[
              { value: 'server', label: '服务器' },
              { value: 'switch', label: '交换机' },
              { value: 'router', label: '路由器' },
              { value: 'firewall', label: '防火墙' },
              { value: 'storage', label: '存储' },
            ]}
          />
          <span>严重度:</span>
          <Select
            allowClear
            placeholder="全部"
            style={{ width: 120 }}
            value={filter.severity}
            onChange={v => setFilter(f => ({ ...f, severity: v }))}
            options={[1, 2, 3, 4, 5].map(s => ({ value: s, label: `P${s}` }))}
          />
        </Space>
      </Card>

      {isError ? (
        <ErrorState error={error} onRetry={refetch} compact />
      ) : (
      <Table
        loading={isLoading}
        rowKey="id"
        dataSource={items}
        pagination={{ pageSize: 20 }}
        locale={{
          emptyText: (
            <EmptyState
              title="暂无 Runbook"
              description={'点击右上角"新建"录入标准操作手册'}
              compact
            />
          ),
        }}
        columns={[
          // M2：加前端本地排序。标题/类型字符串 localeCompare；严重度按 severity 数值；
          // 启用按布尔权重；标签是多值逗号串，排序无意义（同 AlertTable message）不加。
          { title: '标题', dataIndex: 'title', key: 'title', sorter: (a, b) => a.title.localeCompare(b.title) },
          { title: '类型', dataIndex: 'asset_type', key: 'asset_type', width: 100,
            sorter: (a, b) => a.asset_type.localeCompare(b.asset_type),
            render: v => <Tag color="blue">{v}</Tag> },
          { title: '严重度', dataIndex: 'severity', key: 'severity', width: 80,
            sorter: (a, b) => a.severity - b.severity,
            render: v => v > 0 ? <SeverityTag severity={v} /> : <Tag>全部</Tag> },
          { title: '标签', dataIndex: 'tags', key: 'tags',
            render: v => v ? v.split(',').map((t: string) => <Tag key={t}>{t}</Tag>) : null },
          { title: '启用', dataIndex: 'enabled', key: 'enabled', width: 80,
            sorter: (a, b) => Number(a.enabled) - Number(b.enabled),
            render: v => v ? <Tag color="green">是</Tag> : <Tag>否</Tag> },
          { title: '操作', key: 'actions', width: 200,
            render: (_, rb: Runbook) => (
              <Space>
                <Button size="small" onClick={() => setViewing(rb)}>查看</Button>
                <Button size="small" onClick={() => openEdit(rb)}>编辑</Button>
                <Popconfirm
                  title={`确认删除 Runbook「${rb.title}」？`}
                  okText="删除"
                  cancelText="取消"
                  okButtonProps={{ danger: true }}
                  onConfirm={() => rb.id && onDelete(rb.id)}
                >
                  <Button size="small" danger>删除</Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />
      )}

      <Modal
        title={editing ? '编辑 Runbook' : '新建 Runbook'}
        open={creating || !!editing}
        onCancel={() => { setCreating(false); setEditing(null) }}
        onOk={onSubmit}
        confirmLoading={submitting}
        width={720}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入标题' }]}>
            <Input placeholder="如: MySQL 主从延迟告警处理" />
          </Form.Item>
          <Form.Item name="asset_type" label="资产类型" rules={[{ required: true, message: '请选择资产类型' }]}>
            <Select options={[
              { value: 'server', label: '服务器' },
              { value: 'switch', label: '交换机' },
              { value: 'router', label: '路由器' },
              { value: 'firewall', label: '防火墙' },
              { value: 'storage', label: '存储' },
            ]} />
          </Form.Item>
          <Form.Item name="summary" label="摘要">
            <Input placeholder="简短描述（≤ 500 字）" />
          </Form.Item>
          <Form.Item name="content_md" label="处理步骤（Markdown）">
            <Input.TextArea rows={6} placeholder="# 步骤 1\n...\n# 步骤 2\n..." />
          </Form.Item>
          <Form.Item name="tags" label="标签（逗号分隔）">
            <Input placeholder="db,perf,disk" />
          </Form.Item>
          <Form.Item name="severity" label="适用严重度 (0=不限制)">
            <InputNumber min={0} max={5} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        title={viewing?.title ?? ''}
        open={!!viewing}
        onClose={() => setViewing(null)}
        width={560}
      >
        {viewing && (
          <>
            <Tag color="blue">{viewing.asset_type}</Tag>
            {viewing.severity > 0 && <SeverityTag severity={viewing.severity} />}
            {viewing.tags?.split(',').map(t => <Tag key={t}>{t}</Tag>)}
            <Paragraph style={{ marginTop: 16 }}>{viewing.summary}</Paragraph>
            <pre style={{ whiteSpace: 'pre-wrap', background: '#f5f5f5', padding: 12, borderRadius: 4 }}>
              {viewing.content_md ?? '(无详细内容)'}
            </pre>
          </>
        )}
      </Drawer>
    </div>
  )
}

// 推荐面板：用于告警详情页侧栏
function RunbookRecommend({ assetType, severity }: { assetType: string; severity: number }) {
  const { data, isError, error, refetch } = useApiQuery<Runbook[]>(
    ['runbooks', 'recommend', assetType, severity],
    () => apiGet<Runbook[]>(`/runbooks/recommend?asset_type=${encodeURIComponent(assetType)}&severity=${severity}`),
  )
  // 接口形状归一：非数组一律当空，不回落 mock
  const items = Array.isArray(data) ? data : []
  if (isError) return <ErrorState error={error} onRetry={refetch} compact />
  if (items.length === 0) return <Text type="secondary">无推荐 Runbook</Text>
  return (
    <Space direction="vertical" style={{ width: '100%' }}>
      {items.map(rb => (
        <Card key={rb.id} size="small" title={rb.title}>
          <Paragraph style={{ marginBottom: 4 }}>{rb.summary}</Paragraph>
          <Tag color="blue">{rb.asset_type}</Tag>
          {rb.severity > 0 && <SeverityTag severity={rb.severity} />}
        </Card>
      ))}
    </Space>
  )
}

export default RunbookList
export { RunbookRecommend }
export type { Runbook }
