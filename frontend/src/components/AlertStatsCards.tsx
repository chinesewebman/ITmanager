import { Card, Col, Row } from 'antd'

export interface AlertStats {
  total: number
  problem: number
  acknowledged: number
  resolved: number
}

export interface AlertStatsCardsProps {
  stats: AlertStats
  loading?: boolean
}

interface StatCard {
  key: keyof AlertStats
  label: string
  color: string
}

const CARDS: StatCard[] = [
  { key: 'total', label: '总告警', color: '#1890ff' },
  { key: 'problem', label: '未处理', color: '#ff4d4f' },
  { key: 'acknowledged', label: '已确认', color: '#faad14' },
  { key: 'resolved', label: '已解决', color: '#52c41a' },
]

/**
 * AlertStatsCards - 告警状态统计 4 联。
 */
export function AlertStatsCards({ stats, loading }: AlertStatsCardsProps) {
  return (
    <Row gutter={16} style={{ marginBottom: 16 }}>
      {CARDS.map((c) => (
        <Col span={6} key={c.key}>
          <Card loading={loading}>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 24, fontWeight: 'bold', color: c.color }}>{stats[c.key]}</div>
              <div style={{ color: '#999' }}>{c.label}</div>
            </div>
          </Card>
        </Col>
      ))}
    </Row>
  )
}

export default AlertStatsCards
