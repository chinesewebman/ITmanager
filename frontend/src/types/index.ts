// 用户相关类型
export interface User {
  id: string
  username: string
  nickname: string
  email?: string
  role: 'admin' | 'operator' | 'viewer'
  created_at?: string
}

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

// 通知渠道相关类型
export interface NotificationChannel {
  id: string
  name: string
  type: 'email' | 'dingtalk' | 'wechat' | 'webhook'
  config: Record<string, any>
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
