import { Tag } from 'antd'
import type { TagProps } from 'antd'

/**
 * SeverityTag - 告警严重度统一颜色
 *
 * 用法：<SeverityTag severity={4} />  // 渲染 P4 红色
 *      <SeverityTag severity={2} label="中等" />
 *
 * 配色 (按 Zabbix 严重度惯例):
 *   P0 (0) - not_classified - 灰
 *   P1 (1) - information    - 蓝
 *   P2 (2) - warning        - 黄
 *   P3 (3) - average        - 橙
 *   P4 (4) - high           - 红
 *   P5 (5) - disaster       - 紫红
 *
 * 设计要点:
 *   - 数字 0-5 → 标签 (中文 P0-P5)
 *   - 自定义 label 覆盖默认
 *   - 用 AntD Tag 颜色变量, 暗色模式自动适配
 */

const SEVERITY_META: Record<number, { color: TagProps['color']; label: string }> = {
  0: { color: 'default', label: 'P0 未分类' },
  1: { color: 'blue', label: 'P1 信息' },
  2: { color: 'gold', label: 'P2 警告' },
  3: { color: 'orange', label: 'P3 一般' },
  4: { color: 'red', label: 'P4 严重' },
  5: { color: 'magenta', label: 'P5 灾难' },
}

export interface SeverityTagProps {
  severity: number
  label?: string
}

export function SeverityTag({ severity, label }: SeverityTagProps) {
  const meta = SEVERITY_META[severity] ?? { color: 'default' as const, label: `P${severity}` }
  return <Tag color={meta.color}>{label ?? meta.label}</Tag>
}

export { SEVERITY_META }
export default SeverityTag
