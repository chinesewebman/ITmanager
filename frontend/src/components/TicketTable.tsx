import { Button, Space, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { StatusTag } from './StatusTag'
import { EmptyState } from './EmptyState'

export interface Ticket {
  id: string
  title: string
  priority: 'critical' | 'high' | 'normal' | 'low' | string
  status: 'open' | 'in_progress' | 'pending' | 'resolved' | 'closed' | string
  requester: string
  assignee?: string
  created_at: string
  updated_at: string
}

const PRIORITY_LABEL: Record<string, string> = {
  critical: '紧急',
  high: '高',
  normal: '普通',
  low: '低',
}

export interface TicketTableProps {
  data: Ticket[]
  loading: boolean
  onView: (ticket: Ticket) => void
}

export function TicketTable({ data, loading, onView }: TicketTableProps) {
  const columns: ColumnsType<Ticket> = [
    { title: '工单标题', dataIndex: 'title', key: 'title' },
    {
      title: '优先级',
      dataIndex: 'priority',
      key: 'priority',
      width: 80,
      render: (p: string) => (
        <StatusTag value={p} label={PRIORITY_LABEL[p] || p} />
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 80,
      render: (s: string) => <StatusTag value={s} />,
    },
    { title: '请求人', dataIndex: 'requester', key: 'requester', width: 100 },
    {
      title: '处理人',
      dataIndex: 'assignee',
      key: 'assignee',
      width: 100,
      render: (a?: string) => a || '-',
    },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 160 },
    {
      title: '操作',
      key: 'action',
      width: 100,
      render: (_, record) => (
        <Space>
          <Button type="link" size="small" onClick={() => onView(record)}>
            详情
          </Button>
        </Space>
      ),
    },
  ]

  return (
    <Table<Ticket>
      rowKey="id"
      columns={columns}
      dataSource={data}
      loading={loading}
      pagination={{ pageSize: 10, showSizeChanger: true }}
      locale={{
        emptyText: (
          <EmptyState
            title="暂无工单"
            description="当前没有待处理的工单"
            compact
          />
        ),
      }}
    />
  )
}

export default TicketTable
