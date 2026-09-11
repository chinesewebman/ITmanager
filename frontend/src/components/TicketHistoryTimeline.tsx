// M25 工单经手历史时间线（设计见 docs/FIX-PLAN-TICKET-HISTORY.md §2.8）。
//
// 这个组件只做三件事：取数、loading/error/空态、把纯函数分组好的结果渲染出来。
// 分组、组内排序、值渲染全在 utils/ticketHistory.ts —— 那些是会出错的部分，
// 做成纯函数才能脱离 React 做单测与变异反证。
import { useMemo, useState } from 'react'
import { Button, Space, Timeline, Typography } from 'antd'
import { ClockCircleOutlined } from '@ant-design/icons'
import { ticketApi } from '../services/api'
import { useApiQuery, queryKeys } from '../hooks/useApiQuery'
import { ErrorState } from './ErrorState'
import { formatDateTime } from '../utils/time'
import { formatHistoryValue, fieldLabel, groupTicketHistory } from '../utils/ticketHistory'
import type { TicketHistory } from '../types'

const { Text } = Typography

/** 首屏条数。列表页是 20，这里放宽 —— 时间线是「一口气往回看」的场景。 */
const PAGE_SIZE = 50

export interface TicketHistoryTimelineProps {
  ticketId: string
}

async function fetchHistory(
  ticketId: string,
  page: number,
): Promise<{ items: TicketHistory[]; total: number }> {
  const res: any = await ticketApi.history(ticketId, { page, page_size: PAGE_SIZE })
  const body = res?.data?.data
  return {
    items: Array.isArray(body?.items) ? body.items : [],
    total: body?.total ?? 0,
  }
}

export function TicketHistoryTimeline({ ticketId }: TicketHistoryTimelineProps) {
  const [extra, setExtra] = useState<TicketHistory[]>([])
  const [page, setPage] = useState(1)
  const [loadingMore, setLoadingMore] = useState(false)
  const [moreFailed, setMoreFailed] = useState(false)

  const { data, isLoading, isError, error, refetch } = useApiQuery(
    queryKeys.tickets.history(ticketId),
    () => fetchHistory(ticketId, 1),
    { enabled: !!ticketId },
  )

  // 翻页回来的行追加在后面，日期分组照旧由 groupTicketHistory 按输入顺序保持。
  const rows = useMemo(() => [...(data?.items ?? []), ...extra], [data, extra])
  const groups = useMemo(() => groupTicketHistory(rows), [rows])
  const total = data?.total ?? 0

  const loadMore = async () => {
    setLoadingMore(true)
    setMoreFailed(false)
    try {
      const next = await fetchHistory(ticketId, page + 1)
      setExtra((prev) => [...prev, ...next.items])
      setPage((p) => p + 1)
    } catch {
      // 追加失败不清空已显示的行 —— 只是够不到更早的。给出重试入口。
      setMoreFailed(true)
    } finally {
      setLoadingMore(false)
    }
  }

  // 加载失败只落在本区块内，不影响票面信息（票面数据父组件已经拿到）。
  if (isError) return <ErrorState error={error} onRetry={refetch} compact />

  if (isLoading) return <Text type="secondary">加载中…</Text>

  if (groups.length === 0) {
    // 表是 M25 才建的，**所有老票都没历史**，这是常态不是异常。
    // 写「无记录」会被读成「这票从没被改过」—— 那句话是假的。
    return <Text type="secondary">暂无经手记录（本功能上线前的改动未记录）</Text>
  }

  return (
    <>
      <Timeline
        mode="left"
        items={groups.map((g) => ({
          color: g.kind === 'created' ? 'green' : 'blue',
          dot: <ClockCircleOutlined style={{ fontSize: 14 }} />,
          label: formatDateTime(g.createdAt),
          children: (
            <div>
              <Space size={8}>
                <Text strong>{g.actorName}</Text>
                <Text type="secondary">
                  {g.kind === 'created' ? '创建了这张工单' : `修改了 ${g.rows.length} 个字段`}
                </Text>
              </Space>
              {g.kind === 'updated' && (
                <div style={{ marginTop: 4 }}>
                  {g.rows.map((r) => (
                    <div key={r.id}>
                      <Text type="secondary">{fieldLabel(r.field_name ?? '')}：</Text>
                      <Text delete type="secondary">
                        {formatHistoryValue(r.field_name ?? '', r.old_value)}
                      </Text>
                      <Text type="secondary"> → </Text>
                      <Text>{formatHistoryValue(r.field_name ?? '', r.new_value)}</Text>
                    </div>
                  ))}
                </div>
              )}
            </div>
          ),
        }))}
      />
      {rows.length < total && (
        <div style={{ textAlign: 'center' }}>
          {moreFailed && (
            <Text type="danger" style={{ marginRight: 8 }}>
              更早的记录加载失败
            </Text>
          )}
          <Button size="small" loading={loadingMore} onClick={loadMore}>
            加载更多（还有 {total - rows.length} 条）
          </Button>
        </div>
      )}
    </>
  )
}

export default TicketHistoryTimeline
