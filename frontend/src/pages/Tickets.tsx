import { useEffect, useState } from 'react'
import { Button, Select, Space, message } from 'antd'
import { SyncOutlined } from '@ant-design/icons'
import { ticketApi } from '../services/api'
import { PageHeader } from '../components/PageHeader'
import { TicketTable, type Ticket } from '../components/TicketTable'
import { TicketFormModal, type TicketFormValues } from '../components/TicketFormModal'
import { TicketDetailModal } from '../components/TicketDetailModal'
import { TicketStatsCards } from '../components/TicketStatsCards'

const MOCK_TICKETS: Ticket[] = [
  { id: '1', title: '服务器磁盘空间不足', priority: 'high', status: 'open', requester: '张三', assignee: '李四', created_at: '2026-02-14 10:00:00', updated_at: '2026-02-14 11:00:00' },
  { id: '2', title: '网络延迟过高', priority: 'critical', status: 'in_progress', requester: '王五', assignee: '李四', created_at: '2026-02-13 15:00:00', updated_at: '2026-02-14 09:00:00' },
  { id: '3', title: '新增一台服务器', priority: 'normal', status: 'pending', requester: '赵六', created_at: '2026-02-12 09:00:00', updated_at: '2026-02-12 10:00:00' },
  { id: '4', title: '防火墙规则变更', priority: 'high', status: 'resolved', requester: '孙七', assignee: '李四', created_at: '2026-02-10 14:00:00', updated_at: '2026-02-11 16:00:00' },
]

const DEFAULT_STATS = { pending: 3, inProgress: 5, waiting: 2, resolved: 15 }

function Tickets() {
  const [data, setData] = useState<Ticket[]>([])
  const [loading, setLoading] = useState(false)
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [priorityFilter, setPriorityFilter] = useState<string>('')
  const [createOpen, setCreateOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [viewTicket, setViewTicket] = useState<Ticket | null>(null)

  const fetchData = async () => {
    setLoading(true)
    try {
      // 走 services/api.ts 拦截器，自动带 token (C-F9 修复)
      // status/priority 透传到后端 (C-F10 修复)
      const res: any = await ticketApi.list({
        status: statusFilter || undefined,
        priority: priorityFilter || undefined,
      })
      const raw = res?.data?.data?.items ?? res?.data?.data ?? []
      // C-F14: 后端模型字段是 requester_name/assignee_name，
      // 前端 TicketTable 用 requester/assignee — 兼容映射避免空表格
      const normalized = (Array.isArray(raw) ? raw : []).map((t: any) => ({
        ...t,
        requester: t.requester ?? t.requester_name ?? '',
        assignee: t.assignee ?? t.assignee_name ?? '',
      }))
      setData(normalized)
    } catch (e) {
      console.error('获取工单列表失败:', e)
      setData(MOCK_TICKETS)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [statusFilter, priorityFilter])

  const handleSubmit = async (values: TicketFormValues) => {
    setSubmitting(true)
    try {
      await ticketApi.create(values)
      message.success('工单已创建')
      setCreateOpen(false)
      fetchData()
    } catch {
      message.error('创建失败（已用本地 mock）')
      // 兜底：本地插入一条让用户看见（C-F14 兼容映射）
      setData((prev) => [
        { id: String(Date.now()), ...values, status: 'open', requester: '当前用户', requester_name: '当前用户', assignee: '', assignee_name: '', created_at: new Date().toISOString(), updated_at: new Date().toISOString() } as Ticket,
        ...prev,
      ])
      setCreateOpen(false)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div>
      <PageHeader
        title="工单管理"
        subtitle={`共 ${data.length} 个工单`}
        onCreate={() => setCreateOpen(true)}
        createText="创建工单"
        extra={
          <Button icon={<SyncOutlined />} onClick={fetchData}>
            刷新
          </Button>
        }
      />

      <TicketStatsCards stats={DEFAULT_STATS} />

      <div style={{ marginBottom: 16 }}>
        <Space>
          <Select
            placeholder="工单状态"
            allowClear
            value={statusFilter || undefined}
            onChange={(v) => setStatusFilter(v ?? '')}
            style={{ width: 120 }}
            options={[
              { label: '新建', value: 'open' },
              { label: '处理中', value: 'in_progress' },
              { label: '等待中', value: 'pending' },
              { label: '已解决', value: 'resolved' },
            ]}
          />
          <Select
            placeholder="优先级"
            allowClear
            value={priorityFilter || undefined}
            onChange={(v) => setPriorityFilter(v ?? '')}
            style={{ width: 120 }}
            options={[
              { label: '紧急', value: 'critical' },
              { label: '高', value: 'high' },
              { label: '普通', value: 'normal' },
              { label: '低', value: 'low' },
            ]}
          />
        </Space>
      </div>

      <TicketTable data={data} loading={loading} onView={setViewTicket} />

      <TicketFormModal
        open={createOpen}
        submitting={submitting}
        onCancel={() => setCreateOpen(false)}
        onSubmit={handleSubmit}
      />
      <TicketDetailModal ticket={viewTicket} onClose={() => setViewTicket(null)} />
    </div>
  )
}

export default Tickets
