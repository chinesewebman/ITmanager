// P4：AlertTable 未 memo + columns 每渲染重建 → 父组件无关 state 变更整表重渲染。
// 修法：React.memo + columns useMemo + 父组件回调 useCallback。
//
// 验证策略与 AssetTable.memo.test.tsx 一致：不用 <Profiler>（React 18 下 memo bailout 时
// onRender 仍触发 update，无法区分 memo/non-memo——已实验证实）。改为 mock antd Table，
// 在 Table 函数体内计数：memo 生效时父组件重渲染 bail out AlertTable 函数体，Table 不被
// 再次调用；去 memo 后 AlertTable 每次重渲染都重新执行 return <Table/>，Table 被再次调用。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, fireEvent } from '@testing-library/react'
import { useState, useCallback } from 'react'
import { AlertTable, type Alert } from './AlertTable'

// data 用模块级常量保持引用稳定（内联 [] 每次渲染都是新引用，会让 memo 失效、测试失真）
const EMPTY: Alert[] = []

// mock antd Table 为轻量 div（保留其余 antd 导出），并在函数体内计数 Table 被调用的次数
const mockState = vi.hoisted(() => ({ tableCalls: 0 }))

vi.mock('antd', async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>()
  return {
    ...actual,
    Table: () => {
      mockState.tableCalls++
      return <div data-testid="mock-table" />
    },
  }
})

describe('AlertTable memo (P4)', () => {
  beforeEach(() => {
    mockState.tableCalls = 0
  })

  it('P4：无关 state 变化不触发 AlertTable 重渲染（memo 拦截 → Table 不被再次调用）', () => {
    // 模拟 Alerts.tsx：回调都 useCallback 稳定引用，data 用模块级常量
    function Parent() {
      const [, setCount] = useState(0)
      const onAck = useCallback(() => {}, [])
      const onResolve = useCallback(() => {}, [])
      const onMarkFP = useCallback(() => {}, [])
      return (
        <div>
          <button onClick={() => setCount((c) => c + 1)}>inc</button>
          <AlertTable
            data={EMPTY}
            loading={false}
            onAck={onAck}
            onResolve={onResolve}
            onMarkFP={onMarkFP}
          />
        </div>
      )
    }

    const { getByText } = render(<Parent />)
    const afterMount = mockState.tableCalls
    expect(afterMount).toBe(1)

    // 无关 state（count）变化 → Parent 重渲染，但 AlertTable props 引用全部稳定 → memo 拦截，
    // 函数体不再执行 → Table 不被再次调用（计数不变）。
    fireEvent.click(getByText('inc'))

    expect(mockState.tableCalls).toBe(afterMount)
  })
})
