import { Grid, Card } from 'antd'
import type { ReactNode } from 'react'

/**
 * useResponsiveTable - 响应式表格 hook (v1.3)
 *
 * mobile 宽度 (xs): 用 Card/List 替 Table, 防止横向溢出
 * desktop 宽度 (sm+): 正常 Table
 *
 * 用法:
 *   const { isMobile } = useResponsiveTable()
 *   return isMobile ? <MobileCardList ... /> : <Table ... />
 *
 * 设计要点:
 *   - 用 AntD Grid.useBreakpoint (jsdom 默认 false 测 desktop, 真实环境跟随视口)
 *   - 不引 antd-mobile (15MB 冗余, 改用纯 AntD Card 实现 mobile 卡片列表)
 */
export function useResponsiveTable() {
  const screens = Grid.useBreakpoint()
  // xs 时 (mobile) 切卡片
  const isMobile = !!screens.xs && !screens.sm
  return { isMobile, screens }
}

interface MobileCardProps<T> {
  data: T[]
  /** 单条渲染: 移动卡片的内容 */
  renderCard: (item: T, index: number) => ReactNode
  loading?: boolean
  emptyText?: ReactNode
}

/**
 * MobileCardList - 移动卡片列表 (替代表格在 mobile 上的横向溢出)
 *
 * 设计要点:
 *   - 简单 Stack 列表, 每条用 Card 显示关键字段
 *   - 复用 useResponsiveTable 判断是否显示
 */
export function MobileCardList<T extends Record<string, any>>({
  data,
  renderCard,
  loading = false,
  emptyText = "暂无数据",
}: MobileCardProps<T>) {
  if (loading) {
    return <Card loading style={{ margin: 12 }} />
  }
  if (data.length === 0) {
    return (
      <Card style={{ margin: 12, textAlign: "center" }}>
        <div style={{ padding: 24, color: "var(--ant-color-text-secondary)" }}>{emptyText}</div>
      </Card>
    )
  }
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12, padding: 12 }}>
      {data.map((item, idx) => (
        <Card key={idx} size="small">
          {renderCard(item, idx)}
        </Card>
      ))}
    </div>
  )
}
