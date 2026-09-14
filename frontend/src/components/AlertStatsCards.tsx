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
  // M55: 统计卡可点击跳转 (C1 摩擦). 父传 key 决定跳哪; 不传则保持原状只读.
  onCardClick?: (key: keyof AlertStats) => void
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
 * M55: 可选 onCardClick; 不传则纯展示, 传了 hover + cursor pointer + 跳转.
 */
export function AlertStatsCards({ stats, loading, onCardClick }: AlertStatsCardsProps) {
  return (
    <Row gutter={16} style={{ marginBottom: 16 }}>
      {CARDS.map((c) => (
        <Col span={6} key={c.key}>
          <Card
            loading={loading}
            hoverable={!!onCardClick}
            onClick={onCardClick ? () => onCardClick(c.key) : undefined}
            style={{ cursor: onCardClick ? 'pointer' : 'default' }}
          >
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
