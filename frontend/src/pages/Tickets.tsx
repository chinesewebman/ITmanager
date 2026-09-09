import { Button, Select, Space, message } from 'antd'
import { SyncOutlined } from '@ant-design/icons'
import { ticketApi } from '../services/api'
import { PageHeader } from '../components/PageHeader'
import { ErrorState } from '../components/ErrorState'
import { TicketTable, type Ticket } from '../components/TicketTable'
import { TicketFormModal, type TicketFormValues } from '../components/TicketFormModal'
import { TicketDetailModal } from '../components/TicketDetailModal'
import { TicketStatsCards, type TicketStats } from '../components/TicketStatsCards'
import { useApiMutation, useApiQuery, queryKeys } from '../hooks/useApiQuery'
import { useState, useMemo } from 'react'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

// 统计卡要全量计数，后端 list 默认 20 条会截断 → 放大分页。
// P5（服务端分页 + /tickets/stats）落地后改成后端聚合。
const STATS_PAGE_SIZE = 500

const EMPTY_STATS: TicketStats = {
  open: 0,
  in_progress: 0,
  pending: 0,
  resolved: 0,
  closed: 0,
}

/** 列表与统计共用的取数（含 C-F14 字段兼容映射）。 */
async function fetchTickets(params?: {
  status?: string
  priority?: string
  page_size?: number
}): Promise<Ticket[]> {
  const res: any = await ticketApi.list(params)
  const raw = res?.data?.data?.items ?? res?.data?.data ?? []
  // C-F14: requester_name/assignee_name → requester/assignee 兼容映射
  return (Array.isArray(raw) ? raw : []).map((t: any) => ({
    ...t,
    requester: t.requester ?? t.requester_name ?? '',
    assignee: t.assignee ?? t.assignee_name ?? '',
  }))
}

function Tickets() {
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [priorityFilter, setPriorityFilter] = useState<string>('')
  const [createOpen, setCreateOpen] = useState(false)
  const [viewTicket, setViewTicket] = useState<Ticket | null>(null)

  // C-P9: filter 变化走 queryKey 隔离缓存
  const filters = { status: statusFilter, priority: priorityFilter }

  useDocumentTitle('工单管理')
  // W1：删除 MOCK_TICKETS 回落，失败交给 isError → 区块级错误态
  const { data, isLoading, isError, error, refetch } = useApiQuery<Ticket[]>(
    queryKeys.tickets.list(filters),
    () =>
      fetchTickets({
        status: statusFilter || undefined,
        priority: priorityFilter || undefined,
      }),
  )

  // W1：统计卡此前无条件用写死的 DEFAULT_STATS（与接口无关），
  // 改为从**未筛选**列表推导真实四档。
  const {
    data: statsData,
    isLoading: statsLoading,
    isError: statsIsError,
    error: statsError,
    refetch: statsRefetch,
  } = useApiQuery<Ticket[]>(queryKeys.tickets.stats(), () =>
    fetchTickets({ page_size: STATS_PAGE_SIZE }),
  )

  const stats = useMemo<TicketStats>(() => {
    const acc: TicketStats = { ...EMPTY_STATS }
    for (const t of statsData ?? []) {
      // 未知状态（后端新增枚举）忽略，不写入不存在的键
      if (t.status in acc) acc[t.status as keyof TicketStats] += 1
    }
    return acc
  }, [statsData])

  const createMut = useApiMutation((v: TicketFormValues) => ticketApi.create(v), {
    onSuccess: () => {
      message.success('工单已创建')
      setCreateOpen(false)
      refetch()
      statsRefetch()
    },
    onError: () => message.error('创建失败'),
  })

  const list = data ?? []
  // M10：副标题原本用未过滤总数，与表格行数不符
  const hasFilter = Boolean(statusFilter || priorityFilter)

  return (
    <div>
      <PageHeader
        title="工单管理"
        subtitle={`共 ${list.length} 个工单${hasFilter ? '（已筛选）' : ''}`}
        onCreate={() => setCreateOpen(true)}
        createText="创建工单"
        extra={
          <Button
            icon={<SyncOutlined />}
            onClick={() => {
              refetch()
              statsRefetch()
            }}
          >
            刷新
          </Button>
        }
      />

      {isError ? (
        <ErrorState error={error} onRetry={refetch} />
      ) : (
        <>
          {/* 统计卡是独立 query：它失败不牵连列表，只在区块内报错 */}
          {statsIsError ? (
            <ErrorState error={statsError} onRetry={statsRefetch} compact />
          ) : (
            <TicketStatsCards stats={stats} loading={statsLoading} />
          )}

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
                  { label: '待定', value: 'pending' },
                  { label: '已解决', value: 'resolved' },
                  { label: '已关闭', value: 'closed' },
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
          <TicketTable data={list} loading={isLoading} onView={setViewTicket} />
        </>
      )}

      <TicketFormModal
        open={createOpen}
        submitting={createMut.isPending}
        onCancel={() => setCreateOpen(false)}
        onSubmit={(v) => createMut.mutate(v)}
      />
      <TicketDetailModal ticket={viewTicket} onClose={() => setViewTicket(null)} />
    </div>
  )
}

export default Tickets
