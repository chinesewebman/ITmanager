import axios, { AxiosInstance } from 'axios'
import { message } from 'antd'

// 创建 axios 实例
const api: AxiosInstance = axios.create({
  baseURL: '/api',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// 请求拦截器 - 添加 token
api.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  (error) => {
    return Promise.reject(error)
  }
)

// 响应拦截器 - 统一错误处理
api.interceptors.response.use(
  (response) => {
    const res = response.data
    if (res.code !== 0) {
      message.error(res.message || '请求失败')
      return Promise.reject(new Error(res.message || '请求失败'))
    }
    return response
  },
  (error) => {
    if (error.response) {
      switch (error.response.status) {
        case 401:
          message.error('登录已过期，请重新登录')
          localStorage.removeItem('token')
          window.location.href = '/login'
          break
        case 403:
          message.error('没有权限访问')
          break
        case 404:
          message.error('请求的资源不存在')
          break
        case 500:
          message.error('服务器内部错误')
          break
        default:
          message.error(error.response.data?.message || '请求失败')
      }
    } else {
      message.error('网络错误，请检查网络连接')
    }
    return Promise.reject(error)
  }
)

export default api

// ==================== 认证 ====================
export const authApi = {
  login: (data: { username: string; password: string }) =>
    api.post('/auth/login', data),
  logout: () => api.post('/auth/logout'),
}

// ==================== 仪表盘 ====================
export const dashboardApi = {
  getStats: () => api.get('/dashboard/stats'),
  getTrends: () => api.get('/dashboard/trends'),
}

// ==================== 资产管理 ====================
export const assetApi = {
  list: (params?: { site_id?: string; type?: string; status?: string; page?: number; page_size?: number }) =>
    api.get('/assets', { params }),
  get: (id: string) => api.get(`/assets/${id}`),
  create: (data: any) => api.post('/assets', data),
  update: (id: string, data: any) => api.put(`/assets/${id}`, data),
  delete: (id: string) => api.delete(`/assets/${id}`),
}

// ==================== 告警中心 ====================
export const alertApi = {
  list: (params?: { status?: string; severity?: string; host_id?: string }) =>
    api.get('/alerts', { params }),
  get: (id: string) => api.get(`/alerts/${id}`),
  acknowledge: (id: string) => api.put(`/alerts/${id}/ack`),
  resolve: (id: string) => api.put(`/alerts/${id}/resolve`),
  getStats: () => api.get('/alerts/rules/stats'),
}

// ==================== 告警规则 ====================
export const alertRuleApi = {
  list: () => api.get('/alert-rules'),
  get: (id: string) => api.get(`/alert-rules/${id}`),
  create: (data: any) => api.post('/alert-rules', data),
  update: (id: string, data: any) => api.put(`/alert-rules/${id}`, data),
  delete: (id: string) => api.delete(`/alert-rules/${id}`),
}

// ==================== 机房 ====================
export const siteApi = {
  list: () => api.get('/sites'),
  get: (id: string) => api.get(`/sites/${id}`),
}

// ==================== 机柜 ====================
export const rackApi = {
  list: (params?: { site_id?: string }) => api.get('/racks', { params }),
  get: (id: string) => api.get(`/racks/${id}`),
  getDevices: (id: string) => api.get(`/racks/${id}/devices`),
}

// ==================== 工单 ====================
export const ticketApi = {
  list: (params?: { status?: string; priority?: string }) => api.get('/tickets', { params }),
  get: (id: string) => api.get(`/tickets/${id}`),
  create: (data: any) => api.post('/tickets', data),
  update: (id: string, data: any) => api.put(`/tickets/${id}`, data),
}

// ==================== 用户 ====================
export const userApi = {
  list: () => api.get('/users'),
  get: (id: string) => api.get(`/users/${id}`),
}

// ==================== 通知渠道 ====================
export const notificationApi = {
  listChannels: () => api.get('/notification/channels'),
  createChannel: (data: any) => api.post('/notification/channels', data),
  updateChannel: (id: string, data: any) => api.put(`/notification/channels/${id}`, data),
  deleteChannel: (id: string) => api.delete(`/notification/channels/${id}`),
  testChannel: (id: string) => api.post(`/notification/channels/${id}/test`),
}
