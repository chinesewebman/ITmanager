import { Card, Col, Row } from 'antd'

export interface TicketStats {
  pending: number
  inProgress: number
  waiting: number
  resolved: number
}

export interface TicketStatsCardsProps {
  stats: TicketStats
  loading?: boolean
}

interface StatCard {
  label: string
  value: number
  color: string
}

const DEFAULT_STATS: StatCard[] = [
  { label: '待处理', value: 0, color: '#ff4d4f' },
  { label: '处理中', value: 0, color: '#1890ff' },
  { label: '等待中', value: 0, color: '#faad14' },
  { label: '已解决', value: 0, color: '#52c41a' },
]

/**
 * TicketStatsCards - 工单状态统计卡（4 联）。
 */
export function TicketStatsCards({ stats, loading }: TicketStatsCardsProps) {
  const values = [stats.pending, stats.inProgress, stats.waiting, stats.resolved]
  return (
    <Row gutter={16} style={{ marginBottom: 16 }}>
      {DEFAULT_STATS.map((card, i) => (
        <Col span={6} key={card.label}>
          <Card loading={loading}>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 24, fontWeight: 'bold', color: card.color }}>{values[i]}</div>
              <div style={{ color: '#999' }}>{card.label}</div>
            </div>
          </Card>
        </Col>
      ))}
    </Row>
  )
}

export default TicketStatsCards
