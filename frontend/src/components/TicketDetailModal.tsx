import { Button, Col, Modal, Row } from 'antd'
import { StatusTag } from './StatusTag'
import type { Ticket } from './TicketTable'

const PRIORITY_LABEL: Record<string, string> = {
  critical: '紧急',
  high: '高',
  normal: '普通',
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
              {ticket.created_at}
            </Col>
            <Col span={12}>
              <strong>更新时间：</strong>
              {ticket.updated_at}
            </Col>
          </Row>
        </>
      )}
    </Modal>
  )
}

export default TicketDetailModal
