import axios, { AxiosInstance } from "axios";
import { message } from "antd";
import type {
  AssetListParams,
  AlertListParams,
  LoginRequest,
} from "./apiClient";

// 创建 axios 实例（C-F5：用 httpOnly cookie 替代 localStorage 存 token）
const api: AxiosInstance = axios.create({
  baseURL: "/api",
  timeout: 30000,
  headers: {
    "Content-Type": "application/json",
  },
  withCredentials: true, // C-F5: 浏览器自动带上 auth_token cookie
});

// 请求拦截器（C-F5：删除 localStorage token 读取，全部走 cookie）
api.interceptors.request.use(
  (config) => {
    return config;
  },
  (error) => {
    return Promise.reject(error);
  },
);

// 响应拦截器 - 统一错误处理
api.interceptors.response.use(
  (response) => {
    const res = response.data;
    if (res.code !== 0) {
      message.error(res.message || "请求失败");
      return Promise.reject(new Error(res.message || "请求失败"));
    }
    return response;
  },
  (error) => {
    if (error.response) {
      switch (error.response.status) {
        case 401:
          message.error("登录已过期，请重新登录");
          // C-F5: cookie 由后端 /api/auth/logout 清；前端仅跳转
          window.location.href = "/login";
          break;
        case 403:
          message.error("没有权限访问");
          break;
        case 404:
          message.error("请求的资源不存在");
          break;
        case 500:
          message.error("服务器内部错误");
          break;
        default:
          message.error(error.response.data?.message || "请求失败");
      }
    } else {
      message.error("网络错误，请检查网络连接");
    }
    return Promise.reject(error);
  },
);

export default api;

// ==================== 认证 ====================
export const authApi = {
  login: (data: LoginRequest) => api.post("/auth/login", data),
  logout: () => api.post("/auth/logout"),
};

// ==================== 仪表盘 ====================
export const dashboardApi = {
  getStats: () => api.get("/dashboard/stats"),
  getTrends: (days = 7) => api.get("/dashboard/trends", { params: { days } }),
  getKPIs: (days = 7) => api.get("/dashboard/kpis", { params: { days } }),
};

// ==================== 资产管理 ====================
export const assetApi = {
  list: (params?: AssetListParams) => api.get("/assets", { params }),
  get: (id: string) => api.get(`/assets/${id}`),
  create: (data: any) => api.post("/assets", data),
  update: (id: string, data: any) => api.put(`/assets/${id}`, data),
  delete: (id: string) => api.delete(`/assets/${id}`),
};

// ==================== 告警中心 ====================
export const alertApi = {
  list: (params?: AlertListParams) => api.get("/alerts", { params }),
  get: (id: string) => api.get(`/alerts/${id}`),
  acknowledge: (id: string) => api.put(`/alerts/${id}/ack`),
  resolve: (id: string) => api.put(`/alerts/${id}/resolve`),
  getStats: () => api.get("/alerts/rules/stats"),
  // C-P6: 批量操作（单次 SQL，避免 N 次循环）
  bulkAcknowledge: (ids: string[]) => api.post("/alerts/bulk-ack", { ids }),
  bulkResolve: (ids: string[]) => api.post("/alerts/bulk-resolve", { ids }),
  bulkDelete: (ids: string[]) => api.post("/alerts/bulk-delete", { ids }),
  // 小改进 #2：标记/反标记误报 + ML 训练集导出
  // isFP=true 标记为误报，false 反标记；note 备注
  markFalsePositive: (id: string, isFP: boolean, note = "") =>
    api.post(`/alerts/${id}/mark-fp`, { is_false_positive: isFP, note }),
  // 导出误报训练集 CSV（since 可选 RFC3339 增量导出）
  exportFalsePositives: (since?: string) => {
    const params = since ? { since } : {};
    return api.get("/alerts/false-positives/export", {
      params,
      responseType: "blob",
    });
  },
};

// ==================== 告警规则 ====================
export const alertRuleApi = {
  list: () => api.get("/alert-rules"),
  get: (id: string) => api.get(`/alert-rules/${id}`),
  create: (data: any) => api.post("/alert-rules", data),
  update: (id: string, data: any) => api.put(`/alert-rules/${id}`, data),
  delete: (id: string) => api.delete(`/alert-rules/${id}`),
};

// ==================== 机房 ====================
export const siteApi = {
  list: () => api.get("/sites"),
  get: (id: string) => api.get(`/sites/${id}`),
};

// ==================== 机柜 ====================
export const rackApi = {
  list: (params?: { site_id?: string }) => api.get("/racks", { params }),
  get: (id: string) => api.get(`/racks/${id}`),
  getDevices: (id: string) => api.get(`/racks/${id}/devices`),
};

// ==================== 工单 ====================
export const ticketApi = {
  list: (params?: { status?: string; priority?: string }) =>
    api.get("/tickets", { params }),
  get: (id: string) => api.get(`/tickets/${id}`),
  create: (data: any) => api.post("/tickets", data),
  update: (id: string, data: any) => api.put(`/tickets/${id}`, data),
};

// ==================== 用户 ====================
export const userApi = {
  list: () => api.get("/users"),
  get: (id: string) => api.get(`/users/${id}`),
};

// ==================== 通知渠道 ====================
export const notificationApi = {
  listChannels: () => api.get("/notification-channels"),
  createChannel: (data: any) => api.post("/notification-channels", data),
  updateChannel: (id: string, data: any) =>
    api.put(`/notification-channels/${id}`, data),
  deleteChannel: (id: string) => api.delete(`/notification-channels/${id}`),
  testChannel: (id: string) => api.put(`/notification-channels/${id}/test`),
};

// ==================== 网络诊断 ====================
// A-1: ICMP ping + traceroute 探活

export interface PingResult {
  host: string
  count: number
  transmitted: number
  received: number
  loss_percent: number
  min_ms?: number
  avg_ms?: number
  max_ms?: number
  stddev_ms?: number
  duration_ms: number
  raw_output?: string
}

export interface TracerouteHop {
  hop: number
  host?: string
  ip?: string
  rtts?: string[]
  lossed: boolean
}

export interface TracerouteResult {
  host: string
  max_hops: number
  reached: boolean
  duration_ms: number
  hops: TracerouteHop[]
  raw_output?: string
}

export const diagnosticApi = {
  ping: (host: string, count = 4) =>
    api.get<{ code: number; data: PingResult }>("/diagnostics/ping", {
      params: { host, count },
    }),
  traceroute: (host: string, maxHops = 30) =>
    api.get<{ code: number; data: TracerouteResult }>("/diagnostics/traceroute", {
      params: { host, maxHops },
    }),
};

// ==================== 资产复盘 PDF 报告 ====================
// A-2: 复盘报告下载

export const postmortemApi = {
  // 返回 Blob（PDF 文件流）
  downloadReport: async (assetId: string, days = 30): Promise<Blob> => {
    const res = await api.get(`/postmortem/assets/${assetId}/report`, {
      params: { days },
      responseType: 'blob',
    })
    return res.data
  },
};
