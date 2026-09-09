import { Tag } from 'antd'
import type { TagProps } from 'antd'

// 通用状态色映射
const COLOR_MAP: Record<string, string> = {
  // 通用
  active: 'green',
  inactive: 'default',
  // H9：维护态醒目橙色，区别于「离线」（此前移动端把 maintenance 误标红「离线」）
  maintenance: 'orange',
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

// H9：资产状态中文标签单一出口（后端 asset.go:35 值域 active/offline/maintenance/retired）。
// 桌面端 AssetTable 与移动端 Assets 卡片共用，避免「维护」被写成「离线」。
export function statusLabel(s: string): string {
  return (
    s === 'active' ? '在线'
    : s === 'offline' ? '离线'
    : s === 'maintenance' ? '维护'
    : s === 'retired' ? '已退役'
    : s
  )
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
