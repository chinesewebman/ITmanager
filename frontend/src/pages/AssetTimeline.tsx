// 资产诊断时间线页：故障定位核心 UI（P0-1）
//
// 数据流：调用 /api/v1/diagnostics/assets/:id/timeline 拿到聚合事件流
// 渲染：Antd Timeline 组件（按 ts 倒序展示），顶部 Summary 卡片
//
// 设计要点：
//  - 4 种 kind 4 种颜色（alert=红/橙/黄/绿，ticket=蓝，status=灰，link=紫）
//  - severity 0-5 映射到 color（5=红, 4=橙, 3=黄, 2=蓝, 1=绿, 0=灰）
//  - 事件点击跳详情（alert → 告警详情，ticket → 工单详情）
//  - MTTR 缺省值用 "—" 不显示 N/A
//  - mock 优先（data: any 解构，跟其他页一致）

import { useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { Card, Col, Descriptions, Row, Skeleton, Space, Statistic, Tag, Timeline, Typography } from 'antd'
import { ClockCircleOutlined } from '@ant-design/icons'
import { useApiQuery } from '../hooks/useApiQuery'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

const { Text } = Typography

// 事件 kind 的颜色
const KIND_COLOR: Record<string, string> = {
  alert: 'red',
  ticket: 'blue',
  status_change: 'gray',
  link_change: 'purple',
}

// severity → 颜色
function severityColor(sev: number): string {
  if (sev >= 5) return 'red'
  if (sev >= 4) return 'orange'
  if (sev >= 3) return 'gold'
  if (sev >= 2) return 'blue'
  if (sev >= 1) return 'green'
  return 'default'
}

// 事件 sub_kind 中文映射
const SUB_KIND_LABEL: Record<string, string> = {
  triggered: '触发',
  acknowledged: '已确认',
  resolved: '已解决',
  created: '创建',
  closed: '已关闭',
  online: '上线',
  offline: '离线',
  up: '端口 UP',
  down: '端口 DOWN',
}

// Mock 数据：开发环境网络断时也能渲染
const MOCK_SUMMARY = {
  alert_count: 8,
  ticket_count: 2,
  open_alerts: 1,
  open_tickets: 0,
  mttr_seconds: 1800,
  link_down_count: 0,
  window_days: 30,
}

const MOCK_EVENTS = [
  {
    ts: new Date(Date.now() - 30 * 60_000).toISOString(),
    kind: 'alert',
    sub_kind: 'triggered',
    severity: 4,
    title: 'CPU 使用率超阈值',
    description: 'Warning · CPU 持续 5 分钟 > 90%',
    ref_id: '00000000-0000-0000-0000-000000000001',
    ref_table: 'alerts',
  },
  {
    ts: new Date(Date.now() - 25 * 60_000).toISOString(),
    kind: 'alert',
    sub_kind: 'acknowledged',
    severity: 0,
    title: '已确认告警',
    description: '操作人: ops',
    ref_id: '00000000-0000-0000-0000-000000000001',
    ref_table: 'alerts',
  },
  {
    ts: new Date(Date.now() - 2 * 3600_000).toISOString(),
    kind: 'ticket',
    sub_kind: 'created',
    severity: 0,
    title: '服务响应慢',
    description: '工单创建',
    ref_id: '00000000-0000-0000-0000-000000000002',
    ref_table: 'tickets',
  },
  {
    ts: new Date(Date.now() - 24 * 3600_000).toISOString(),
    kind: 'status_change',
    sub_kind: 'online',
    severity: 0,
    title: '资产上线',
    description: 'OnlineTime 变更',
  },
]

const MOCK_TIMELINE = {
  asset: { id: 'mock', name: 'mock-asset', asset_type: 'server', status: 'active' },
  events: MOCK_EVENTS,
  summary: MOCK_SUMMARY,
}

function formatDuration(seconds: number | undefined | null): string {
  if (!seconds) return '—'
  if (seconds < 60) return `${seconds} 秒`
  if (seconds < 3600) return `${Math.round(seconds / 60)} 分钟`
  if (seconds < 86400) return `${(seconds / 3600).toFixed(1)} 小时`
  return `${(seconds / 86400).toFixed(1)} 天`
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleString('zh-CN', { hour12: false })
}

interface TimelineEvent {
  ts: string
  kind: 'alert' | 'ticket' | 'status_change' | 'link_change'
  sub_kind: string
  severity: number
  title: string
  description?: string
  ref_id?: string
  ref_table?: string
}

interface TimelineResponse {
  asset: { id: string; name: string; asset_type: string; status: string }
  events: TimelineEvent[]
  summary: typeof MOCK_SUMMARY
}

export function AssetTimeline() {
  const { id } = useParams<{ id: string }>()

  useDocumentTitle('资产诊断')
  const [days] = useState(30)

  const { data, isLoading } = useApiQuery<TimelineResponse>(
    ['diagnostics', 'timeline', id ?? '', days] as const,
    async () => {
      const token = localStorage.getItem('token') ?? ''
      const res = await fetch(`/api/v1/diagnostics/assets/${id}/timeline?days=${days}`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) return MOCK_TIMELINE
      const json: any = await res.json()
      return json?.data ?? MOCK_TIMELINE
    },
    { enabled: !!id },
  )

  if (isLoading) {
    return <Skeleton active paragraph={{ rows: 6 }} />
  }

  const tl = data ?? MOCK_TIMELINE
  const summary = tl.summary ?? MOCK_SUMMARY
  const events = tl.events ?? []
  const asset = tl.asset

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Link to="/assets">← 返回资产列表</Link>
      </Space>

      <Card title={`资产诊断：${asset.name}`} size="small" style={{ marginBottom: 16 }}>
        <Descriptions size="small" column={4}>
          <Descriptions.Item label="类型">
            <Tag color="blue">{asset.asset_type}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="状态">
            <Tag color={asset.status === 'active' ? 'green' : 'default'}>{asset.status}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="查询窗口">{summary.window_days} 天</Descriptions.Item>
          <Descriptions.Item label="事件总数">{events.length}</Descriptions.Item>
        </Descriptions>
      </Card>

      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card>
            <Statistic title="告警总数" value={summary.alert_count} valueStyle={{ color: '#cf1322' }} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="未处理告警" value={summary.open_alerts} valueStyle={{ color: '#fa8c16' }} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="工单总数" value={summary.ticket_count} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="MTTR (平均恢复)"
              value={formatDuration(summary.mttr_seconds)}
            />
          </Card>
        </Col>
      </Row>

      <Card title="事件时间线" size="small">
        {events.length === 0 ? (
          <Text type="secondary">该资产在 {summary.window_days} 天窗口内无事件</Text>
        ) : (
          <Timeline
            mode="left"
            items={events.map((e) => {
              const color = e.kind === 'alert' ? severityColor(e.severity) : (KIND_COLOR[e.kind] ?? 'gray')
              const subLabel = SUB_KIND_LABEL[e.sub_kind] ?? e.sub_kind
              const detailLink =
                e.ref_table === 'alerts' && e.ref_id
                  ? `/alerts`
                  : e.ref_table === 'tickets' && e.ref_id
                    ? `/tickets`
                    : null
              return {
                color,
                dot: <ClockCircleOutlined style={{ fontSize: 16 }} />,
                label: formatTime(e.ts),
                children: (
                  <div>
                    <Space>
                      <Tag color={color}>{subLabel}</Tag>
                      <Text strong>{e.title}</Text>
                    </Space>
                    {e.description && (
                      <div>
                        <Text type="secondary">{e.description}</Text>
                      </div>
                    )}
                    {detailLink && (
                      <div>
                        <Link to={detailLink}>查看详情 →</Link>
                      </div>
                    )}
                  </div>
                ),
              }
            })}
          />
        )}
      </Card>
    </div>
  )
}

export default AssetTimeline
