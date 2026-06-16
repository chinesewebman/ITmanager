// 值班 + 升级策略管理（P1-2）
import { useState } from 'react'
import {
  Button, Card, Form, Input, Modal, Popconfirm, Space, Switch, Table, Tag, Typography, Tabs, message,
} from 'antd'
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons'
import { useApiQuery } from '../hooks/useApiQuery'

const { Text } = Typography

interface OncallSchedule { id?: string; name: string; description?: string; timezone?: string; enabled: boolean }
interface OncallCurrent { schedule_id: string; schedule_name: string; user_name: string; ends_at: string }
interface EscalationPolicy {
  id?: string; name: string; enabled: boolean
  levels: { level: number; target_type: string; target_id: string; wait_minutes: number; notify_methods: string }[]
}

const MOCK_CURRENT: OncallCurrent[] = [
  { schedule_id: 's1', schedule_name: 'dev-team', user_name: 'alice', ends_at: new Date(Date.now() + 3 * 3600_000).toISOString() },
  { schedule_id: 's2', schedule_name: 'ops-team', user_name: 'bob', ends_at: new Date(Date.now() + 1 * 3600_000).toISOString() },
]
const MOCK_SCHEDULES: OncallSchedule[] = [
  { id: 's1', name: 'dev-team', description: '研发组白班', enabled: true },
  { id: 's2', name: 'ops-team', description: '运维组 7x24', enabled: true },
]
const MOCK_POLICIES: EscalationPolicy[] = [
  { id: 'p1', name: 'critical-alert', enabled: true, levels: [
    { level: 1, target_type: 'user', target_id: 'alice', wait_minutes: 5, notify_methods: 'email' },
    { level: 2, target_type: 'user', target_id: 'bob', wait_minutes: 5, notify_methods: 'sms' },
    { level: 3, target_type: 'channel', target_id: 'all-ops', wait_minutes: 5, notify_methods: 'sms,webhook' },
  ] },
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
    method, headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  if (res.status === 204) return undefined as T
  const json: any = await res.json()
  return json?.data
}

export function Oncall() {
  return (
    <Tabs
      items={[
        { key: 'current', label: '当前值班', children: <CurrentTab /> },
        { key: 'schedules', label: '值班组', children: <SchedulesTab /> },
        { key: 'policies', label: '升级策略', children: <PoliciesTab /> },
      ]}
    />
  )
}

function CurrentTab() {
  const { data } = useApiQuery<OncallCurrent[]>(['oncall', 'current'] as const,
    async () => { try { return await apiGet<OncallCurrent[]>('/oncall/current') } catch { return MOCK_CURRENT } })

  const list = data ?? MOCK_CURRENT
  return (
    <Card title="当前在班" size="small">
      {list.length === 0 ? <Text type="secondary">当前无在班 user</Text> : (
        <Space direction="vertical" style={{ width: '100%' }}>
          {list.map((c) => (
            <Card key={c.schedule_id} size="small" type="inner" title={c.schedule_name}>
              <Text strong style={{ fontSize: 16 }}>{c.user_name}</Text>
              <br />
              <Text type="secondary">值班至 {new Date(c.ends_at).toLocaleString('zh-CN')}</Text>
            </Card>
          ))}
        </Space>
      )}
    </Card>
  )
}

