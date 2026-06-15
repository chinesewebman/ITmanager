import { Tag } from 'antd'
import type { TagProps } from 'antd'

// 通用状态色映射
const COLOR_MAP: Record<string, string> = {
  // 通用
  active: 'green',
  inactive: 'default',
  enabled: 'green',
  disabled: 'red',
  // 告警
  problem: 'red',
  acknowledged: 'orange',
  resolved: 'green',
  // 资产类型
  server: 'blue',
  switch: 'green',
  router: 'cyan',
  firewall: 'red',
  storage: 'purple',
  // 工单
  pending: 'gold',
  in_progress: 'blue',
  closed: 'default',
  // 通知
  success: 'green',
  failed: 'red',
}

export interface StatusTagProps {
  value?: string
  label?: string
  color?: TagProps['color']
}

/**
 * StatusTag - 统一状态色 tag。
 * 用法：<StatusTag value="active" /> 或 <StatusTag label="在线" color="green" />
 */
export function StatusTag({ value, label, color }: StatusTagProps) {
  const resolvedColor = color ?? (value ? COLOR_MAP[value.toLowerCase()] : undefined) ?? 'default'
  const text = label ?? value ?? ''
  return <Tag color={resolvedColor}>{text}</Tag>
}

export default StatusTag
