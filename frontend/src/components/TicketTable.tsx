import { Button, Space, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { StatusTag } from './StatusTag'
import { EmptyState } from './EmptyState'
import { formatDateTime } from '../utils/time'

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
  // M16 已归一：契约（openapi.yaml:2448-2450）与三个写入方都是 normal，
  // 迁移 000023 也把存量 medium 改了。这条是**遗留同义词安全网** ——
  // 未跑迁移的库、以及外部直接写库的行仍可能带 medium，留着免得把英文原值怼给用户。
  // 一个版本后可删（删前先确认没有 priority='medium' 的行）。
  medium: '普通',
  low: '低',
}

// M2：优先级按严重度权重排序（critical 最高）。medium 与 normal 同权 ——
// 归一后它只是上面的安全网条目，权重仍需一致，否则遗留行的排序会跳档。
const PRIORITY_WEIGHT: Record<string, number> = {
  critical: 4,
  high: 3,
  normal: 2,
  medium: 2,
  low: 1,
}

export interface TicketTableProps {
  data: Ticket[]
  loading: boolean
  onView: (ticket: Ticket) => void
  // M3/P5: 服务端分页受控。total 传入时启用受控分页（current/pageSize/onChange 由父组件持有），
  // 否则回落到 antd 内部分页（前端假分页，仅兼容旧调用方）。
  total?: number
  page?: number
  pageSize?: number
  onPageChange?: (page: number, pageSize: number) => void
}

export function TicketTable({ data, loading, onView, total, page, pageSize, onPageChange }: TicketTableProps) {
  const columns: ColumnsType<Ticket> = [
    // M2：加前端本地排序。优先级按严重度权重；创建时间按 Date 解析（RFC3339 字符串
    // 字典序会因时区偏移错序）；其余字符串列 localeCompare（assignee 可能为空）。
    { title: '工单标题', dataIndex: 'title', key: 'title', sorter: (a, b) => a.title.localeCompare(b.title) },
    {
      title: '优先级',
      dataIndex: 'priority',
      key: 'priority',
      width: 80,
      sorter: (a, b) => (PRIORITY_WEIGHT[a.priority] ?? 0) - (PRIORITY_WEIGHT[b.priority] ?? 0),
      render: (p: string) => (
        <StatusTag value={p} label={PRIORITY_LABEL[p] || p} />
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 80,
      sorter: (a, b) => a.status.localeCompare(b.status),
      render: (s: string) => <StatusTag value={s} />,
    },
    { title: '请求人', dataIndex: 'requester', key: 'requester', width: 100, sorter: (a, b) => a.requester.localeCompare(b.requester) },
    {
      title: '处理人',
      dataIndex: 'assignee',
      key: 'assignee',
      width: 100,
      sorter: (a, b) => (a.assignee ?? '').localeCompare(b.assignee ?? ''),
      render: (a?: string) => a || '-',
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 180,
      sorter: (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
      // W2：原样渲染后端时间串，与其它页口径不一致
      render: (iso: string) => formatDateTime(iso),
    },
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
      pagination={
        total !== undefined
          ? {
              current: page ?? 1,
              pageSize: pageSize ?? 20,
              total,
              showSizeChanger: true,
              showTotal: (t) => `共 ${t} 条`,
              pageSizeOptions: ['10', '20', '50', '100'],
              onChange: (p, ps) => onPageChange?.(p, ps),
            }
          : {
              pageSize: 10,
              showSizeChanger: true,
              showTotal: (t) => `共 ${t} 条`,
            }
      }
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
