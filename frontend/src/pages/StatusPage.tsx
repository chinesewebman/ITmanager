import { Result, Button } from 'antd'
import { useNavigate } from 'react-router-dom'

interface StatusPageProps {
  status: '404' | '403' | '500'
  title?: string
  subTitle?: string
}

/**
 * 通用状态页 (404/403/500)
 *
 * 设计要点：
 *   - 404 用 "🐾" emoji + 友好文案 (替代 antd 默认插画, 风格统一)
 *   - 提供"返回首页" + "返回上一页"两个动作
 *   - 状态码色块: 4xx 蓝/紫 (用户侧问题), 5xx 红 (服务器侧问题)
 */
export function StatusPage({ status, title, subTitle }: StatusPageProps) {
  const navigate = useNavigate()
  const is5xx = status === '500'

  const defaultConfig = {
    '404': { title: '页面走丢了', subTitle: '您访问的页面不存在或已被删除' },
    '403': { title: '权限不足', subTitle: '您没有权限访问此页面，请联系管理员' },
    '500': { title: '服务开小差', subTitle: '服务器内部错误，请稍后重试' },
  }[status]

  const finalTitle = title ?? defaultConfig.title
  const finalSubTitle = subTitle ?? defaultConfig.subTitle

  return (
    <Result
      status={is5xx ? '500' : status === '403' ? '403' : '404'}
      title={
        <span style={{ color: is5xx ? 'var(--ant-color-error)' : 'var(--ant-color-primary)' }}>
          {finalTitle}
        </span>
      }
      subTitle={finalSubTitle}
      extra={[
        <Button key="back" onClick={() => navigate(-1)}>
          返回上一页
        </Button>,
        <Button key="home" type="primary" onClick={() => navigate('/')}>
          返回首页
        </Button>,
      ]}
      style={{ paddingTop: 80 }}
    />
  )
}

/** 404 友好页 */
export function NotFoundPage() {
  return <StatusPage status="404" />
}

/** 403 权限不足页 */
export function ForbiddenPage() {
  return <StatusPage status="403" />
}

/** 500 服务器错误页 */
export function ServerErrorPage() {
  return <StatusPage status="500" />
}

export default StatusPage
