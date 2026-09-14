import axios, { AxiosInstance } from "axios";
import { message } from "antd";
import type {
  AssetListParams,
  AlertListParams,
  TicketListParams,
  LoginRequest,
  User as UserDTO,
} from "./apiClient";
import { dispatchAuthLogout } from "./authEvents";
// M49: 审计日志查询参数（与 openapi AuditLog 同源的手写类型，见 types/index.ts）
import type { AuditListParams } from "../types";

// 创建 axios 实例（C-F5：用 httpOnly cookie 替代 localStorage 存 token）
const api: AxiosInstance = axios.create({
  baseURL: "/api",
  timeout: 30000,
  headers: {
    "Content-Type": "application/json",
  },
  withCredentials: true, // C-F5: 浏览器自动带上 auth_token cookie
});

// 响应拦截器 - 统一错误处理
api.interceptors.response.use(
  (response) => {
    // 只有「对象且自有 code 字段」的 JSON 业务包才校验 code。
    // 204（response.data === ""）、blob 下载、HTML 回退都没有 code，直接放行——
    // 否则 `res.code !== 0` 会把成功当失败（''.code 为 undefined）。
    // 用 hasOwnProperty 而非 `'code' in res`：后者沿原型链判定；也不用 Object.hasOwn（ES2022，tsconfig lib 为 ES2020）。
    const res = response.data;
    if (
      res &&
      typeof res === "object" &&
      Object.prototype.hasOwnProperty.call(res, "code") &&
      res.code !== 0
    ) {
      message.error(res.message || "请求失败");
      return Promise.reject(new Error(res.message || "请求失败"));
    }
    return response;
  },
  (error) => {
    if (error.response) {
      switch (error.response.status) {
        case 401:
          // C-F5: cookie 由后端 /api/auth/logout 清；前端通过事件让 Router 跳转
          // P1-审计: 不用 window.location.href，保留 React Router state (from URL)
          message.error("登录已过期，请重新登录");
          dispatchAuthLogout({ reason: "401" });
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

// ==================== 通用请求助手 ====================
// 解包 `{code, data}` 业务包，返回 data。
// 原先 AlertSuppressions / Oncall / Runbook / MetricSnapshot 各自复制了一份，
// 还手拼 Bearer 头——token 取自 localStorage 里一个自 C-F5 改用 httpOnly cookie 后
// 就没人写入的键 → 恒 401（后端见到非空 Authorization 头就不回退 cookie）。
// 副本已漂移过一次（Runbook 少了 204 分支），故收敛为唯一一份；
// 鉴权由实例的 withCredentials 携带 cookie。见 docs/FIX-PLAN-FRONTEND-TOKEN.md。
export async function apiGet<T>(path: string): Promise<T> {
  const res = await api.get(path);
  return res.data?.data as T;
}

export async function apiSend<T>(method: string, path: string, body?: any): Promise<T> {
  const res = await api.request({ method, url: path, data: body });
  return res.data?.data as T; // 204 无 body → undefined
}

// ==================== 认证 ====================
export const authApi = {
  login: (data: LoginRequest) => api.post("/auth/login", data),
  logout: () => api.post("/auth/logout"),
  // C7: 确认无强改密待办（幂等）。
  // 服务端判据是 DB 的 must_change_password，**不看** reason（reason 已废弃、可省略）；
  // 强改密态下无论传什么一律 400。见 docs/FIX-PLAN-AUTHZ-LEFTOVER.md §2 D-A。
  skipPasswordChange: (reason?: string) => api.post("/auth/skip-password-change", { reason }),
  // C7: 改密 (复用 PUT /auth/password; 后端 ChangePassword handler 兼容)
  changePassword: (oldPassword: string, newPassword: string) =>
    api.put("/auth/password", { old_password: oldPassword, new_password: newPassword }),
  // M49: 当前身份 + 能力集（后端从鉴权上下文下发 role/capabilities）。
  // Settings 的「管理」入口据此判断是否显示审计日志链接 —— 前端不复制一份
  // 角色→能力矩阵（backend/internal/middleware/roles.go 注释明确警告过复制会漂移，
  // 且「按钮隐藏但接口放行」的错位正是从复制矩阵开始的）。
  me: () => api.get("/auth/me"),
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
  // B4: 软退役 + 恢复 (主人 7/01 insight: 软删除的 IP 必须能被新设备继承)
  retire: (id: string, reason: string) =>
    api.post(`/assets/${id}/retire`, { reason }),
  // M58: 批量退役 —— 单请求替代 N 次串行 retire（100 项 = 100 RTT → 1 RTT）。
  // 响应是**部分成功**语义：200 + { succeeded: string[], failed: { [id]: msg } }，
  // 失败的 id 是 JSON object 的键 → 顺序不定，调用方只取数量、不依赖顺序。
  bulkRetire: (ids: string[], reason: string) =>
    api.post("/assets/bulk-retire", { ids, reason }),
  restore: (id: string) => api.post(`/assets/${id}/restore`),
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
  // D-3：从告警一键建单。后端**幂等** —— 该告警已有关联工单时返回既有那张并置
  // created=false（HTTP 200），不是错误。created 决定前端提示「已建单」还是「该告警已建单」。
  createTicket: (id: string) =>
    apiSend<{ ticket: { ticket_number: string }; created: boolean }>(
      "post",
      `/alerts/${id}/ticket`,
    ),
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
  // page_size 供统计卡取全量（后端默认 20 条会截断计数）
  list: (params?: TicketListParams) => api.get("/tickets", { params }),
  get: (id: string) => api.get(`/tickets/${id}`),
  create: (data: any) => api.post("/tickets", data),
  update: (id: string, data: any) => api.put(`/tickets/${id}`, data),
  // M25 经手历史。分页键是 size（不是 page_size）—— 与同页邻居 GET /tickets 一致，
  // 全仓本就不统一（user_handler 用 page_size），契约层也是 size。
  history: (id: string, params?: { page?: number; page_size?: number }) =>
    api.get(`/tickets/${id}/history`, { params }),
};

// ==================== 用户 ====================
// M61：账号处置（启用/禁用、改角色、置强改密）。三个写端点挂 canIdentity + 
// RejectAPIKeyAuth（仅 admin 会话可调）。
//
// 后端契约（**读实现，不猜**）：
//   - PUT   /users/:id          局部更新（status / role / must_change_password）
//   - PATCH /users/:id/status   只改状态，可写值 {active, inactive}（**没有 locked** ——
//     鉴权侧只拦 inactive，写 locked 不拦任何请求）
//   - PATCH /users/:id/role     只改角色，词表 {admin, ops_admin, ops_user, auditor, readonly}
//     （遗留别名 operator/viewer 由服务端折叠，前端只提供词表值）
// 三条都是**严格请求体**：未知键 400。故这里逐字段显式传，不透传任意对象。
export type UserStatus = 'active' | 'inactive'

/** 用户角色词表（与后端 middleware/roles.go 的 knownRoles 同源；不含遗留别名与 user 地板）。 */
export const ASSIGNABLE_ROLES = ['admin', 'ops_admin', 'ops_user', 'auditor', 'readonly'] as const
export type AssignableRole = (typeof ASSIGNABLE_ROLES)[number]

export const userApi = {
  // M50：派单候选人下拉要按角色筛，故必须能指定 page_size —— 后端默认 20 条
  // （user_handler.go:27），一个几十人的部署会静默丢掉候选池里的运维。
  // 上限由 service 夹在 500（user_service.go:34-36），传大了不会报错。
  list: (params?: { page?: number; page_size?: number }) =>
    api.get<{ code: number; data: { items: UserDTO[]; total: number } }>("/users", { params }),
  get: (id: string) => api.get(`/users/${id}`),
  // M61：局部更新。三个字段都可选，至少给一个（后端对空对象返 400）。
  update: (
    id: string,
    updates: { status?: UserStatus; role?: AssignableRole; must_change_password?: boolean },
  ) => api.put(`/users/${id}`, updates),
  updateStatus: (id: string, status: UserStatus) => api.patch(`/users/${id}/status`, { status }),
  updateRole: (id: string, role: AssignableRole) => api.patch(`/users/${id}/role`, { role }),
};

// ==================== API 密钥（B1-1） ====================
// P1 修复：Settings.tsx 之前是死表单（"重新生成"/"生成" 按钮零 onClick）
// 后端有 POST/GET/DELETE/PUT /auth/api-keys/* 四个端点，缺的只是前端 client。
// 路径前缀必须是 /auth/api-keys：baseURL 已是 "/api"，写成 "/api-keys" 会请求
// /api/api-keys → 落到 NoRoute 返回 index.html（FIX-PLAN-AUTHZ-CLOSURE.md §1 S-2a）。
export interface APIKey {
  id: string
  name: string
  prefix: string
  permissions: string[]
  ip_whitelist: string[]
  rate_limit: number
  status: string
  expires_at?: string | null
  last_used_at?: string | null
  created_at: string
}

export const apiKeyApi = {
  list: () => api.get("/auth/api-keys"),
  create: (data: { name: string; permissions?: string[]; expires_at?: string }) =>
    api.post("/auth/api-keys", data),
  revoke: (id: string) => api.put(`/auth/api-keys/${id}/revoke`),
  delete: (id: string) => api.delete(`/auth/api-keys/${id}`),
}

// ==================== 审计日志（M49 G-UI-Audit） ====================
// 后端 GET /api/audit-logs（protected + canAudit 能力：admin / ops_admin / auditor）。
// 路径**不能**写成 "/audit/logs"：真实路由是 `/api/audit-logs`（routes.go:286），
// 前缀错会落到 NoRoute 返回 index.html（同 B1-1 的 /auth/api-keys 教训）。
// 分页是 **cursor 式**（limit + next_cursor），没有 page/page_size 与 total。
export const auditApi = {
  list: (params?: AuditListParams) => api.get("/audit-logs", { params }),
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

// ==================== 第三方集成（v2.2） ====================
// P2-2 优化：Settings 集成页接后端 API，不再是死表单。
// 每个集成三件套：status 读取 / 保存配置 / 测试连通 / 立即同步。
export const integrationApi = {
  getStatus: () => api.get("/integrations/status"),

  // Zabbix
  updateZabbix: (data: { url: string; user: string; password?: string }) =>
    api.put("/integrations/zabbix", data),
  testZabbix: () => api.post("/integrations/zabbix/test"),
  syncZabbix: () => api.post("/integrations/sync", { type: "zabbix" }),

  // NetBox
  updateNetBox: (data: { url: string; token?: string }) =>
    api.put("/integrations/netbox", data),
  testNetBox: () => api.post("/integrations/netbox/test"),
  syncNetBox: () => api.post("/integrations/sync", { type: "netbox" }),

  // GLPI
  updateGLPI: (data: { url: string; app_token?: string; user_token?: string }) =>
    api.put("/integrations/glpi", data),
  testGLPI: () => api.post("/integrations/glpi/test"),
  syncGLPI: () => api.post("/integrations/sync", { type: "glpi" }),
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
