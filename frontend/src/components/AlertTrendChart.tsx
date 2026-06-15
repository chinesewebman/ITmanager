import { Card } from 'antd'
import ReactECharts from 'echarts-for-react'

export interface AlertTrend {
  date: string
  count: number
}

export interface AlertTrendChartProps {
  data: AlertTrend[]
  height?: number
}

/**
 * AlertTrendChart - 告警趋势 ECharts 折线图。
 */
export function AlertTrendChart({ data, height = 300 }: AlertTrendChartProps) {
  const option = {
    title: { text: '告警趋势', left: 'center' },
    tooltip: { trigger: 'axis' as const },
    xAxis: { type: 'category' as const, data: data.map((t) => t.date) },
    yAxis: { type: 'value' as const },
    series: [
      {
        data: data.map((t) => t.count),
        type: 'line' as const,
        smooth: true,
        areaStyle: { opacity: 0.3 },
        itemStyle: { color: '#1890ff' },
      },
    ],
  }
  return (
    <Card title="告警趋势">
      <ReactECharts option={option} style={{ height }} />
    </Card>
  )
}

export default AlertTrendChart
