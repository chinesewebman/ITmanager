import { Card } from 'antd'
// P2（性能审计）：`echarts-for-react` 默认入口会整包引入 echarts（占 Dashboard
// chunk 98%，实测 −556KB raw / −182KB gzip）。这里改用 echarts/core + 显式注册。
//
// ⚠️ 新增图表类型（柱状图 / 饼图 / 散点…）或新组件（DataZoom / Legend…）时，
// 必须在本文件的 echarts.use([...]) 里补上对应模块，否则**图表静默空白、不报错**。
import ReactEChartsCore from 'echarts-for-react/lib/core'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import {
  GridComponent,
  TitleComponent,
  TooltipComponent,
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'

echarts.use([
  LineChart,
  GridComponent,
  TitleComponent,
  TooltipComponent,
  CanvasRenderer,
])

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
      <ReactEChartsCore
        echarts={echarts}
        option={option}
        style={{ height }}
      />
    </Card>
  )
}

export default AlertTrendChart
