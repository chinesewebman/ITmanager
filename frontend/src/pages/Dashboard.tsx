import { Card, Col, List, Row, Tag } from 'antd'
import { dashboardApi } from '../services/api'
import { PageHeader } from '../components/PageHeader'
import { DashboardCards, type DashboardCardsStats } from '../components/DashboardCards'
import { AlertTrendChart, type AlertTrend } from '../components/AlertTrendChart'
import { useApiQuery, queryKeys } from '../hooks/useApiQuery'

const MOCK_STATS: DashboardCardsStats = {
  assets: 156,
  alerts: 8,
  tickets: 23,
  sites: 3,
  machines: 45,
  networks: 12,
}

const MOCK_TRENDS: AlertTrend[] = [
  { date: '2026-02-08', count: 10 },
  { date: '2026-02-09', count: 15 },
  { date: '2026-02-10', count: 8 },
  { date: '2026-02-11', count: 12 },
  { date: '2026-02-12', count: 5 },
  { date: '2026-02-13', count: 7 },
  { date: '2026-02-14', count: 3 },
]

interface RecentAlert {
  id: number
  host: string
  message: string
  severity: 'critical' | 'warning' | 'error' | string
  time: string
}

const MOCK_RECENT: RecentAlert[] = [
  { id: 1, host: 'web-server-01', message: 'CPU使用率超过90%', severity: 'critical', time: '10分钟前' },
  { id: 2, host: 'db-server-02', message: '磁盘空间不足', severity: 'warning', time: '30分钟前' },
  { id: 3, host: 'switch-core-01', message: '端口状态异常', severity: 'error', time: '1小时前' },
  { id: 4, host: 'firewall-main', message: '连接数超阈值', severity: 'warning', time: '2小时前' },
]

function Dashboard() {
  // C-P9: stats + trends 走 React Query（1min 缓存）
  const { data: stats } = useApiQuery<DashboardCardsStats>(
    queryKeys.dashboard.stats(),
    async () => {
      const res: any = await dashboardApi.getStats()
      return res?.data?.data ?? MOCK_STATS
    },
    { staleTime: 60_000 },
  )
  const { data: trends } = useApiQuery<AlertTrend[]>(
    queryKeys.dashboard.trends(),
    async () => {
      const res: any = await dashboardApi.getTrends()
      return res?.data?.data?.alert_trends ?? MOCK_TRENDS
    },
    { staleTime: 60_000 },
  )

  const safeStats = stats ?? MOCK_STATS
  const safeTrends = trends ?? MOCK_TRENDS

  // 简化的 delta 计算：对比前 3 天 vs 后 3 天的均值
  const delta =
    safeTrends.length >= 6
      ? safeTrends.slice(-3).reduce((s, p) => s + p.count, 0) -
        safeTrends.slice(-6, -3).reduce((s, p) => s + p.count, 0)
      : 5

  return (
    <div>
      <PageHeader title="仪表盘" subtitle="网络运维平台核心指标速览" />

      <DashboardCards stats={safeStats} alertTrendDelta={delta} />

      <Row gutter={16} style={{ marginTop: 16 }}>
        <Col span={16}>
          <AlertTrendChart data={safeTrends} />
        </Col>
        <Col span={8}>
          <Card title="最近告警" style={{ height: 380 }}>
            <List
              dataSource={MOCK_RECENT}
              renderItem={(item) => (
                <List.Item>
                  <List.Item.Meta
                    title={item.host}
                    description={
                      <div>
                        <span>{item.message}</span>
                        <div>
                          <Tag color={item.severity === 'critical' ? 'red' : 'orange'} style={{ marginTop: 4 }}>
                            {item.severity}
                          </Tag>
                          <span style={{ color: '#999', fontSize: 12, marginLeft: 8 }}>{item.time}</span>
                        </div>
                      </div>
                    }
                  />
                </List.Item>
              )}
            />
          </Card>
        </Col>
      </Row>
    </div>
  )
}

export default Dashboard
