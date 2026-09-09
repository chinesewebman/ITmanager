import { Card, Col, Row } from 'antd'

// FIX-PLAN-UI-PERF §1.3-7②（修正）：档位取**契约**域，不取 models 注释。
// `openapi.yaml:2371` 的 status enum = [open, in_progress, pending, resolved, closed]，
// 且 `integration/glpi.go:157` 会把 GLPI 状态 3 映射成 `pending` —— models/ticket.go:18
// 的注释「open, in_progress, resolved, closed」漏了 pending。若按注释做四档，
// GLPI 同步来的待定工单会被统计卡静默漏掉。
export interface TicketStats {
  open: number
  in_progress: number
  pending: number
  resolved: number
  closed: number
}

export interface TicketStatsCardsProps {
  stats: TicketStats
  loading?: boolean
}

interface StatCard {
  key: keyof TicketStats
  label: string
  color: string
}

const CARDS: StatCard[] = [
  { key: 'open', label: '待处理', color: '#ff4d4f' },
  { key: 'in_progress', label: '处理中', color: '#1890ff' },
  { key: 'pending', label: '待定', color: '#faad14' },
  { key: 'resolved', label: '已解决', color: '#52c41a' },
  { key: 'closed', label: '已关闭', color: '#8c8c8c' },
]

/**
 * TicketStatsCards - 工单状态统计卡（5 联，按契约状态域）。
 */
export function TicketStatsCards({ stats, loading }: TicketStatsCardsProps) {
  return (
    <Row gutter={16} style={{ marginBottom: 16 }}>
      {CARDS.map((card) => (
        <Col flex="1" key={card.key}>
          <Card loading={loading}>
            <div style={{ textAlign: 'center' }}>
              {/* 守卫：stats 缺字段（200 + 空 data）时显示 0，不 TypeError 白屏 */}
              <div style={{ fontSize: 24, fontWeight: 'bold', color: card.color }}>
                {stats?.[card.key] ?? 0}
              </div>
              <div style={{ color: '#999' }}>{card.label}</div>
            </div>
          </Card>
        </Col>
      ))}
    </Row>
  )
}

export default TicketStatsCards
