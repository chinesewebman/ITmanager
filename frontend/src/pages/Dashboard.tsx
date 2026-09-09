import { Card, Col, List, Row, Typography } from 'antd'
import { dashboardApi, alertApi } from '../services/api'
import { PageHeader } from '../components/PageHeader'
import { DashboardCards, type DashboardCardsStats } from '../components/DashboardCards'
import { AlertTrendChart, type AlertTrend } from '../components/AlertTrendChart'
import { KpiCards, type KPI } from '../components/KpiCards'
import { ErrorState } from '../components/ErrorState'
import { EmptyState } from '../components/EmptyState'
import { LoadingSkeleton } from '../components/LoadingSkeleton'
import { SeverityTag } from '../components/SeverityTag'
import type { Alert } from '../components/AlertTable'
import { useApiQuery, queryKeys } from '../hooks/useApiQuery'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { formatRelativeTime } from '../utils/time'

const RECENT_ALERT_LIMIT = 5
const EMPTY_STATS: DashboardCardsStats = {
  assets: 0,
  alerts: 0,
  tickets: 0,
  sites: 0,
  machines: 0,
  networks: 0,
}

function Dashboard() {
  useDocumentTitle('仪表盘')

  // FIX-PLAN-UI-PERF §W1：四个区块各自暴露 isLoading / isError。
  // 修前 stats/trends 空值回落 MOCK_STATS / MOCK_TRENDS，失败被伪装成成功。
  const statsQ = useApiQuery<DashboardCardsStats | null>(
    queryKeys.dashboard.stats(),
    async () => {
      const res: any = await dashboardApi.getStats()
      return res?.data?.data ?? null
    },
    { staleTime: 60_000 },
  )
  const trendsQ = useApiQuery<AlertTrend[]>(
    queryKeys.dashboard.trends(),
    async () => {
      const res: any = await dashboardApi.getTrends()
      const list = res?.data?.data?.alert_trends
      return Array.isArray(list) ? list : []
    },
    { staleTime: 60_000 },
  )
  const kpiQ = useApiQuery<KPI | null>(
    ['dashboard', 'kpis'],
    async () => {
      const res: any = await dashboardApi.getKPIs()
      return res?.data?.data ?? null
    },
    { staleTime: 60_000 },
  )
  // 后端 /dashboard/* 只有 stats/trends/kpis（routes.go:351-355），没有「最近告警」接口，
  // 复用 GET /alerts：服务端已按 created_at DESC, id DESC 排序（alert_service.go:134）。
  // 修前这里是无条件渲染的 4 条 MOCK_RECENT 假告警（Dashboard.tsx:100）。
  const recentQ = useApiQuery<Alert[]>(
    ['dashboard', 'recent-alerts'],
    async () => {
      const res: any = await alertApi.list({ limit: RECENT_ALERT_LIMIT })
      const items = res?.data?.data?.items
      return Array.isArray(items) ? items : []
    },
    { staleTime: 60_000 },
  )

  const trends = trendsQ.data ?? []
  // 趋势点不足 6 个时不做「后 3 天 vs 前 3 天」对比：传 undefined 让角标不渲染，
  // 而不是造一个 5（修前 `: 5` 是虚构趋势）。
  const delta =
    trends.length >= 6
      ? trends.slice(-3).reduce((s, p) => s + p.count, 0) -
        trends.slice(-6, -3).reduce((s, p) => s + p.count, 0)
      : undefined
  const recent = recentQ.data ?? []

  return (
    <div>
      <PageHeader title="仪表盘" subtitle="网络运维平台核心指标速览" />

      {kpiQ.isError ? (
        <ErrorState error={kpiQ.error} onRetry={kpiQ.refetch} compact />
      ) : (
        <KpiCards kpi={kpiQ.data ?? null} loading={kpiQ.isLoading} />
      )}

      {statsQ.isError ? (
        <ErrorState error={statsQ.error} onRetry={statsQ.refetch} />
      ) : statsQ.data ? (
        <DashboardCards
          stats={statsQ.data}
          loading={statsQ.isLoading}
          alertTrendDelta={delta}
        />
      ) : statsQ.isLoading ? (
        <DashboardCards stats={EMPTY_STATS} loading />
      ) : (
        <EmptyState
          title="暂无统计数据"
          description="后端尚未返回资产与告警汇总"
          compact
        />
      )}

      <Row gutter={16} style={{ marginTop: 16 }}>
        <Col span={16}>
          {trendsQ.isError ? (
            <Card title="告警趋势">
              <ErrorState
                error={trendsQ.error}
                onRetry={trendsQ.refetch}
                compact
              />
            </Card>
          ) : (
            <AlertTrendChart data={trends} />
          )}
        </Col>
        <Col span={8}>
          <Card title="最近告警" style={{ height: 380 }}>
            {recentQ.isError ? (
              <ErrorState
                error={recentQ.error}
                onRetry={recentQ.refetch}
                compact
              />
            ) : recentQ.isLoading ? (
              <LoadingSkeleton variant="list" rows={4} />
            ) : recent.length === 0 ? (
              <EmptyState preset="no-alerts" compact />
            ) : (
              <List
                dataSource={recent}
                renderItem={(a) => (
                  <List.Item>
                    <List.Item.Meta
                      title={a.host}
                      description={
                        <div>
                          <span>{a.message}</span>
                          <div>
                            <SeverityTag
                              severity={a.severity}
                              label={a.severity_name}
                            />
                            <Typography.Text
                              type="secondary"
                              style={{ fontSize: 12, marginLeft: 8 }}
                            >
                              {formatRelativeTime(a.created_at)}
                            </Typography.Text>
                          </div>
                        </div>
                      }
                    />
                  </List.Item>
                )}
              />
            )}
          </Card>
        </Col>
      </Row>
    </div>
  )
}

export default Dashboard
