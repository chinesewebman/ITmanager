import { ReactNode } from 'react'
import { Empty, Button } from 'antd'
import type { EmptyProps } from 'antd'

/**
 * EmptyState - 统一空状态组件
 *
 * 替代零散的内联 <Empty> 调用和"表格没数据时一片空白"的情况
 *
 * 用法：
 *   <EmptyState title="暂无资产" description="点击新建开始录入" actionText="新建资产" onAction={() => setOpen(true)} />
 *   <EmptyState title="暂无数据" />
 *   <EmptyState preset="no-alerts" />
 *
 * 设计要点：
 *   - 标题+描述+操作按钮三段式
 *   - 预设 preset 覆盖最常见的 5 种空状态文案/图标
 *   - 支持自定义 icon (AntD Empty 的 imageStyle)
 *   - 自动居中 + 适度的 padding (48px)
 */
export type EmptyStatePreset =
  | 'no-assets'        // 资产列表空
  | 'no-alerts'        // 告警列表空
  | 'no-tickets'       // 工单列表空
  | 'no-racks'         // 机柜列表空
  | 'no-search-result' // 搜索无结果

interface EmptyStateProps extends Omit<EmptyProps, 'description'> {
  title?: ReactNode
  description?: ReactNode
  actionText?: ReactNode
  onAction?: () => void
  preset?: EmptyStatePreset
  /** 自定义图标: AntD Empty 的 imageStyle.image 接受 ReactNode */
  image?: ReactNode
  /** Card 内嵌时的紧凑模式 (减小 padding) */
  compact?: boolean
}

const PRESETS: Record<EmptyStatePreset, { title: ReactNode; description: ReactNode; actionText?: ReactNode }> = {
  'no-assets': {
    title: '暂无资产',
    description: '当前没有可管理的资产，请新建或导入',
    actionText: '新建资产',
  },
  'no-alerts': {
    title: '暂无告警',
    description: '系统当前运行平稳，没有需要处理的告警',
  },
  'no-tickets': {
    title: '暂无工单',
    description: '当前没有待处理的工单',
  },
  'no-racks': {
    title: '暂无机柜',
    description: '当前机房尚未添加机柜',
  },
  'no-search-result': {
    title: '未找到匹配结果',
    description: '请调整搜索条件后重试',
  },
}

export function EmptyState({
  title,
  description,
  actionText,
  onAction,
  preset,
  image,
  compact = false,
  style,
  ...rest
}: EmptyStateProps) {
  // preset 优先级最低, prop 显式传则覆盖
  const cfg = preset ? PRESETS[preset] : null
  const finalTitle = title ?? cfg?.title ?? '暂无数据'
  const finalDescription = description ?? cfg?.description
  const finalActionText = actionText ?? cfg?.actionText

  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'center',
        padding: compact ? 24 : 48,
        minHeight: compact ? undefined : 200,
        ...style,
      }}
    >
      <Empty
        image={image ?? Empty.PRESENTED_IMAGE_SIMPLE}
        description={
          <div style={{ textAlign: 'center' }}>
            <div style={{ fontSize: 16, fontWeight: 500, marginBottom: 4, color: 'var(--ant-color-text)' }}>
              {finalTitle}
            </div>
            {finalDescription && (
              <div style={{ fontSize: 14, color: 'var(--ant-color-text-secondary)' }}>
                {finalDescription}
              </div>
            )}
          </div>
        }
        {...rest}
      >
        {finalActionText && onAction && (
          <Button type="primary" onClick={onAction}>
            {finalActionText}
          </Button>
        )}
      </Empty>
    </div>
  )
}

export default EmptyState
