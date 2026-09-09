// P2（性能审计）：echarts 从整包改成 `echarts/core` + 显式注册后，**漏注册模块
// 不会报错，只会静默白屏**（实测：未注册的 series 类型 setOption 不抛错，
// getOption() 还会原样回显你传进去的 option —— 所以不能用 getOption 断言）。
//
// 真正能区分「已注册 / 未注册」的是 series 模型：未注册时
// `getModel().getSeriesByIndex(0)` 为 undefined。这条用例靠副作用导入组件模块，
// 触发它顶层的 `echarts.use([...])`，再用 SSR + svg 渲染器（jsdom 没有 canvas）
// 验证 line 系列真的可用。把 LineChart 从 AlertTrendChart.tsx 的 use([...]) 里
// 删掉，本用例必红。
import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import * as echarts from 'echarts/core'
import { SVGRenderer } from 'echarts/renderers'
// 副作用导入：触发被测组件模块顶层的 echarts.use([...])
import { AlertTrendChart } from './AlertTrendChart'

// 测试侧自带 svg 渲染器（SSR 模式，避开 jsdom 没有 canvas 的限制）
echarts.use([SVGRenderer])

function seriesSubType(type: string): string | undefined {
  const inst = echarts.init(document.createElement('div'), undefined, {
    ssr: true,
    renderer: 'svg',
    width: 400,
    height: 300,
  })
  inst.setOption({
    xAxis: { type: 'category', data: ['a', 'b'] },
    yAxis: { type: 'value' },
    series: [{ type, data: [1, 2] }],
  })
  const subType = (
    inst as unknown as {
      getModel: () => { getSeriesByIndex: (i: number) => { subType?: string } }
    }
  )
    .getModel()
    .getSeriesByIndex(0)?.subType
  inst.dispose()
  return subType
}

describe('AlertTrendChart', () => {
  it('组件模块已注册折线图系列（漏注册 → 静默白屏）', () => {
    expect(seriesSubType('line')).toBe('line')
  })

  it('未注册的系列类型确实取不到模型（证明上面的断言有区分力）', () => {
    expect(seriesSubType('bogus-chart-xyz')).toBeUndefined()
  })

  it('渲染出 ECharts 容器（core 版包装器接线正确）', () => {
    const { container } = render(
      <AlertTrendChart
        data={[
          { date: '2026-09-08', count: 3 },
          { date: '2026-09-09', count: 7 },
        ]}
      />,
    )
    // echarts.init 成功后会在容器上挂 _echarts_instance_
    expect(container.querySelector('div[_echarts_instance_]')).not.toBeNull()
  })
})
