import { Card, Col, Row, Statistic } from 'antd'
import {
  AlertOutlined,
  ArrowDownOutlined,
  BuildOutlined,
  DesktopOutlined,
  HomeOutlined,
} from '@ant-design/icons'

export interface DashboardCardsStats {
  assets: number
  alerts: number
  tickets: number
  sites: number
  machines: number
  networks: number
}

export interface DashboardCardsProps {
  stats: DashboardCardsStats
  loading?: boolean
  alertTrendDelta?: number
}

interface CardSpec {
  key: keyof DashboardCardsStats
  title: string
  icon: React.ReactNode
  color: string
  showDelta?: boolean
}

const CARDS: CardSpec[] = [
  { key: 'assets', title: '资产总数', icon: <DesktopOutlined />, color: '#1890ff' },
  { key: 'alerts', title: '活跃告警', icon: <AlertOutlined />, color: '#ff4d4f', showDelta: true },
  { key: 'tickets', title: '待处理工单', icon: <BuildOutlined />, color: '#faad14' },
  { key: 'sites', title: '机房数量', icon: <HomeOutlined />, color: '#52c41a' },
]

export function DashboardCards({ stats, loading, alertTrendDelta }: DashboardCardsProps) {
  return (
    <Row gutter={16}>
      {CARDS.map((c) => (
        <Col span={6} key={c.key}>
          <Card loading={loading}>
            <Statistic
              title={c.title}
              value={stats[c.key]}
              prefix={c.icon}
              valueStyle={{ color: c.color }}
              suffix={
                c.showDelta && alertTrendDelta !== undefined ? (
                  <span style={{ fontSize: 14, color: '#52c41a', marginLeft: 8 }}>
                    <ArrowDownOutlined /> {Math.abs(alertTrendDelta)}
                  </span>
                ) : null
              }
            />
          </Card>
        </Col>
      ))}
    </Row>
  )
}

export default DashboardCards
