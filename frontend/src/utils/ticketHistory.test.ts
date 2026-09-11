// M25 步骤 6：历史时间线的展示层纯函数（docs/FIX-PLAN-TICKET-HISTORY.md §2.8）。
//
// 这里守的都是「不写下来就会被当成小事」的判断：组内顺序必须确定（同批次各行
// created_at 相同，次序键落在随机 uuid 上）、NULL 与空串必须显示成两个不同的东西、
// 字典查不到的列必须原样露出来而不是藏起来。
import { describe, it, expect } from 'vitest'
import { formatHistoryValue, fieldLabel, groupTicketHistory } from './ticketHistory'
import type { TicketHistory } from '../types'

function row(over: Partial<TicketHistory>): TicketHistory {
  return {
    id: Math.random().toString(36).slice(2),
    ticket_id: 't1',
    batch_id: 'b1',
    kind: 'updated',
    field_name: 'status',
    old_value: 'open',
    new_value: 'resolved',
    actor_id: 'a1',
    actor_name: '燕如',
    source: '',
    request_id: '',
    created_at: '2026-09-11T10:00:00',
    ...over,
  }
}

describe('groupTicketHistory', () => {
  it('同一次操作的多行归为一组，且组内按 field_name 升序（与写入侧同序）', () => {
    // 输入故意乱序：读端点按 created_at DESC, id DESC 排，同批次各行时间相同 →
    // 组内顺序实际由随机 uuid 决定，不重排的话每次刷新顺序都在变。
    const groups = groupTicketHistory([
      row({ batch_id: 'b1', field_name: 'status' }),
      row({ batch_id: 'b1', field_name: 'assignee_name' }),
      row({ batch_id: 'b1', field_name: 'priority' }),
    ])

    expect(groups).toHaveLength(1)
    expect(groups[0].rows.map((r) => r.field_name)).toEqual([
      'assignee_name',
      'priority',
      'status',
    ])
  })

  it('不同批次分成多组，组的顺序保持输入顺序（最新在前）', () => {
    const groups = groupTicketHistory([
      row({ batch_id: 'b3', created_at: '2026-09-11T12:00:00' }),
      row({ batch_id: 'b2', created_at: '2026-09-11T11:00:00' }),
      row({ batch_id: 'b1', created_at: '2026-09-11T10:00:00' }),
    ])

    expect(groups.map((g) => g.batchId)).toEqual(['b3', 'b2', 'b1'])
    expect(groups[0].createdAt).toBe('2026-09-11T12:00:00')
  })

  it('出生行（field_name 为 null）自成一组，kind=created', () => {
    const groups = groupTicketHistory([
      row({ batch_id: 'b2', field_name: 'status' }),
      row({ batch_id: 'b1', kind: 'created', field_name: null, old_value: null, new_value: null }),
    ])

    expect(groups).toHaveLength(2)
    expect(groups[1].kind).toBe('created')
    expect(groups[1].actorName).toBe('燕如')
    expect(groups[1].rows).toHaveLength(1)
  })

  it('空输入返回空数组（空态文案由组件决定，这里不编造占位组）', () => {
    expect(groupTicketHistory([])).toEqual([])
  })
})

describe('formatHistoryValue', () => {
  it('null 与空串显示成两个不同的东西', () => {
    // §2.3 的决定③：nil 与空串不等同（resolved_at 被清空是 NULL 不是空串）。
    // 抹平它等于把那条决定废掉 —— 读历史的人分不出「清空了」和「改成了空白」。
    expect(formatHistoryValue('resolved_at', null)).toBe('（空）')
    expect(formatHistoryValue('resolved_at', '')).toBe('（空字符串）')
    expect(formatHistoryValue('resolved_at', null)).not.toBe(formatHistoryValue('resolved_at', ''))
  })

  it('枚举列过字典，字典外的取值原样显示（历史里可能留着已废弃的词）', () => {
    expect(formatHistoryValue('status', 'resolved')).toBe('已解决')
    expect(formatHistoryValue('priority', 'medium')).toBe('普通')
    expect(formatHistoryValue('status', 'archived')).toBe('archived')
  })

  it('时间列按全站口径格式化，不把 RFC3339 原文怼给用户', () => {
    expect(formatHistoryValue('resolved_at', '2026-09-11T10:00:00')).toBe('2026-09-11 10:00:00')
    expect(formatHistoryValue('due_date', '2026-09-11T10:00:00')).toBe('2026-09-11 10:00:00')
  })

  it('时间列的值不是合法时间时原样返回，不吞成占位符', () => {
    // 历史存的是文本快照，超 500 字符会被截断（§2.3）。截断后的时间戳解析不出，
    // 显示 '—' 等于把内容吞掉 —— 读的人会以为这条记录是空的。
    expect(formatHistoryValue('resolved_at', '2026-09-11T10:00:00…(截断)')).toBe(
      '2026-09-11T10:00:00…(截断)',
    )
  })

  it('字典外的列（模型外裸列）原样显示', () => {
    // tickets 有 12 个 models 之外的列，它们真的会进历史（§2.3）。
    expect(formatHistoryValue('alert_id', 'a-1')).toBe('a-1')
    expect(formatHistoryValue('progress', '30.00')).toBe('30.00')
  })
})

describe('fieldLabel', () => {
  it('已知列给中文名，未知列原样给列名而不是隐藏', () => {
    expect(fieldLabel('status')).toBe('状态')
    expect(fieldLabel('alert_id')).toBe('alert_id')
  })
})
