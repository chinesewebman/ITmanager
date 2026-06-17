import { Breadcrumb } from 'antd'
import { Link, useLocation, useParams } from 'react-router-dom'
import { useMemo } from 'react'
import { HomeOutlined } from '@ant-design/icons'

/**
 * AppBreadcrumb - 全局面包屑导航 (v1.3)
 *
 * 根据当前 pathname 自动生成面包屑路径
 * - 一级: 静态映射 (assets / alerts / ...)
 * - 二级: 详情页用 `:id` (从 useParams 取实际 id)
 * - 三级: 子页 (如 /assets/:id/diagnostics)
 *
 * 设计要点:
 *   - 用 react-router 路径, 切页自动更新
 *   - 最后一项不可点 (current page)
 *   - Home icon 在第一项
 *   - 自动跳过 404 / 403
 */

interface Crumb {
  path: string
  label: string
}

// 路由 → 中文 label 映射 (顶层)
const TOP_LABELS: Record<string, string> = {
  '/': '仪表盘',
  '/assets': '资产管理',
  '/alerts': '告警中心',
  '/alert-suppressions': '告警抑制',
  '/racks': '机房机柜',
  '/topology': '网络拓扑',
  '/oncall': '值班管理',
  '/runbooks': '故障 Runbook',
  '/metric-snapshots': '指标快照',
  '/tickets': '工单管理',
  '/settings': '系统设置',
}

const TOP_ORDER: Array<{ path: string; label: string }> = [
  { path: '/', label: '首页' },
  ...Object.entries(TOP_LABELS)
    .filter(([p]) => p !== '/')
    .map(([path, label]) => ({ path, label })),
]

export function AppBreadcrumb() {
  const location = useLocation()
  const params = useParams()

  const crumbs = useMemo(() => {
    const path = location.pathname
    if (path === '/' || path === '/login' || path === '/404') return []

    const items: Crumb[] = []

    // 首页总是第一项
    items.push({ path: '/', label: '首页' })

    // 匹配顶层路径
    const topMatch = TOP_ORDER.find((c) => c.path !== '/' && path.startsWith(c.path))
    if (topMatch) {
      items.push({ path: topMatch.path, label: topMatch.label })

      // 详情页: /assets/:id 或 /assets/:id/diagnostics
      if (params.id) {
        items.push({ path: path, label: `ID: ${params.id.slice(0, 8)}...` })
      } else if (path.includes('/diagnostics')) {
        items.push({ path: path, label: '诊断' })
      }
    }

    return items
  }, [location.pathname, params.id])

  if (crumbs.length === 0) return null

  return (
    <Breadcrumb
      style={{ marginBottom: 12 }}
      items={crumbs.map((c, idx) => {
        const isLast = idx === crumbs.length - 1
        return {
          title: isLast ? (
            <span style={{ color: 'var(--ant-color-text-secondary)' }}>{c.label}</span>
          ) : (
            <Link to={c.path}>
              {idx === 0 ? <HomeOutlined style={{ marginRight: 4 }} /> : null}
              {c.label}
            </Link>
          ),
        }
      })}
    />
  )
}

export default AppBreadcrumb
