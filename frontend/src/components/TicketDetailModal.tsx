import { Button, Col, Modal, Row } from 'antd'
import { StatusTag } from './StatusTag'
import { formatDateTime } from '../utils/time'
import type { Ticket } from './TicketTable'
import { TicketHistoryTimeline } from './TicketHistoryTimeline'

const PRIORITY_LABEL: Record<string, string> = {
  critical: '紧急',
  high: '高',
  normal: '普通',
  medium: '普通', // M16 遗留同义词安全网（写入方已归一为 normal），见 TicketTable.tsx 同名字典注释
  low: '低',
}

const STATUS_LABEL: Record<string, string> = {
  open: '新建',
  in_progress: '处理中',
  pending: '等待中',
  resolved: '已解决',
  closed: '关闭',
}

export interface TicketDetailModalProps {
  ticket: Ticket | null
  onClose: () => void
}

export function TicketDetailModal({ ticket, onClose }: TicketDetailModalProps) {
  return (
    <Modal
      title="工单详情"
      open={!!ticket}
      onCancel={onClose}
      footer={[<Button key="close" onClick={onClose}>关闭</Button>]}
      width={600}
      destroyOnClose
    >
      {ticket && (
        <>
          <Row gutter={16} style={{ marginBottom: 16 }}>
            <Col span={12}>
              <strong>工单标题：</strong>
              {ticket.title}
            </Col>
            <Col span={12}>
              <strong>工单ID：</strong>
              {ticket.id}
            </Col>
          </Row>
          <Row gutter={16} style={{ marginBottom: 16 }}>
            <Col span={12}>
              <strong>优先级：</strong>
              <StatusTag value={ticket.priority} label={PRIORITY_LABEL[ticket.priority] || ticket.priority} />
            </Col>
            <Col span={12}>
              <strong>状态：</strong>
              <StatusTag value={ticket.status} label={STATUS_LABEL[ticket.status] || ticket.status} />
            </Col>
          </Row>
          <Row gutter={16} style={{ marginBottom: 16 }}>
            <Col span={12}>
              <strong>请求人：</strong>
              {ticket.requester}
            </Col>
            <Col span={12}>
              <strong>处理人：</strong>
              {ticket.assignee || '-'}
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <strong>创建时间：</strong>
              {/* W2：与列表页统一走 utils/time 出口 */}
              {formatDateTime(ticket.created_at)}
            </Col>
            <Col span={12}>
              <strong>更新时间：</strong>
              {formatDateTime(ticket.updated_at)}
            </Col>
          </Row>

          {/* M25 经手记录。key=ticket.id：换一张票就重挂，省得把上一张的行带过来
              （弹窗 destroyOnClose 只在关闭时销毁，父组件直接换 ticket 时不会重挂）。 */}
          <div style={{ marginTop: 8 }}>
            <strong>经手记录</strong>
            <div style={{ marginTop: 8 }}>
              <TicketHistoryTimeline key={ticket.id} ticketId={ticket.id} />
            </div>
          </div>
        </>
      )}
    </Modal>
  )
}

export default TicketDetailModal
