import { Button, Select, Space, message } from 'antd'
import { SyncOutlined, CheckOutlined, CheckCircleOutlined } from '@ant-design/icons'
import { alertApi } from '../services/api'
import type { AlertListParams } from '../services/apiClient'
import { PageHeader } from '../components/PageHeader'
import { AlertTable, type Alert } from '../components/AlertTable'
import { AlertStatsCards, type AlertStats } from '../components/AlertStatsCards'
import { useApiMutation, useApiQuery, queryKeys } from '../hooks/useApiQuery'
import { useState } from 'react'

const MOCK_ALERTS: Alert[] = [
  { id: '1', host: 'web-server-01', message: 'CPU使用率超过90%', severity: 5, severity_name: '灾难', status: 'problem', created_at: '2026-02-14 10:00:00' },
  { id: '2', host: 'db-server-02', message: '磁盘空间不足', severity: 4, severity_name: '严重', status: 'problem', created_at: '2026-02-14 09:30:00' },
  { id: '3', host: 'switch-core-01', message: '端口状态异常', severity: 3, severity_name: '一般', status: 'acknowledged', created_at: '2026-02-14 08:00:00', ack_time: '2026-02-14 08:30:00' },
  { id: '4', host: 'firewall-main', message: '连接数超阈值', severity: 3, severity_name: '一般', status: 'resolved', created_at: '2026-02-13 20:00:00' },
]

const DEFAULT_STATS: AlertStats = { total: 15, problem: 8, acknowledged: 3, resolved: 4 }

interface AlertsResp {
  items: Alert[]
  stats: AlertStats
}

function Alerts() {
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [severityFilter, setSeverityFilter] = useState<string>('')
  const [selectedIds, setSelectedIds] = useState<string[]>([])

  // C-P9: 列表 + stats 合并到 React Query，filter 变化走 queryKey 隔离缓存
  const filters = { status: statusFilter, severity: severityFilter }
  const { data, isLoading, refetch } = useApiQuery<AlertsResp>(
    queryKeys.alerts.list(filters),
    async () => {
      // antd Select onChange 给 string，但 spec 要求 literal union
      // 调用方负责 narrow（业务已知：只有 3 个合法值）
      const params = {
        ...(statusFilter && { status: statusFilter as AlertListParams['status'] }),
        ...(severityFilter && { severity: severityFilter }),
      } as AlertListParams
      const res: any = await alertApi.list(params)
      return {
        items: res?.data?.data?.items ?? MOCK_ALERTS,
        stats: res?.data?.data?.stats ?? DEFAULT_STATS,
      }
    },
  )

  // 写操作 invalidate alerts 树
  const ackMut = useApiMutation((id: string) => alertApi.acknowledge(id), {
    onSuccess: () => {
      message.success('告警已确认')
      refetch()
    },
    onError: () => message.error('确认失败'),
  })
  const resolveMut = useApiMutation((id: string) => alertApi.resolve(id), {
    onSuccess: () => {
      message.success('告警已解决')
      refetch()
    },
    onError: () => message.error('解决失败'),
  })

  // C-P6: 批量写操作
  const bulkAckMut = useApiMutation(
    (ids: string[]) => alertApi.bulkAcknowledge(ids).then((r) => ({ r, count: ids.length })),
    {
      onSuccess: ({ count }) => {
        message.success(`已批量确认 ${count} 条告警`)
        setSelectedIds([])
        refetch()
      },
      onError: () => message.error('批量确认失败'),
    },
  )
  const bulkResolveMut = useApiMutation(
    (ids: string[]) => alertApi.bulkResolve(ids).then((r) => ({ r, count: ids.length })),
    {
      onSuccess: ({ count }) => {
        message.success(`已批量解决 ${count} 条告警`)
        setSelectedIds([])
        refetch()
      },
      onError: () => message.error('批量解决失败'),
    },
  )

  const list = data?.items ?? MOCK_ALERTS
  const stats = data?.stats ?? DEFAULT_STATS
  const hasSelection = selectedIds.length > 0

  return (
    <div>
      <PageHeader
        title="告警中心"
        subtitle={`当前 ${stats.problem} 个未处理告警${hasSelection ? `，已选 ${selectedIds.length} 条` : ''}`}
        extra={
          <Space>
            {hasSelection && (
              <>
                <Button
                  icon={<CheckOutlined />}
                  onClick={() => bulkAckMut.mutate(selectedIds)}
                  loading={bulkAckMut.isPending}
                >
                  批量确认
                </Button>
                <Button
                  type="primary"
                  icon={<CheckCircleOutlined />}
                  onClick={() => bulkResolveMut.mutate(selectedIds)}
                  loading={bulkResolveMut.isPending}
                >
                  批量解决
                </Button>
              </>
            )}
            <Button icon={<SyncOutlined />} onClick={() => refetch()}>
              刷新
            </Button>
          </Space>
        }
      />

      <AlertStatsCards stats={stats} />

      <div style={{ marginBottom: 16 }}>
        <Space>
          <Select
            placeholder="状态"
            allowClear
            value={statusFilter || undefined}
            onChange={(v) => setStatusFilter(v ?? '')}
            style={{ width: 120 }}
            options={[
              { label: '未处理', value: 'problem' },
              { label: '已确认', value: 'acknowledged' },
              { label: '已解决', value: 'resolved' },
            ]}
          />
          <Select
            placeholder="严重级别 ≥"
            allowClear
            value={severityFilter || undefined}
            onChange={(v) => setSeverityFilter(v ?? '')}
            style={{ width: 140 }}
            options={[
              { label: '灾难 (≥5)', value: '5' },
              { label: '严重 (≥4)', value: '4' },
              { label: '一般 (≥3)', value: '3' },
            ]}
          />
        </Space>
      </div>

      <AlertTable
        data={list}
        loading={isLoading}
        onAck={(id) => ackMut.mutate(id)}
        onResolve={(id) => resolveMut.mutate(id)}
        selectedIds={selectedIds}
        onSelectionChange={setSelectedIds}
      />
    </div>
  )
}

export default Alerts