function SchedulesTab() {
  const [form] = Form.useForm<OncallSchedule>()
  const [modalOpen, setModalOpen] = useState(false)
  const { data, refetch } = useApiQuery<OncallSchedule[]>(['oncall', 'schedules'] as const,
    async () => { try { return await apiGet<OncallSchedule[]>('/oncall/schedules') } catch { return MOCK_SCHEDULES } })
  const list = data ?? MOCK_SCHEDULES

  async function onSubmit() {
    try {
      const v = await form.validateFields()
      await apiSend('POST', '/oncall/schedules', v)
      message.success('已创建')
      setModalOpen(false)
      refetch()
    } catch (e: any) { if (!e?.errorFields) message.error(e?.message ?? '失败') }
  }

  async function onDelete(id: string) {
    try { await apiSend('DELETE', `/oncall/schedules/${id}`); message.success('已删除'); refetch() }
    catch (e: any) { message.error(e?.message ?? '失败') }
  }

  return (
    <Card size="small" extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => { form.resetFields(); setModalOpen(true) }}>新建</Button>}>
      <Table dataSource={list} rowKey={(r) => r.id ?? r.name} pagination={false}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '时区', dataIndex: 'timezone', render: (v) => v ?? 'Asia/Shanghai' },
          { title: '启用', dataIndex: 'enabled', render: (v: boolean) => v ? <Tag color="green">ON</Tag> : <Tag>OFF</Tag> },
          { title: '说明', dataIndex: 'description' },
          { title: '操作', key: 'actions', render: (_, r) => <Popconfirm title="删除？" onConfirm={() => r.id && onDelete(r.id)}><Button danger size="small" icon={<DeleteOutlined />}>删除</Button></Popconfirm> },
        ]} />
      <Modal title="新建值班组" open={modalOpen} onCancel={() => setModalOpen(false)} onOk={onSubmit} okText="保存" cancelText="取消">
        <Form form={form} layout="vertical">
          <Form.Item label="名称" name="name" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item label="时区" name="timezone" initialValue="Asia/Shanghai"><Input /></Form.Item>
          <Form.Item label="启用" name="enabled" valuePropName="checked" initialValue={true}><Switch /></Form.Item>
          <Form.Item label="说明" name="description"><Input.TextArea rows={2} /></Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}

function PoliciesTab() {
  const [form] = Form.useForm<EscalationPolicy>()
  const [modalOpen, setModalOpen] = useState(false)
  const { data, refetch } = useApiQuery<EscalationPolicy[]>(['oncall', 'policies'] as const,
    async () => { try { return await apiGet<EscalationPolicy[]>('/oncall/policies') } catch { return MOCK_POLICIES } })
  const list = data ?? MOCK_POLICIES

  async function onSubmit() {
    try {
      const v = await form.validateFields()
      // levels 是 form 里的 JSON 字符串
      const levels = JSON.parse(v.levelsJson || '[]')
      await apiSend('POST', '/oncall/policies', { name: v.name, enabled: v.enabled, levels })
      message.success('已创建')
      setModalOpen(false)
      refetch()
    } catch (e: any) { if (!e?.errorFields) message.error(e?.message ?? '失败') }
  }

  async function onDelete(id: string) {
    try { await apiSend('DELETE', `/oncall/policies/${id}`); message.success('已删除'); refetch() }
    catch (e: any) { message.error(e?.message ?? '失败') }
  }

  return (
    <Card size="small" extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => { form.resetFields(); setModalOpen(true) }}>新建</Button>}>
      <Table dataSource={list} rowKey={(r) => r.id ?? r.name} pagination={false}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '层级数', key: 'levels', render: (_, r) => <Tag>{r.levels?.length ?? 0} 级</Tag> },
          { title: '启用', dataIndex: 'enabled', render: (v: boolean) => v ? <Tag color="green">ON</Tag> : <Tag>OFF</Tag> },
          { title: '层级详情', key: 'detail', render: (_, r) => (
            <Space size="small" wrap>
              {(r.levels ?? []).map((lv) => (
                <Tag key={lv.level} color="blue">L{lv.level} {lv.target_type}/{lv.target_id} {lv.wait_minutes}m {lv.notify_methods}</Tag>
              ))}
            </Space>
          ) },
          { title: '操作', key: 'actions', render: (_, r) => <Popconfirm title="删除？" onConfirm={() => r.id && onDelete(r.id)}><Button danger size="small" icon={<DeleteOutlined />}>删除</Button></Popconfirm> },
        ]} />
      <Modal title="新建升级策略" open={modalOpen} onCancel={() => setModalOpen(false)} onOk={onSubmit} okText="保存" cancelText="取消">
        <Form form={form} layout="vertical">
          <Form.Item label="名称" name="name" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item label="启用" name="enabled" valuePropName="checked" initialValue={true}><Switch /></Form.Item>
          <Form.Item label="Levels (JSON 数组)" name="levelsJson" initialValue='[{"level":1,"target_type":"user","target_id":"u1","wait_minutes":5,"notify_methods":"email"}]'>
            <Input.TextArea rows={6} placeholder='[{"level":1,"target_type":"user","target_id":"u1","wait_minutes":5,"notify_methods":"email"}]' />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}

export default Oncall
