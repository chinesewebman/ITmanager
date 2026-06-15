import { useEffect, useState } from 'react'
import { Button, Select, Space, message } from 'antd'
import { SyncOutlined } from '@ant-design/icons'
import { alertApi } from '../services/api'
import { PageHeader } from '../components/PageHeader'
import { AlertTable, type Alert } from '../components/AlertTable'
import { AlertStatsCards, type AlertStats } from '../components/AlertStatsCards'

const MOCK_ALERTS: Alert[] = [
  { id: '1', host: 'web-server-01', message: 'CPU使用率超过90%', severity: 5, severity_name: '灾难', status: 'problem', created_at: '2026-02-14 10:00:00' },
  { id: '2', host: 'db-server-02', message: '磁盘空间不足', severity: 4, severity_name: '严重', status: 'problem', created_at: '2026-02-14 09:30:00' },
  { id: '3', host: 'switch-core-01', message: '端口状态异常', severity: 3, severity_name: '一般', status: 'acknowledged', created_at: '2026-02-14 08:00:00', ack_time: '2026-02-14 08:30:00' },
  { id: '4', host: 'firewall-main', message: '连接数超阈值', severity: 3, severity_name: '一般', status: 'resolved', created_at: '2026-02-13 20:00:00' },
]

const DEFAULT_STATS: AlertStats = { total: 15, problem: 8, acknowledged: 3, resolved: 4 }

function Alerts() {
  const [data, setData] = useState<Alert[]>([])
  const [loading, setLoading] = useState(false)
  const [stats, setStats] = useState<AlertStats>(DEFAULT_STATS)
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [severityFilter, setSeverityFilter] = useState<string>('')

  const fetchData = async () => {
    setLoading(true)
    try {
      const params: { status?: string; severity?: string } = {}
      if (statusFilter) params.status = statusFilter
      if (severityFilter) params.severity = severityFilter

      const res: any = await alertApi.list(params)
      setData(res?.data?.data?.items ?? [])
      setStats(res?.data?.data?.stats ?? DEFAULT_STATS)
    } catch (error) {
      console.error('获取告警列表失败:', error)
      setData(MOCK_ALERTS)
      setStats(DEFAULT_STATS)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [statusFilter, severityFilter])

  const handleAck = async (id: string) => {
    try {
      await alertApi.acknowledge(id)
      message.success('告警已确认')
      fetchData()
    } catch {
      // 兜底：本地变更
      setData((prev) =>
        prev.map((a) => (a.id === id ? { ...a, status: 'acknowledged', ack_time: new Date().toISOString() } : a))
      )
    }
  }

  const handleResolve = async (id: string) => {
    try {
      await alertApi.resolve(id)
      message.success('告警已解决')
      fetchData()
    } catch {
      setData((prev) => prev.map((a) => (a.id === id ? { ...a, status: 'resolved' } : a)))
    }
  }

  return (
    <div>
      <PageHeader
        title="告警中心"
        subtitle={`当前 ${stats.problem} 个未处理告警`}
        extra={
          <Button icon={<SyncOutlined />} onClick={fetchData}>
            刷新
          </Button>
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

      <AlertTable data={data} loading={loading} onAck={handleAck} onResolve={handleResolve} />
    </div>
  )
}

export default Alerts
