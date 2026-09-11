// M25 工单经手历史的展示层纯函数（设计见 docs/FIX-PLAN-TICKET-HISTORY.md §2.8）。
//
// 后端返回的是**平铺行**：一行 = 一次请求里一个字段的一次变更。分组、排序、值渲染
// 都是展示层的事，放在这里而不是后端（§2.6「不做展示层聚合」）。
//
// 做成纯函数是为了能脱离 React 单测与变异反证 —— 组件里那点 fetch/loading 胶水
// 不值得为它写一堆断言，真正会出错的逻辑全在这个文件里。
import { formatDateTime, EMPTY_TIME } from './time'
import type { TicketHistory } from '../types'

/** 一次操作（同 `batch_id` 的行归为一组）。 */
export interface TicketHistoryGroup {
  batchId: string
  kind: 'created' | 'updated'
  actorName: string
  createdAt: string
  rows: TicketHistory[]
}

// 字段中文名字典。**只影响显示，不影响数据** —— 查不到的列（models 之外的裸列，
// §2.3 记录过 `tickets` 有 12 个）会原样显示列名，不隐藏：它们真的会进历史。
const FIELD_LABEL: Record<string, string> = {
  title: '标题',
  description: '描述',
  ticket_type: '工单类型',
  priority: '优先级',
  status: '状态',
  requester_name: '请求人',
  requester_email: '请求人邮箱',
  assignee_name: '处理人',
  category: '分类',
  tags: '标签',
  asset_name: '关联资产',
  resolution: '解决方案',
  resolved_at: '解决时刻',
  closed_at: '关闭时刻',
  due_date: '截止时间',
  external_id: '外部单号',
  source: '来源',
}

// 枚举列的取值字典。与 TicketTable / TicketDetailModal 的同名字典**故意各留一份**：
// 那两处渲染的是「工单当前状态」，这里渲染的是「历史快照里的值」，取值域会随历史
// 变宽（历史里可能留着已废弃的枚举值），合并成一个反而会让两边的意图互相牵制。
const STATUS_LABEL: Record<string, string> = {
  open: '新建',
  in_progress: '处理中',
  pending: '等待中',
  resolved: '已解决',
  closed: '关闭',
}

const PRIORITY_LABEL: Record<string, string> = {
  critical: '紧急',
  high: '高',
  normal: '普通',
  medium: '普通', // 遗留同义词，见 TicketTable.tsx 同名字典注释
  low: '低',
}

const VALUE_LABEL: Record<string, Record<string, string>> = {
  status: STATUS_LABEL,
  priority: PRIORITY_LABEL,
}

/** 时间列：这些列的历史快照是 RFC3339 文本，直接显示与全站口径不一致（W2）。 */
const TIME_FIELD = /(_at|_date)$/

export function fieldLabel(field: string): string {
  return FIELD_LABEL[field] ?? field
}

/**
 * 渲染历史里的一个值。
 *
 * `null` 与 `''` **必须显示成两个不同的东西**：§2.3 的决定③就是「nil 与空串不等同」
 * （`resolved_at` 被清空是 NULL 不是空串），UI 抹平它等于把那条决定废掉。
 */
export function formatHistoryValue(field: string, value: string | null): string {
  if (value === null) return '（空）'
  if (value === '') return '（空字符串）'

  const dict = VALUE_LABEL[field]
  if (dict && dict[value]) return dict[value]

  if (TIME_FIELD.test(field)) {
    const formatted = formatDateTime(value)
    // formatDateTime 对非法输入返回占位符。历史值可能是被截断的文本快照
    // （§2.3 的 500 字符截断），显示占位符等于把内容吞掉 —— 回退显示原文。
    return formatted === EMPTY_TIME ? value : formatted
  }
  return value
}

/**
 * 按 `batch_id` 分组，**保持输入顺序**（后端给的是 `created_at DESC`，最新在前），
 * 组内按 `field_name` 升序。
 *
 * 组内必须重排：同批次各行 `created_at` 相同，读端点的次序键落到随机 uuid 上，
 * 不排的话同一批字段的顺序每次刷新都在变（T-45 同族）。升序正好还原写入侧
 * 按列名升序输出的顺序（§2.3）。
 */
export function groupTicketHistory(rows: TicketHistory[]): TicketHistoryGroup[] {
  const groups: TicketHistoryGroup[] = []
  const byBatch = new Map<string, TicketHistoryGroup>()

  for (const row of rows) {
    let g = byBatch.get(row.batch_id)
    if (!g) {
      g = {
        batchId: row.batch_id,
        kind: row.kind,
        actorName: row.actor_name,
        createdAt: row.created_at,
        rows: [],
      }
      byBatch.set(row.batch_id, g)
      groups.push(g)
    }
    g.rows.push(row)
  }

  for (const g of groups) {
    g.rows.sort((a, b) => (a.field_name ?? '').localeCompare(b.field_name ?? ''))
  }
  return groups
}
