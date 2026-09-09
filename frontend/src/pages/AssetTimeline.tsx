// 资产诊断时间线页：故障定位核心 UI（P0-1）
//
// 数据流：调用 /api/diagnostics/assets/:id/timeline 拿到聚合事件流
// 渲染：Antd Timeline 组件（按 ts 倒序展示），顶部 Summary 卡片
//
// 设计要点：
//  - 4 种 kind 4 种颜色（alert=红/橙/黄/绿，ticket=蓝，status=灰，link=紫）
//  - severity 0-5 映射到 color（5=红, 4=橙, 3=黄, 2=蓝, 1=绿, 0=灰）
//  - 事件点击跳详情（alert → 告警详情，ticket → 工单详情）
//  - MTTR 缺省值用 "—" 不显示 N/A
//
// W1：`:153` `?? MOCK_TIMELINE` + `:155` `catch { return MOCK_TIMELINE }` 与渲染层
// `:165/:166` `data ?? MOCK_TIMELINE` / `tl.summary ?? MOCK_SUMMARY` 三重兜底已删除。
// 原写法 isError 恒 false —— 接口挂了照常画出 4 条虚构事件（含「CPU 使用率超阈值」），
// 运维会当真实故障历史排查；新增 normalizeTimeline() 兜「200 + 形状异常」。
// W2：`:122` formatTime 用 toLocaleString('zh-CN')，口径与其它页不一致，改 formatDateTime。

import { useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { Card, Col, Descriptions, Row, Skeleton, Space, Statistic, Tag, Timeline, Typography } from 'antd'
import { ClockCircleOutlined } from '@ant-design/icons'
import api from '../services/api'
import { useApiQuery } from '../hooks/useApiQuery'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { formatDateTime } from '../utils/time'
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

// 无数据兜底用全 0 结构（非假数据）：只出现在「200 + shape 异常」，正常后端总带全字段
const EMPTY_SUMMARY: TimelineSummary = {
  alert_count: 0,
  ticket_count: 0,
  open_alerts: 0,
  open_tickets: 0,
  mttr_seconds: null,
  link_down_count: 0,
  window_days: 0,
}

function formatDuration(seconds: number | undefined | null): string {
  if (!seconds) return '—'
  if (seconds < 60) return `${seconds} 秒`
  if (seconds < 3600) return `${Math.round(seconds / 60)} 分钟`
  if (seconds < 86400) return `${(seconds / 3600).toFixed(1)} 小时`
  return `${(seconds / 86400).toFixed(1)} 天`
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

interface TimelineSummary {
  alert_count: number
  ticket_count: number
  open_alerts: number
  open_tickets: number
  mttr_seconds?: number | null
  link_down_count: number
  window_days: number
}

interface TimelineAsset {
  id: string
  name: string
  asset_type: string
  status: string
}

interface TimelineResponse {
  asset: TimelineAsset | null
  events: TimelineEvent[]
  summary: TimelineSummary
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === 'object' && !Array.isArray(v)
}

/**
 * 接口形状归一：asset/summary 非对象、events 非数组一律降级不崩。
 * asset 缺失返回 null（走「资产不存在」空态）；summary 缺字段回落全 0（mttr → '—'）。
 */
function normalizeTimeline(v: unknown): TimelineResponse {
  const t = (v ?? {}) as Partial<TimelineResponse>
  return {
    asset: isRecord(t.asset) ? (t.asset as unknown as TimelineAsset) : null,
    events: Array.isArray(t.events) ? t.events : [],
    summary: { ...EMPTY_SUMMARY, ...(isRecord(t.summary) ? t.summary : {}) } as TimelineSummary,
  }
}

export function AssetTimeline() {
  const { id } = useParams<{ id: string }>()

  useDocumentTitle('资产诊断')
  const [days] = useState(30)

  const { data, isLoading, isError, error, refetch } = useApiQuery<TimelineResponse>(
    ['diagnostics', 'timeline', id ?? '', days] as const,
    async () => {
      const res = await api.get(`/diagnostics/assets/${id}/timeline`, { params: { days } })
      return normalizeTimeline(res.data?.data)
    },
    { enabled: !!id },
  )

  if (isLoading) {
    return <Skeleton active paragraph={{ rows: 6 }} />
  }

  const { asset, summary, events } = normalizeTimeline(data)

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Link to="/assets">← 返回资产列表</Link>
      </Space>

      {isError ? (
        <ErrorState error={error} onRetry={refetch} />
      ) : !asset ? (
        <Card title="资产诊断" size="small">
          <EmptyState title="资产不存在" description="该资产不存在或已被删除" />
        </Card>
      ) : (
        <>
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
                    label: formatDateTime(e.ts),
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
        </>
      )}
    </div>
  )
}

export default AssetTimeline
