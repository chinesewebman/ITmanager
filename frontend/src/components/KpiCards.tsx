import { Card, Col, Row, Statistic, Tag, Tooltip } from 'antd'
import {
  ClockCircleOutlined,
  EyeOutlined,
  AlertOutlined,
  CheckCircleOutlined,
  InfoCircleOutlined,
} from '@ant-design/icons'

export interface KPI {
  mttr_seconds?: number | null
  mttd_seconds?: number | null
  alert_density: number
  sla_closed_rate?: number | null
  window_days: number
  resolved_alerts?: number
  acked_alerts?: number
  closed_tickets?: number
  on_time_tickets?: number
}

export interface KpiCardsProps {
  kpi: KPI | null
  loading?: boolean
}

/**
 * KPI 阈值常量 (v1.1 可配置化)
 *
 * 当前值基于 IT 运维经验：
 *   - MTTR > 1h 视为恢复慢
 *   - MTTD > 10min 视为检测慢（告警延迟高）
 *   - 告警密度 > 5/day 视为告警风暴
 *   - SLA 达成率 < 90% 视为未达标
 *
 * 如需调整建议从环境变量/配置文件注入，避免散落硬编码。
 */
export const KPI_THRESHOLDS = {
  MTTR_RED_SEC: 3600, // 1h
  MTTD_RED_SEC: 600, // 10min
  ALERT_DENSITY_RED: 5, // alerts/day
  SLA_TARGET: 0.9, // 90%
} as const

/**
 * KpiCards - 关键 KPI 指标卡片
 *
 * 4 大指标:
 *   - MTTR (平均恢复时间)
 *   - MTTD (平均检测时间)
 *   - 告警密度 (alerts/day)
 *   - SLA 达成率
 *
 * 字段为 null → 显示 n/a，不传 0 假装有数据
 */
export function KpiCards({ kpi, loading }: KpiCardsProps) {
  if (!kpi) {
    return (
      <Card loading={loading} style={{ marginBottom: 16 }}>
        <span style={{ color: '#999' }}>无 KPI 数据</span>
      </Card>
    )
  }

  return (
    <Card
      title={
        <span>
          <InfoCircleOutlined style={{ marginRight: 8 }} />
          关键 KPI（最近 {kpi.window_days} 天）
        </span>
      }
      style={{ marginBottom: 16 }}
      size="small"
    >
      <Row gutter={16}>
        <Col xs={12} sm={12} md={6}>
          <Tooltip title="Mean Time To Recover：告警从触发到恢复的平均耗时">
            <Statistic
              title="MTTR (平均恢复)"
              value={kpi.mttr_seconds != null ? formatDuration(kpi.mttr_seconds) : 'n/a'}
              prefix={<ClockCircleOutlined />}
              valueStyle={{
                color: kpi.mttr_seconds != null && kpi.mttr_seconds > KPI_THRESHOLDS.MTTR_RED_SEC ? '#cf1322' : '#3f8600',
              }}
            />
          </Tooltip>
        </Col>
        <Col xs={12} sm={12} md={6}>
          <Tooltip title="Mean Time To Detect：告警从触发到确认的平均耗时">
            <Statistic
              title="MTTD (平均检测)"
              value={kpi.mttd_seconds != null ? formatDuration(kpi.mttd_seconds) : 'n/a'}
              prefix={<EyeOutlined />}
              valueStyle={{
                color: kpi.mttd_seconds != null && kpi.mttd_seconds > KPI_THRESHOLDS.MTTD_RED_SEC ? '#cf1322' : '#3f8600',
              }}
            />
          </Tooltip>
        </Col>
        <Col xs={12} sm={12} md={6}>
          <Tooltip title="窗口内每日平均告警数（alerts/day）">
            <Statistic
              title="告警密度"
              value={kpi.alert_density > 0 ? kpi.alert_density.toFixed(1) : '0'}
              suffix={kpi.alert_density > 0 ? 'alerts/day' : ''}
              prefix={<AlertOutlined />}
              valueStyle={{ color: kpi.alert_density > KPI_THRESHOLDS.ALERT_DENSITY_RED ? '#cf1322' : '#3f8600' }}
            />
          </Tooltip>
        </Col>
        <Col xs={12} sm={12} md={6}>
          <Tooltip
            title={
              kpi.sla_closed_rate != null
                ? `已关工单 ${kpi.closed_tickets} 中按时 ${kpi.on_time_tickets}`
                : '无已关工单数据'
            }
          >
            <Statistic
              title="SLA 达成率"
              value={kpi.sla_closed_rate != null ? `${(kpi.sla_closed_rate * 100).toFixed(1)}%` : 'n/a'}
              prefix={<CheckCircleOutlined />}
              valueStyle={{
                color:
                  kpi.sla_closed_rate != null && kpi.sla_closed_rate < KPI_THRESHOLDS.SLA_TARGET ? '#cf1322' : '#3f8600',
              }}
              suffix={
                kpi.sla_closed_rate != null && kpi.sla_closed_rate < KPI_THRESHOLDS.SLA_TARGET ? (
                  <Tag color="red" style={{ marginLeft: 8 }}>未达标</Tag>
                ) : null
              }
            />
          </Tooltip>
        </Col>
      </Row>
    </Card>
  )
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h${Math.floor((seconds % 3600) / 60)}m`
  return `${Math.floor(seconds / 86400)}d${Math.floor((seconds % 86400) / 3600)}h`
}

export default KpiCards
