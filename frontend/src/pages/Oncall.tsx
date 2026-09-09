// 值班 + 升级策略管理（P1-2）
import { useState } from 'react'
import {
  Button, Card, Form, Input, Modal, Popconfirm, Space, Switch, Table, Tag, Typography, Tabs, message,
} from 'antd'
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons'
import { useApiQuery } from '../hooks/useApiQuery'
import { apiGet, apiSend } from '../services/api'
import { ErrorState } from '../components/ErrorState'
import { EmptyState } from '../components/EmptyState'
import { formatDateTime } from '../utils/time'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

const { Text } = Typography

interface OncallSchedule { id?: string; name: string; description?: string; timezone?: string; enabled: boolean }
interface OncallCurrent { schedule_id: string; schedule_name: string; user_name: string; ends_at: string }
interface EscalationPolicy {
  id?: string; name: string; enabled: boolean
  levels: { level: number; target_type: string; target_id: string; wait_minutes: number; notify_methods: string }[]
}

// W1：三处 `catch { return MOCK_* }` 已删除。原写法让 isError 恒 false ——
// 接口失败时页面显示虚构的值班人/值班组/升级策略，运维照着一屏假数据排查。

export function Oncall() {
  useDocumentTitle('值班管理')
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
  const { data, isLoading, isError, error, refetch } = useApiQuery<OncallCurrent[]>(
    ['oncall', 'current'] as const,
    async () => {
      const items = await apiGet<OncallCurrent[]>('/oncall/current')
      return Array.isArray(items) ? items : []
    },
  )

  const list = data ?? []
  return (
    <Card title="当前在班" size="small" loading={isLoading}>
      {isError ? (
        <ErrorState error={error} onRetry={refetch} compact />
      ) : list.length === 0 ? (
        <EmptyState title="当前无人在班" description="没有正在进行的值班排班" compact />
      ) : (
        <Space direction="vertical" style={{ width: '100%' }}>
          {list.map((c) => (
            <Card key={c.schedule_id} size="small" type="inner" title={c.schedule_name}>
              <Text strong style={{ fontSize: 16 }}>{c.user_name}</Text>
              <br />
              {/* W2：原先用无 locale 的 toLocaleString('zh-CN')，与其它页口径不一致 */}
              <Text type="secondary">值班至 {formatDateTime(c.ends_at)}</Text>
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
  // M4：提交按钮 loading，防连点重复创建（范本 AssetFormModal confirmLoading）
  const [submitting, setSubmitting] = useState(false)
  const { data, isLoading, isError, error, refetch } = useApiQuery<OncallSchedule[]>(
    ['oncall', 'schedules'] as const,
    async () => {
      const items = await apiGet<OncallSchedule[]>('/oncall/schedules')
      return Array.isArray(items) ? items : []
    },
  )
  const list = data ?? []

  async function onSubmit() {
    setSubmitting(true)
    try {
      const v = await form.validateFields()
      await apiSend('POST', '/oncall/schedules', v)
      message.success('已创建')
      setModalOpen(false)
      refetch()
    } catch (e: any) { if (!e?.errorFields && !e?.isAxiosError) message.error(e?.message ?? '失败') }
    finally { setSubmitting(false) }
  }

  async function onDelete(id: string) {
    try { await apiSend('DELETE', `/oncall/schedules/${id}`); message.success('已删除'); refetch() }
    catch (e: any) { if (!e?.isAxiosError) message.error(e?.message ?? '失败') }
  }

  return (
    <Card size="small" extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => { form.resetFields(); setModalOpen(true) }}>新建</Button>}>
      {isError ? (
        <ErrorState error={error} onRetry={refetch} compact />
      ) : (
        <Table dataSource={list} rowKey={(r) => r.id ?? r.name} pagination={false} loading={isLoading}
          locale={{ emptyText: <EmptyState title="暂无值班组" description="点击「新建」创建第一个值班组" compact /> }}
          columns={[
            { title: '名称', dataIndex: 'name' },
            { title: '时区', dataIndex: 'timezone', render: (v) => v ?? 'Asia/Shanghai' },
            { title: '启用', dataIndex: 'enabled', render: (v: boolean) => v ? <Tag color="green">ON</Tag> : <Tag>OFF</Tag> },
            { title: '说明', dataIndex: 'description' },
            { title: '操作', key: 'actions', render: (_, r) => (
              <Popconfirm
                title={`确认删除值班组「${r.name}」？`}
                okText="删除"
                cancelText="取消"
                okButtonProps={{ danger: true }}
                onConfirm={() => r.id && onDelete(r.id)}
              >
                <Button danger size="small" icon={<DeleteOutlined />}>删除</Button>
              </Popconfirm>
            ) },
          ]} />
      )}
      <Modal title="新建值班组" open={modalOpen} onCancel={() => setModalOpen(false)} onOk={onSubmit} confirmLoading={submitting} okText="保存" cancelText="取消">
        <Form form={form} layout="vertical">
          <Form.Item label="名称" name="name" rules={[{ required: true, message: '请输入名称' }]}><Input /></Form.Item>
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
  // M4：提交按钮 loading，防连点重复创建（范本 AssetFormModal confirmLoading）
  const [submitting, setSubmitting] = useState(false)
  const { data, isLoading, isError, error, refetch } = useApiQuery<EscalationPolicy[]>(
    ['oncall', 'policies'] as const,
    async () => {
      const items = await apiGet<EscalationPolicy[]>('/oncall/policies')
      return Array.isArray(items) ? items : []
    },
  )
  const list = data ?? []

  async function onSubmit() {
    setSubmitting(true)
    try {
      // levelsJson 是 Form.Item 字段, 接口层 EscalationPolicy 不含, 这里 cast
      const v = await form.validateFields() as EscalationPolicy & { levelsJson?: string }
      // levels 是 form 里的 JSON 字符串
      const levels = JSON.parse(v.levelsJson || '[]')
      await apiSend('POST', '/oncall/policies', { name: v.name, enabled: v.enabled, levels })
      message.success('已创建')
      setModalOpen(false)
      refetch()
    } catch (e: any) {
      if (e?.errorFields || e?.isAxiosError) return
      // M7：JSON.parse 抛 SyntaxError 时给友好中文，而非英文技术报错（如 "Unexpected token..."）
      message.error(e instanceof SyntaxError ? 'Levels JSON 格式错误，请检查后重试' : (e?.message ?? '失败'))
    }
    finally { setSubmitting(false) }
  }

  async function onDelete(id: string) {
    try { await apiSend('DELETE', `/oncall/policies/${id}`); message.success('已删除'); refetch() }
    catch (e: any) { if (!e?.isAxiosError) message.error(e?.message ?? '失败') }
  }

  return (
    <Card size="small" extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => { form.resetFields(); setModalOpen(true) }}>新建</Button>}>
      {isError ? (
        <ErrorState error={error} onRetry={refetch} compact />
      ) : (
        <Table dataSource={list} rowKey={(r) => r.id ?? r.name} pagination={false} loading={isLoading}
          locale={{ emptyText: <EmptyState title="暂无升级策略" description="点击「新建」创建第一条升级策略" compact /> }}
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
            { title: '操作', key: 'actions', render: (_, r) => (
              <Popconfirm
                title={`确认删除升级策略「${r.name}」？`}
                okText="删除"
                cancelText="取消"
                okButtonProps={{ danger: true }}
                onConfirm={() => r.id && onDelete(r.id)}
              >
                <Button danger size="small" icon={<DeleteOutlined />}>删除</Button>
              </Popconfirm>
            ) },
          ]} />
      )}
      <Modal title="新建升级策略" open={modalOpen} onCancel={() => setModalOpen(false)} onOk={onSubmit} confirmLoading={submitting} okText="保存" cancelText="取消">
        <Form form={form} layout="vertical">
          <Form.Item label="名称" name="name" rules={[{ required: true, message: '请输入名称' }]}><Input /></Form.Item>
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
