import { Skeleton, Card, Row, Col, Space } from 'antd'

/**
 * LoadingSkeleton - 统一的加载占位组件
 *
 * 替代零散的 `<Spin>` + 文本提示，首屏不抖、骨架明确
 *
 * 用法：
 *   <LoadingSkeleton variant="table" rows={5} />
 *   <LoadingSkeleton variant="kpi-cards" />
 *   <LoadingSkeleton variant="detail" />
 *   <LoadingSkeleton variant="chart" />
 *
 * 设计要点：
 *   - 用 AntD Skeleton 的 active 动画 (微微闪烁)
 *   - variant 覆盖最常见的 4 种 loading 场景
 *   - 默认 padding 24px, 跟内容区对齐
 */

export type LoadingSkeletonVariant = 'table' | 'kpi-cards' | 'detail' | 'chart' | 'list'

interface LoadingSkeletonProps {
  variant?: LoadingSkeletonVariant
  rows?: number
}

export function LoadingSkeleton({ variant = 'table', rows = 5 }: LoadingSkeletonProps) {
  switch (variant) {
    case 'kpi-cards':
      return (
        <Row gutter={16} style={{ padding: 24 }}>
          {[1, 2, 3, 4].map((i) => (
            <Col span={6} key={i}>
              <Card>
                <Skeleton active paragraph={{ rows: 1 }} title={{ width: '60%' }} />
              </Card>
            </Col>
          ))}
        </Row>
      )

    case 'detail':
      return (
        <div style={{ padding: 24 }}>
          <Skeleton active paragraph={{ rows: 6 }} title={{ width: '40%' }} />
        </div>
      )

    case 'chart':
      return (
        <Card style={{ margin: 16 }}>
          <Skeleton.Node active style={{ width: '100%', height: 320 }}>
            <span style={{ display: 'block', width: '100%', height: 320 }} />
          </Skeleton.Node>
        </Card>
      )

    case 'list':
      return (
        <div style={{ padding: 24 }}>
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            {Array.from({ length: rows }).map((_, i) => (
              <Skeleton key={i} active avatar paragraph={{ rows: 2 }} />
            ))}
          </Space>
        </div>
      )

    case 'table':
    default:
      return (
        <div style={{ padding: 24 }}>
          <Skeleton active paragraph={{ rows: 1 }} title={{ width: '30%' }} style={{ marginBottom: 16 }} />
          <Skeleton active paragraph={{ rows }} />
        </div>
      )
  }
}

export default LoadingSkeleton
