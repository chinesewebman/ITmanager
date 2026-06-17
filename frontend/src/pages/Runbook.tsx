// 故障 Runbook 管理页（P2-1）
// 按 asset_type 分类的标准操作手册（SOP），可关联告警
import { useState } from 'react'
import {
  Button, Card, Drawer, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Switch, Table, Tag, Typography, message,
} from 'antd'
import { PlusOutlined, BookOutlined } from '@ant-design/icons'
import { useApiQuery } from '../hooks/useApiQuery'
import { EmptyState } from '../components/EmptyState'
import { SeverityTag } from '../components/SeverityTag'
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

const MOCK_RUNBOOKS: Runbook[] = [
  { id: 'r1', title: 'MySQL 主从延迟告警处理', asset_type: 'server', summary: '主从延迟 > 30s 时的检查流程', severity: 4, enabled: true, tags: 'db,mysql', content_md: '# 处理步骤\n\n1. 检查主库写入压力\n2. 查看从库 IO/SQL 线程\n3. 必要时切换主从' },
  { id: 'r2', title: '核心交换机端口 down', asset_type: 'switch', summary: '核心交换机端口 down 的紧急处理', severity: 5, enabled: true, tags: 'network,switch', content_md: '# 处理步骤\n\n1. 确认物理连接\n2. 检查光模块功率\n3. 切换备用链路' },
  { id: 'r3', title: '磁盘空间不足', asset_type: 'server', summary: '磁盘使用率 > 90% 清理', severity: 3, enabled: true, tags: 'disk', content_md: '# 处理步骤\n\n1. du -sh 找大目录\n2. 清理旧日志\n3. 扩容评估' },
]

const MOCK_RECOMMEND: Runbook[] = [
  { id: 'r1', title: 'MySQL 主从延迟告警处理', asset_type: 'server', summary: '主从延迟 > 30s', severity: 4, enabled: true, tags: 'db,mysql' },
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
  if (!res.ok) throw new Error('fetch failed')
  const json: any = await res.json()
  return json?.data
}

function RunbookList() {
  const [filter, setFilter] = useState<{ asset_type?: string; severity?: number }>({})

  useDocumentTitle('故障 Runbook')
  const [editing, setEditing] = useState<Runbook | null>(null)
  const [creating, setCreating] = useState(false)
  const [viewing, setViewing] = useState<Runbook | null>(null)
  const [form] = Form.useForm<Runbook>()

  const { data, isLoading, refetch } = useApiQuery(['runbooks', filter], () =>
    apiGet<{ items: Runbook[]; total: number }>(`/runbooks?${new URLSearchParams({
      ...(filter.asset_type ? { asset_type: filter.asset_type } : {}),
      ...(filter.severity ? { severity: String(filter.severity) } : {}),
    } as any).toString()}`).catch(() => ({ items: MOCK_RUNBOOKS.filter(r => !filter.asset_type || r.asset_type === filter.asset_type), total: 3 })),
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
    try {
      const values = await form.validateFields()
      if (editing) {
        await apiSend('PUT', `/runbooks/${editing.id}`, values).catch(() => null)
        message.success('已更新（mock）')
      } else {
        await apiSend('POST', '/runbooks', values).catch(() => null)
        message.success('已创建（mock）')
      }
      setEditing(null)
      setCreating(false)
      refetch()
    } catch (e: any) {
      message.error(e?.message ?? '提交失败')
    }
  }

  async function onDelete(id: string) {
    await apiSend('DELETE', `/runbooks/${id}`).catch(() => null)
    message.success('已删除（mock）')
    refetch()
  }

  const items = data?.items ?? []
  const total = data?.total ?? 0

  return (
    <div style={{ padding: 24 }}>
      <Space style={{ marginBottom: 16 }}>
        <BookOutlined style={{ fontSize: 20 }} />
        <Text strong style={{ fontSize: 18 }}>故障 Runbook</Text>
        <Tag>{total} 条</Tag>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新建</Button>
      </Space>

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
          { title: '标题', dataIndex: 'title', key: 'title' },
          { title: '类型', dataIndex: 'asset_type', key: 'asset_type', width: 100,
            render: v => <Tag color="blue">{v}</Tag> },
          { title: '严重度', dataIndex: 'severity', key: 'severity', width: 80,
            render: v => v > 0 ? <SeverityTag severity={v} /> : <Tag>全部</Tag> },
          { title: '标签', dataIndex: 'tags', key: 'tags',
            render: v => v ? v.split(',').map((t: string) => <Tag key={t}>{t}</Tag>) : null },
          { title: '启用', dataIndex: 'enabled', key: 'enabled', width: 80,
            render: v => v ? <Tag color="green">是</Tag> : <Tag>否</Tag> },
          { title: '操作', key: 'actions', width: 200,
            render: (_, rb: Runbook) => (
              <Space>
                <Button size="small" onClick={() => setViewing(rb)}>查看</Button>
                <Button size="small" onClick={() => openEdit(rb)}>编辑</Button>
                <Popconfirm title="确定删除?" onConfirm={() => rb.id && onDelete(rb.id)}>
                  <Button size="small" danger>删除</Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />

      <Modal
        title={editing ? '编辑 Runbook' : '新建 Runbook'}
        open={creating || !!editing}
        onCancel={() => { setCreating(false); setEditing(null) }}
        onOk={onSubmit}
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
            {viewing.severity > 0 && <Tag color={viewing.severity >= 4 ? 'red' : 'orange'}>P{viewing.severity}</Tag>}
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
  const { data } = useApiQuery(['runbooks', 'recommend', assetType, severity], () =>
    apiGet<Runbook[]>(`/runbooks/recommend?asset_type=${encodeURIComponent(assetType)}&severity=${severity}`)
      .catch(() => MOCK_RECOMMEND),
  )
  const items = data ?? []
  if (items.length === 0) return <Text type="secondary">无推荐 Runbook</Text>
  return (
    <Space direction="vertical" style={{ width: '100%' }}>
      {items.map(rb => (
        <Card key={rb.id} size="small" title={rb.title}>
          <Paragraph style={{ marginBottom: 4 }}>{rb.summary}</Paragraph>
          <Tag color="blue">{rb.asset_type}</Tag>
          {rb.severity > 0 && <Tag color={rb.severity >= 4 ? 'red' : 'orange'}>P{rb.severity}</Tag>}
        </Card>
      ))}
    </Space>
  )
}

export default RunbookList
export { RunbookRecommend }
export type { Runbook }
