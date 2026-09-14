import { Breadcrumb } from 'antd'
import { Link, useLocation, useParams } from 'react-router-dom'
import { useMemo } from 'react'
import { HomeOutlined } from '@ant-design/icons'
import { useApiQuery } from '../hooks/useApiQuery'
import { assetApi, ticketApi } from '../services/api'

/**
 * AppBreadcrumb - 全局面包屑导航 (v1.3, M47 G-UI-Breadcrumb)
 *
 * 根据当前 pathname 自动生成面包屑路径
 * - 一级: 静态映射 (assets / alerts / ...)
 * - 二级: 详情页用 资产名/工单标题 (M47 修: 此前显示 ID 前 8 位, 运维不知是谁)
 * - 三级: 子页 (如 /assets/:id/diagnostics)
 *
 * 设计要点:
 *   - 用 react-router 路径, 切页自动更新
 *   - 最后一项不可点 (current page)
 *   - Home icon 在第一项
 *   - 自动跳过 404 / 403
 *   - M47: 详情 fetch 走 react-query, 失败 fallback ID: ... 保留老行为
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
  // M49：审计页由 Settings「管理」区链接进入（不在侧边栏），但面包屑要认得它
  '/audit': '审计日志',
  // M61：用户管理（admin 专属，侧边栏按 identity 能力显示；面包屑无条件认得）
  '/users': '用户管理',
}

const TOP_ORDER: Array<{ path: string; label: string }> = [
  { path: '/', label: '首页' },
  ...Object.entries(TOP_LABELS)
    .filter(([p]) => p !== '/')
    .map(([path, label]) => ({ path, label })),
]

// Fallback label 让 fetch 失败/未到达前不抖。
function fallbackIdLabel(id: string): string {
  return `ID: ${id.slice(0, 8)}...`
}

// 按顶层路径决定是否要 fetch 详情; 只对 assets/tickets 启用.
// 其他详情页 (alert-suppressions 等) 仍走 fallback ID.
function isDetailTop(topPath: string): boolean {
  return topPath === '/assets' || topPath === '/tickets'
}

// 顶层路径 → 详情解析器 (拉 name/title)
function useDetailLabel(topPath: string, id: string | undefined): string | undefined {
  const enabled = !!id && isDetailTop(topPath)

  // 只在 /assets/:id 时拉 asset; /tickets/:id 时拉 ticket.
  // 详情路由是同一个 hook 调用形状 (变量 fetcher) — 让 react-query key 区分别走.
  const assetQ = useApiQuery(
    ['breadcrumb', 'asset', id],
    async () => {
      const r: any = await assetApi.get(id!)
      return (r?.data?.data?.name ?? r?.data?.name) as string | undefined
    },
    { enabled: enabled && topPath === '/assets', staleTime: 60_000 },
  )

  const ticketQ = useApiQuery(
    ['breadcrumb', 'ticket', id],
    async () => {
      const r: any = await ticketApi.get(id!)
      return (r?.data?.data?.title ?? r?.data?.title) as string | undefined
    },
    { enabled: enabled && topPath === '/tickets', staleTime: 60_000 },
  )

  if (topPath === '/assets') return assetQ.data
  if (topPath === '/tickets') return ticketQ.data
  return undefined
}

export function AppBreadcrumb() {
  const location = useLocation()
  const params = useParams()
  // 顶层路径必须在这里解析, 供 useDetailLabel 决定是否启用 fetch.
  const topMatch = useMemo(
    () => TOP_ORDER.find((c) => c.path !== '/' && location.pathname.startsWith(c.path)),
    [location.pathname],
  )
  const topPath = topMatch?.path ?? ''
  const detailLabel = useDetailLabel(topPath, params.id)

  const crumbs = useMemo(() => {
    const path = location.pathname
    if (path === '/' || path === '/login' || path === '/404') return []

    const items: Crumb[] = []

    // 首页总是第一项
    items.push({ path: '/', label: '首页' })

    // 匹配顶层路径
    if (topMatch) {
      items.push({ path: topMatch.path, label: topMatch.label })

      // 详情页: /assets/:id 或 /assets/:id/diagnostics
      if (params.id) {
        // M47: 详情页优先显示 fetch 到的 name/title; 加载中/失败 fallback ID: ...
        const label = detailLabel ?? fallbackIdLabel(params.id)
        items.push({ path: path, label })
      } else if (path.includes('/diagnostics')) {
        items.push({ path: path, label: '诊断' })
      }
    }

    return items
  }, [location.pathname, topMatch, params.id, detailLabel])

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
