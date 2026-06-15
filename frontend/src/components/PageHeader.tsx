import { Space, Button, Typography } from 'antd'
import type { ReactNode } from 'react'

export interface PageHeaderProps {
  title: string
  subtitle?: string
  extra?: ReactNode
  onCreate?: () => void
  createText?: string
}

/**
 * PageHeader - 页面标题 + 右侧操作区。
 * 统一各页面的顶部布局。
 */
export function PageHeader({ title, subtitle, extra, onCreate, createText = '新建' }: PageHeaderProps) {
  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        marginBottom: 16,
      }}
    >
      <div>
        <Typography.Title level={4} style={{ margin: 0 }}>
          {title}
        </Typography.Title>
        {subtitle && (
          <Typography.Text type="secondary" style={{ fontSize: 13 }}>
            {subtitle}
          </Typography.Text>
        )}
      </div>
      <Space>
        {extra}
        {onCreate && (
          <Button type="primary" onClick={onCreate}>
            {createText}
          </Button>
        )}
      </Space>
    </div>
  )
}

export default PageHeader
