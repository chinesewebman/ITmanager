// 用户相关类型
// User 由 openapi.yaml 生成（api.types.ts components.schemas.User），
// 从 apiClient 单一源头 re-export，避免手维护 role union 漂移（P1-4）。
import type { User, User as ApiClientUser, TicketHistoryDTO as ApiTicketHistory } from '../services/apiClient'
export type { User }

// P1-4 单一源头断言：User 必须与 apiClient.User 同型（角色词表唯一来源）。
// 手写本地 User（漏/错角色）会让下面这行 tsc 报错，防 role union 漂移。
type _Equal<X, Y> = (<T>() => T extends X ? 1 : 2) extends (<T>() => T extends Y ? 1 : 2) ? true : false
type _Assert<T extends true> = T
export type UserSourceOK = _Assert<_Equal<User, ApiClientUser>>

// 认证相关类型
export interface LoginParams {
  username: string
  password: string
}

export interface LoginResponse {
  token: string
  user: User
}

// 资产相关类型
export interface Asset {
  id: string
  name: string
  asset_type: 'server' | 'switch' | 'router' | 'firewall' | 'storage' | 'other'
  ip_address: string
  mac_address?: string
  status: 'active' | 'inactive' | 'maintenance'
  site_id?: string
  site_name?: string
  rack_id?: string
  rack_name?: string
  rack_position?: number
  created_at?: string
  updated_at?: string
}

export interface AssetListParams {
  site_id?: string
  type?: string
  status?: string
  page?: number
  page_size?: number
}

// 告警相关类型
export interface Alert {
  id: string
  host: string
  message: string
  severity: number
  severity_name: string
  status: 'problem' | 'acknowledged' | 'resolved'
  asset_id?: string
  created_at: string
  ack_time?: string
  ack_user?: string
  resolve_time?: string
  resolve_user?: string
  duration?: number
}

export interface AlertStats {
  total: number
  problem: number
  acknowledged: number
  resolved: number
}

// 机房相关类型
export interface Site {
  id: string
  name: string
  location?: string
  is_active: boolean
}

// 机柜相关类型
export interface Rack {
  id: string
  name: string
  site_id: string
  total_units: number
  used_units: number
}

export interface RackDevice {
  id: string
  name: string
  asset_type: string
  rack_position: number
  status: 'green' | 'yellow' | 'red'
  alert_count: number
}

// 工单相关类型
export interface Ticket {
  id: string
  title: string
  description?: string
  priority: 'critical' | 'high' | 'normal' | 'low'
  status: 'open' | 'in_progress' | 'pending' | 'resolved' | 'closed'
  requester: string
  assignee?: string
  created_at: string
  updated_at: string
}

// M25 工单经手历史：一行 = 一次请求里一个字段的一次变更（append-only，只增不改）。
//
// 这里**手写**而不是直接 re-export 生成物，为的是让渲染层有个能挂注释、能随手加派生字段的
// 落脚点（同 Ticket）。代价是可能与 openapi 漂移 —— 所以下面用**双向可赋值**断言把它变成
// tsc 报错：正方向抓「生成物多了/收紧了字段」，反方向抓「生成物少了字段」。
//
// 用双向可赋值而不是文件顶部 User 那种 _Equal：_Equal 走条件类型同一性比较，interface 与
// 生成物里的 interface 结构完全一致时也可能判 false（实测：换成双向可赋值后立刻通过，
// 而两个方向都成立说明结构确实一致）。这里要的是「结构一致」，可赋值才是对的判据。
//
// openapi 那边对应地**如实声明了 required**（见 openapi.yaml 的 TicketHistory 注释），
// 否则生成物每个字段都是可选的，反方向恒真，断言就退化成单向。
//
// 空值语义要紧：可空列在 JSON 里是**真 null**，不是「缺键」——
// 出生行（kind=created）的 field_name 就是 null，那一次改的不是某个字段，而是「这张票存在了」。
export interface TicketHistory {
  id: string
  ticket_id: string
  /** 同一次 PUT 产生的多行共享同一个值；UI 按它分组，避免同秒两次操作并成一组 */
  batch_id: string
  kind: 'created' | 'updated'
  field_name: string | null
  /** 文本快照（时间戳按 RFC3339）。超 500 字符按 rune 截断并追加「…(截断)」 */
  old_value: string | null
  new_value: string | null
  /** 裸 UUID、无外键；写入时的操作者。用户被删号后仍保留 */
  actor_id: string | null
  /** 写入时点的姓名快照 —— 用户改名后旧记录仍显示当时的名字 */
  actor_name: string
  source: string
  request_id: string
  created_at: string
}
// [X] extends [Y] 的方括号不能省：裸 X 是类型参数，会**分配**到 union 的每个成员上，
// 那样 kind 这种 union 字段就被逐个比较，漏掉「union 整体是否等价」。
type _Extends<X, Y> = [X] extends [Y] ? true : false
export type TicketHistoryDriftOK = _Assert<_Extends<TicketHistory, ApiTicketHistory>>
export type TicketHistoryDriftOKRev = _Assert<_Extends<ApiTicketHistory, TicketHistory>>

// 通知渠道相关类型
// config 是后端落库的 JSON **字符串**（表单里才 parse 成对象）。
// 与 openapi.yaml 的 NotificationChannel 保持一致。
export interface NotificationChannel {
  id: string
  name: string
  type: 'email' | 'dingtalk' | 'wechat' | 'webhook'
  config: string
  is_enabled: boolean
}

// 仪表盘统计
export interface DashboardStats {
  assets: number
  alerts: number
  tickets: number
  sites: number
  machines: number
  networks: number
}

export interface AlertTrend {
  date: string
  count: number
}

// API 响应类型
export interface ApiResponse<T> {
  code: number
  message?: string
  data: T
}

export interface PaginatedResponse<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}
